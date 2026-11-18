package app

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"log/slog"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/auth"
)

// oldAPICertificate writes a certificate as Toskar made them before #1263,
// with the computer's name in its subject and names.
func oldAPICertificate(t *testing.T, secrets *auth.SecretStore) {
	t.Helper()
	if err := secrets.EnsureDir(); err != nil {
		t.Fatal(err)
	}
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(7),
		Subject:      pkix.Name{CommonName: "Toskar on Michaels-MacBook-Pro"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(1, 0, 0),
		DNSNames:     []string{"localhost", "Michaels-MacBook-Pro", "Michaels-MacBook-Pro.local"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, _ := x509.MarshalPKCS8PrivateKey(key)
	_ = os.WriteFile(secrets.Path("api-tls.crt"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644)
	_ = os.WriteFile(secrets.Path("api-tls.key"), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600)
}

func newTLSApp(t *testing.T) (*App, *auth.SecretStore) {
	t.Helper()
	t.Setenv("TOSKAR_DISCOVERY_ENABLED", "false")
	dir := t.TempDir()
	a, err := New(Options{DataDir: dir, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.DB.Close() })
	secrets := auth.NewSecretStore(a.Config.Get().DataDir)
	oldAPICertificate(t, secrets)
	return a, secrets
}

// A certificate that names the computer is replaced once the phones used
// lately have learned the next one, so they don't take the switch for
// another computer.
func TestOldCertificateIsReplacedOncePhonesKnowTheNext(t *testing.T) {
	// Regression: toskar-core#1263
	a, secrets := newTLSApp(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); a.wg.Wait() })
	phone, secret, err := a.APIKeys.CreateDevice(ctx, "Phone")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.APIKeys.Verify(ctx, secret); err != nil {
		t.Fatal(err)
	}
	// A phone not used for months doesn't hold the switch up.
	if _, _, err := a.APIKeys.CreateDevice(ctx, "Old phone"); err != nil {
		t.Fatal(err)
	}

	a.setAPITLS(ctx)
	old := a.API.TLS().Fingerprint
	sw := a.tlsSwitch
	if sw == nil {
		t.Fatal("no switch for a certificate that names the computer")
	}
	next := auth.CertFingerprint(sw.next)
	if next == old {
		t.Fatal("the next certificate is the old one")
	}
	if auth.NamesComputer(sw.next) {
		t.Fatalf("the next certificate names the computer: %v %v", sw.next.Leaf.Subject, sw.next.Leaf.DNSNames)
	}
	if a.maybeSwitchTLS(ctx, sw, time.Now()) {
		t.Fatal("switched before the phone learned the next certificate")
	}

	sw.asked(phone.ID)
	deadline := time.Now().Add(5 * time.Second)
	for a.API.TLS().Fingerprint != next {
		if time.Now().After(deadline) {
			t.Fatal("never switched after the phone learned the next certificate")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if cert, ok := a.API.TLSCertificate(); !ok || auth.CertFingerprint(cert) != next {
		t.Fatal("the relay's record would still be signed with the old certificate")
	}
	kept, err := auth.APICertificate(secrets)
	if err != nil || auth.CertFingerprint(kept) != next {
		t.Fatalf("the switch isn't kept for the next start: %v", err)
	}
}

// The switch waits for phones at most tlsSwitchAtMost.
func TestOldCertificateIsReplacedAfterAMonth(t *testing.T) {
	// Regression: toskar-core#1263
	a, _ := newTLSApp(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); a.wg.Wait() })
	_, secret, _ := a.APIKeys.CreateDevice(ctx, "Phone")
	if _, err := a.APIKeys.Verify(ctx, secret); err != nil {
		t.Fatal(err)
	}
	a.setAPITLS(ctx)
	sw := a.tlsSwitch
	if a.maybeSwitchTLS(ctx, sw, time.Now()) {
		t.Fatal("switched at once")
	}
	if !a.maybeSwitchTLS(ctx, sw, time.Now().Add(tlsSwitchAtMost+time.Hour)) {
		t.Fatal("still waiting after a month")
	}
	if a.API.TLS().Fingerprint != auth.CertFingerprint(sw.next) {
		t.Fatal("the API still answers with the old certificate")
	}
}

// A certificate made since #1263 is kept as it is.
func TestNewCertificateIsKept(t *testing.T) {
	t.Setenv("TOSKAR_DISCOVERY_ENABLED", "false")
	a, err := New(Options{DataDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.DB.Close() })
	a.setAPITLS(context.Background())
	if a.tlsSwitch != nil {
		t.Fatal("a certificate without the computer's name is being replaced")
	}
}
