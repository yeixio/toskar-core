// Command client is the paired device in the outside-network check
// (tests/outside): it tries each way to this computer in the app's order
// (home, then direct, then the relay; see the apps' anywhere.ts), pins the
// certificate it was paired with, and prints what each way did as JSON.
//
//	client -key <device key> -pin sha256:<hex> \
//	  -try home=10.77.0.10:7331 -try direct=11.0.0.2:7333 \
//	  -try relay=11.0.0.10:443,<route>.relay.toskar.test
package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"syscall"
	"time"
)

// Try is one way in and what it did.
type Try struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	OK      bool   `json:"ok"`
	// Kind is why it failed: pin (another certificate answered), refused,
	// timeout, unreachable, tls, closed (the relay hung up: no tunnel), or
	// http.
	Kind  string `json:"kind,omitempty"`
	Error string `json:"error,omitempty"`
	// NodeName is the computer that answered, from its settings.
	NodeName string `json:"node_name,omitempty"`
	MS       int64  `json:"ms"`
}

// Result is every way tried; Reached is the first that worked, as the app
// would use.
type Result struct {
	Reached string `json:"reached,omitempty"`
	Tries   []Try  `json:"tries"`
}

type tries []string

func (t *tries) String() string     { return strings.Join(*t, " ") }
func (t *tries) Set(v string) error { *t = append(*t, v); return nil }

var errPin = errors.New("the certificate isn't the one paired")

func main() {
	key := flag.String("key", "", "the device's key")
	pin := flag.String("pin", "", "the paired certificate, sha256:<hex>")
	timeout := flag.Duration("timeout", 8*time.Second, "how long each way gets")
	var ways tries
	flag.Var(&ways, "try", "name=host:port[,server name], in the order to try; repeat")
	flag.Parse()
	var out Result
	for _, w := range ways {
		name, rest, _ := strings.Cut(w, "=")
		address, sni, _ := strings.Cut(rest, ",")
		t := try(name, address, sni, *key, *pin, *timeout)
		if t.OK && out.Reached == "" {
			out.Reached = t.Name
		}
		out.Tries = append(out.Tries, t)
	}
	_ = json.NewEncoder(os.Stdout).Encode(out)
}

func try(name, address, sni, key, pin string, timeout time.Duration) (t Try) {
	t = Try{Name: name, Address: address}
	start := time.Now()
	defer func() { t.MS = time.Since(start).Milliseconds() }()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cfg := &tls.Config{
		ServerName: sni,
		// The pin is the trust, as in the app: a computer's certificate is
		// its own, signed by no authority.
		InsecureSkipVerify: true,
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return errPin
			}
			sum := sha256.Sum256(cs.PeerCertificates[0].Raw)
			if "sha256:"+hex.EncodeToString(sum[:]) != pin {
				return errPin
			}
			return nil
		},
	}
	client := &http.Client{Transport: &http.Transport{
		DialContext:     (&net.Dialer{}).DialContext,
		TLSClientConfig: cfg,
	}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+address+"/api/v1/settings", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := client.Do(req)
	if err != nil {
		t.Kind, t.Error = kind(err), err.Error()
		return t
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		t.Kind, t.Error = "http", fmt.Sprintf("%d %s", resp.StatusCode, strings.TrimSpace(string(body)))
		return t
	}
	var settings struct {
		NodeName string `json:"node_name"`
	}
	_ = json.Unmarshal(body, &settings)
	t.OK, t.NodeName = true, settings.NodeName
	return t
}

func kind(err error) string {
	var ne net.Error
	switch {
	case errors.Is(err, errPin):
		return "pin"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "refused"
	case errors.Is(err, syscall.ENETUNREACH), errors.Is(err, syscall.EHOSTUNREACH):
		return "unreachable"
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &ne) && ne.Timeout():
		return "timeout"
	case errors.Is(err, io.EOF), errors.Is(err, syscall.ECONNRESET):
		return "closed"
	}
	var re *tls.RecordHeaderError
	if errors.As(err, &re) {
		return "tls"
	}
	return "other"
}
