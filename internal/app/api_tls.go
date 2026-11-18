package app

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"sync"
	"time"

	"github.com/yeixio/toskar-core/internal/api"
	"github.com/yeixio/toskar-core/internal/auth"
)

// setAPITLS has the API answer HTTPS (#213): with the person's own
// certificate when api_tls_cert and api_tls_key are set and can be read,
// and otherwise with the one Toskar makes and keeps.
func (a *App) setAPITLS(ctx context.Context) {
	cfg := a.Config.Get()
	info := api.APITLS{}
	var cert tls.Certificate
	var err error
	if cfg.APITLSCert != "" || cfg.APITLSKey != "" {
		if cert, err = auth.LoadCertificate(cfg.APITLSCert, cfg.APITLSKey); err == nil {
			info.Custom = true
		} else {
			info.Error = err.Error()
			a.Logger.Warn("own API certificate not used", "error", err)
		}
	}
	if !info.Custom {
		if cert, err = auth.APICertificate(auth.NewSecretStore(cfg.DataDir)); err != nil {
			a.Logger.Warn("API serves plain HTTP only: no certificate", "error", err)
			return
		}
	}
	a.API.SetTLS(&tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}, tlsInfo(cert, info))
	if !info.Custom && auth.NamesComputer(cert) {
		a.startTLSSwitch(ctx)
	}
}

func tlsInfo(cert tls.Certificate, info api.APITLS) api.APITLS {
	info.Enabled = true
	info.Fingerprint = auth.CertFingerprint(cert)
	info.Short = auth.ShortFingerprint(info.Fingerprint)
	if cert.Leaf != nil {
		t := cert.Leaf.NotAfter
		info.NotAfter = &t
	}
	return info
}

// Replacing a certificate that names the computer (#1263). Certificates
// made before then carry its name, which the remote listener and the relay
// show anyone before a key is checked. Phones pin the certificate's
// fingerprint, so a new one would look like another computer to them.
// Instead, Toskar makes the next certificate and offers its fingerprint at
// /api/v1/tls/next; the app saves it and accepts the switch. Toskar
// switches once every phone used lately has asked, or after
// tlsSwitchAtMost, whichever is first.
const (
	// tlsSwitchActive is how recently a device must have been used for the
	// switch to wait for it.
	tlsSwitchActive = 30 * 24 * time.Hour
	// tlsSwitchAtMost is the longest the switch waits.
	tlsSwitchAtMost = 30 * 24 * time.Hour
	tlsSwitchEvery  = time.Hour
	tlsSwitchFile   = "api-tls-next.json"
)

// tlsSwitchState is kept beside the next certificate: when it was made,
// and which devices' keys have asked for it.
type tlsSwitchState struct {
	Since time.Time            `json:"since"`
	Asked map[string]time.Time `json:"asked"`
}

type tlsSwitch struct {
	mu      sync.Mutex
	secrets *auth.SecretStore
	next    tls.Certificate
	state   tlsSwitchState
	done    bool
	wake    chan struct{}
}

func (a *App) startTLSSwitch(ctx context.Context) {
	secrets := auth.NewSecretStore(a.Config.Get().DataDir)
	next, err := auth.NextAPICertificate(secrets)
	if err != nil {
		a.Logger.Warn("the API certificate names this computer and can't be replaced", "error", err)
		return
	}
	sw := &tlsSwitch{secrets: secrets, next: next, wake: make(chan struct{}, 1)}
	if raw, err := secrets.Read(tlsSwitchFile); err == nil {
		_ = json.Unmarshal([]byte(raw), &sw.state)
	}
	if sw.state.Since.IsZero() {
		sw.state.Since = time.Now().UTC()
	}
	if sw.state.Asked == nil {
		sw.state.Asked = map[string]time.Time{}
	}
	sw.save()
	a.tlsSwitch = sw
	a.API.SetNextTLS(auth.CertFingerprint(next), sw.asked)
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		for {
			if a.maybeSwitchTLS(ctx, sw, time.Now()) {
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-sw.wake:
			case <-time.After(tlsSwitchEvery):
			}
		}
	}()
}

// asked notes a device's key asking for the next certificate.
func (sw *tlsSwitch) asked(keyID string) {
	sw.mu.Lock()
	_, known := sw.state.Asked[keyID]
	sw.state.Asked[keyID] = time.Now().UTC()
	if !known {
		sw.saveLocked()
	}
	sw.mu.Unlock()
	if !known {
		select {
		case sw.wake <- struct{}{}:
		default:
		}
	}
}

func (sw *tlsSwitch) save() {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	sw.saveLocked()
}

func (sw *tlsSwitch) saveLocked() {
	if raw, err := json.Marshal(sw.state); err == nil {
		_ = sw.secrets.Write(tlsSwitchFile, string(raw))
	}
}

// maybeSwitchTLS switches to the next certificate when it's time, and
// reports whether the switch is done.
func (a *App) maybeSwitchTLS(ctx context.Context, sw *tlsSwitch, now time.Time) bool {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	if sw.done {
		return true
	}
	if waiting := a.devicesWithoutNextTLS(ctx, sw.state.Asked, now); waiting > 0 && now.Sub(sw.state.Since) < tlsSwitchAtMost {
		return false
	}
	if err := auth.PromoteNextAPICertificate(sw.secrets); err != nil {
		a.Logger.Warn("couldn't switch to the API certificate without the computer's name", "error", err)
		return false
	}
	_ = sw.secrets.Delete(tlsSwitchFile)
	sw.done = true
	a.API.ReplaceTLSCertificate(sw.next, tlsInfo(sw.next, api.APITLS{}))
	a.API.SetNextTLS("", nil)
	// The relay's record is signed with the certificate: sign it again.
	a.relayMu.Lock()
	if a.relay != nil {
		a.relay.RegisterNow()
	}
	a.relayMu.Unlock()
	a.Logger.Info("switched to an API certificate without the computer's name")
	return true
}

// devicesWithoutNextTLS counts the phones used lately that haven't asked
// for the next certificate. A watch's key doesn't count: its phone hands
// it the new fingerprint.
func (a *App) devicesWithoutNextTLS(ctx context.Context, asked map[string]time.Time, now time.Time) int {
	if a.APIKeys == nil {
		return 0
	}
	keys, err := a.APIKeys.List(ctx)
	if err != nil {
		return 1
	}
	companions, err := a.APIKeys.CompanionKeys(ctx)
	if err != nil {
		return 1
	}
	n := 0
	for _, k := range keys {
		if k.Kind != auth.KindDevice || companions[k.ID] || k.LastUsed == nil || now.Sub(*k.LastUsed) > tlsSwitchActive {
			continue
		}
		if _, ok := asked[k.ID]; !ok {
			n++
		}
	}
	return n
}
