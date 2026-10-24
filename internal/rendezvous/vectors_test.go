package rendezvous

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/auth"
)

// The connection test vectors (docs/connecting.md, toskar-core#666): what
// every SDK and app checks its own route IDs, pins, and record opening
// against. They're made here, with this package as the reference, and this
// test keeps the file and the code in step. UPDATE_CONNECTION_VECTORS=1
// makes them again, with new keys and records.

const vectorsFile = "../../api/connection-vectors.json"

type vectors struct {
	About   string         `json:"about"`
	Routes  []routeVector  `json:"routes"`
	Pins    []pinVector    `json:"pins"`
	Records []recordVector `json:"records"`
}

type routeVector struct {
	// Secret is the route secret as pairing gives it: base64url, no padding.
	Secret       string `json:"secret"`
	RouteID      string `json:"route_id"`
	RecordKeyHex string `json:"record_key_hex"`
}

type pinVector struct {
	CertDER     string `json:"cert_der_base64"`
	Fingerprint string `json:"fingerprint"`
	Short       string `json:"short"`
}

type recordVector struct {
	Name string `json:"name"`
	// Blob is the record as the relay answers it, in standard base64.
	Blob          string   `json:"blob_base64"`
	Secret        string   `json:"secret"`
	Pin           string   `json:"pin"`
	Now           int64    `json:"now"`
	MaxAgeSeconds int64    `json:"max_age_seconds"`
	Addresses     []string `json:"addresses,omitempty"`
	At            int64    `json:"at,omitempty"`
	// Error is why it must be refused: sealed, pin, or stale.
	Error string `json:"error,omitempty"`
}

func TestConnectionVectors(t *testing.T) {
	if os.Getenv("UPDATE_CONNECTION_VECTORS") != "" {
		writeVectors(t)
	}
	raw, err := os.ReadFile(vectorsFile)
	if err != nil {
		t.Fatal(err)
	}
	var v vectors
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	if len(v.Routes) == 0 || len(v.Pins) == 0 || len(v.Records) == 0 {
		t.Fatal("the vectors are empty")
	}
	for _, r := range v.Routes {
		secret, err := base64.RawURLEncoding.DecodeString(r.Secret)
		if err != nil || len(secret) != SecretSize {
			t.Fatalf("secret %q: %v", r.Secret, err)
		}
		if got := RouteID(secret); got != r.RouteID {
			t.Errorf("route ID of %s is %s, the vectors say %s", r.Secret, got, r.RouteID)
		}
		if got := hex.EncodeToString(recordKey(secret)); got != r.RecordKeyHex {
			t.Errorf("record key of %s is %s, the vectors say %s", r.Secret, got, r.RecordKeyHex)
		}
	}
	for _, p := range v.Pins {
		der, err := base64.StdEncoding.DecodeString(p.CertDER)
		if err != nil {
			t.Fatal(err)
		}
		fp := auth.CertFingerprint(tls.Certificate{Certificate: [][]byte{der}})
		if fp != p.Fingerprint || auth.ShortFingerprint(fp) != p.Short {
			t.Errorf("pin is %s (%s), the vectors say %s (%s)", fp, auth.ShortFingerprint(fp), p.Fingerprint, p.Short)
		}
	}
	for _, r := range v.Records {
		blob, err := base64.StdEncoding.DecodeString(r.Blob)
		if err != nil {
			t.Fatalf("%s: %v", r.Name, err)
		}
		secret, _ := base64.RawURLEncoding.DecodeString(r.Secret)
		opened, err := Open(blob, secret, r.Pin, time.Unix(r.Now, 0), time.Duration(r.MaxAgeSeconds)*time.Second)
		switch {
		case r.Error != "":
			if want := map[string]error{"sealed": ErrRecordSealed, "pin": ErrRecordPin, "stale": ErrRecordStale}[r.Error]; !errors.Is(err, want) {
				t.Errorf("%s: opened with %v, the vectors say %s", r.Name, err, r.Error)
			}
		case err != nil:
			t.Errorf("%s: %v", r.Name, err)
		case opened.At.Unix() != r.At || !equal(opened.Addresses, r.Addresses):
			t.Errorf("%s: opened %v at %d, the vectors say %v at %d", r.Name, opened.Addresses, opened.At.Unix(), r.Addresses, r.At)
		}
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// writeVectors makes the vectors: three route secrets, a certificate for
// each signature algorithm, and records to open and to refuse.
func writeVectors(t *testing.T) {
	t.Helper()
	at := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	secret := func(fill byte) []byte {
		b := make([]byte, SecretSize)
		for i := range b {
			b[i] = fill + byte(i)
		}
		return b
	}
	secrets := [][]byte{secret(0), secret(0x40), secret(0xa0)}
	v := vectors{About: "Test vectors for connecting to Toskar Core as a paired device: docs/connecting.md. Made by internal/rendezvous's TestConnectionVectors, which keeps them in step with the reference code."}
	for _, s := range secrets {
		v.Routes = append(v.Routes, routeVector{Secret: EncodeSecret(s), RouteID: RouteID(s), RecordKeyHex: hex.EncodeToString(recordKey(s))})
	}

	certs := map[string]tls.Certificate{}
	for _, alg := range []string{"ecdsa-sha256", "ed25519", "rsa-pkcs1-sha256"} {
		certs[alg] = selfSigned(t, alg)
		fp := auth.CertFingerprint(certs[alg])
		v.Pins = append(v.Pins, pinVector{CertDER: base64.StdEncoding.EncodeToString(certs[alg].Certificate[0]), Fingerprint: fp, Short: auth.ShortFingerprint(fp)})
	}

	addresses := []string{"203.0.113.7:7333", "[2001:db8::7]:7333", RouteID(secrets[0]) + ".relay.toskar.ai:4443"}
	seal := func(s []byte, alg string) []byte {
		blob, err := Seal(s, certs[alg], addresses, at)
		if err != nil {
			t.Fatal(err)
		}
		return blob
	}
	pin := func(alg string) string { return auth.CertFingerprint(certs[alg]) }
	add := func(name string, blob, s []byte, p string, now time.Time, failure string) {
		r := recordVector{Name: name, Blob: base64.StdEncoding.EncodeToString(blob), Secret: EncodeSecret(s), Pin: p, Now: now.Unix(), MaxAgeSeconds: 3600, Error: failure}
		if failure == "" {
			r.Addresses, r.At = addresses, at.Unix()
		}
		v.Records = append(v.Records, r)
	}
	ecdsaBlob := seal(secrets[0], "ecdsa-sha256")
	add("opens: signed with an ECDSA P-256 key", ecdsaBlob, secrets[0], pin("ecdsa-sha256"), at.Add(10*time.Minute), "")
	add("opens: signed with an Ed25519 key", seal(secrets[0], "ed25519"), secrets[0], pin("ed25519"), at.Add(10*time.Minute), "")
	add("opens: signed with an RSA key", seal(secrets[0], "rsa-pkcs1-sha256"), secrets[0], pin("rsa-pkcs1-sha256"), at.Add(10*time.Minute), "")
	add("opens: written a minute in the future, within the 5 minutes allowed", ecdsaBlob, secrets[0], pin("ecdsa-sha256"), at.Add(-time.Minute), "")
	add("refused: another computer's route secret", ecdsaBlob, secrets[1], pin("ecdsa-sha256"), at.Add(10*time.Minute), "sealed")
	add("refused: not the pinned certificate", ecdsaBlob, secrets[0], pin("ed25519"), at.Add(10*time.Minute), "pin")
	add("refused: older than the hour allowed", ecdsaBlob, secrets[0], pin("ecdsa-sha256"), at.Add(61*time.Minute), "stale")
	add("refused: more than 5 minutes in the future", ecdsaBlob, secrets[0], pin("ecdsa-sha256"), at.Add(-6*time.Minute), "stale")
	tampered := append([]byte(nil), ecdsaBlob...)
	tampered[len(tampered)/2] ^= 0x01
	add("refused: changed on the way", tampered, secrets[0], pin("ecdsa-sha256"), at.Add(10*time.Minute), "sealed")
	add("refused: too short to be a record", ecdsaBlob[:8], secrets[0], pin("ecdsa-sha256"), at.Add(10*time.Minute), "sealed")
	// A record another computer signed, sealed with this computer's secret:
	// it opens, but its certificate isn't this one's pin.
	add("refused: sealed with the secret but signed by another computer", seal(secrets[0], "rsa-pkcs1-sha256"), secrets[0], pin("ecdsa-sha256"), at.Add(10*time.Minute), "pin")
	forged := forgeRoute(t, secrets[0], certs["ecdsa-sha256"], RouteID(secrets[1]), at)
	add("refused: signed for another route", forged, secrets[0], pin("ecdsa-sha256"), at.Add(10*time.Minute), "pin")

	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(vectorsFile, append(out, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

// forgeRoute seals a record whose signed payload names another route, as a
// computer replaying its record under someone else's route would.
func forgeRoute(t *testing.T, secret []byte, cert tls.Certificate, route string, at time.Time) []byte {
	t.Helper()
	payload, _ := json.Marshal(signed{V: 1, Route: route, At: at.Unix(), Addresses: []string{"198.51.100.9:7333"}})
	alg, sig, err := sign(cert.PrivateKey, payload)
	if err != nil {
		t.Fatal(err)
	}
	plain, _ := json.Marshal(record{Payload: payload, Cert: cert.Certificate[0], Alg: alg, Sig: sig})
	gcm, err := aead(secret)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, gcm.NonceSize())
	_, _ = rand.Read(nonce)
	return gcm.Seal(nonce, nonce, plain, []byte(RouteID(secret)))
}

func selfSigned(t *testing.T, alg string) tls.Certificate {
	t.Helper()
	var key crypto.Signer
	var err error
	switch alg {
	case "ecdsa-sha256":
		key, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	case "ed25519":
		_, key, err = ed25519.GenerateKey(rand.Reader)
	case "rsa-pkcs1-sha256":
		key, err = rsa.GenerateKey(rand.Reader, 2048)
	}
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "Toskar test vectors (" + alg + ")"},
		NotBefore:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		NotAfter:     time.Date(2036, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, key.Public(), key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}
