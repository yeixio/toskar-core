package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"strings"
	"time"
)

// HTTPS for local network access (#213): the API on port 7331 serves a
// certificate that phones and browsers can check. It is ECDSA, which they
// accept, unlike the ed25519 key Bifrost uses, and it is kept, so its
// fingerprint stays the same and a phone that saved it keeps trusting it.

// The API certificate's files, and those of the one that takes its place
// when it names the computer (#1263).
const (
	apiCertFile     = "api-tls.crt"
	apiKeyFile      = "api-tls.key"
	nextAPICertFile = "api-tls-next.crt"
	nextAPIKeyFile  = "api-tls-next.key"
	oldAPICertFile  = "api-tls-old.crt"
	oldAPIKeyFile   = "api-tls-old.key"
)

// APICertificate loads this computer's API certificate, or makes one: for
// localhost and its addresses on the local network.
func APICertificate(secrets *SecretStore) (tls.Certificate, error) {
	return loadOrMakeCertificate(secrets, apiCertFile, apiKeyFile)
}

// NextAPICertificate loads, or makes, the certificate that replaces one
// that names the computer (#1263). Phones learn its fingerprint first, so
// the switch doesn't look like another computer to them.
func NextAPICertificate(secrets *SecretStore) (tls.Certificate, error) {
	return loadOrMakeCertificate(secrets, nextAPICertFile, nextAPIKeyFile)
}

// PromoteNextAPICertificate makes the next certificate the API's own. The
// old one is kept beside it, as api-tls-old, in case it's wanted back.
func PromoteNextAPICertificate(secrets *SecretStore) error {
	if _, err := tls.LoadX509KeyPair(secrets.Path(nextAPICertFile), secrets.Path(nextAPIKeyFile)); err != nil {
		return fmt.Errorf("the next API certificate: %w", err)
	}
	for _, f := range [][2]string{{apiCertFile, oldAPICertFile}, {apiKeyFile, oldAPIKeyFile}, {nextAPIKeyFile, apiKeyFile}, {nextAPICertFile, apiCertFile}} {
		if err := os.Rename(secrets.Path(f[0]), secrets.Path(f[1])); err != nil {
			return err
		}
	}
	return nil
}

// NamesComputer reports a certificate made before #1263, whose subject or
// names carry the computer's name. Anyone who opens a connection to the
// remote listener or the relay reads them before a key is checked.
func NamesComputer(cert tls.Certificate) bool {
	leaf := cert.Leaf
	if leaf == nil {
		return false
	}
	if leaf.Subject.CommonName != "Toskar" {
		return true
	}
	for _, name := range leaf.DNSNames {
		if name != "localhost" {
			return true
		}
	}
	return false
}

func loadOrMakeCertificate(secrets *SecretStore, certName, keyName string) (tls.Certificate, error) {
	if err := secrets.EnsureDir(); err != nil {
		return tls.Certificate{}, err
	}
	certPath, keyPath := secrets.Path(certName), secrets.Path(keyName)
	if cert, err := tls.LoadX509KeyPair(certPath, keyPath); err == nil {
		return withLeaf(cert)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 62))
	if err != nil {
		return tls.Certificate{}, err
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		// No computer name in the subject or the names: the remote listener
		// shows this certificate to anyone before a key is checked (#1263).
		// Phones pin its fingerprint, so nothing reads the name.
		Subject:     pkix.Name{CommonName: "Toskar", Organization: []string{"Toskar"}},
		NotBefore:   now.Add(-time.Hour),
		NotAfter:    now.AddDate(10, 0, 0),
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:    []string{"localhost"},
		IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
	}
	tmpl.IPAddresses = append(tmpl.IPAddresses, localAddresses()...)
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return tls.Certificate{}, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return tls.Certificate{}, err
	}
	if err := os.WriteFile(certPath, certPEM, 0o644); err != nil {
		return tls.Certificate{}, err
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, err
	}
	return withLeaf(cert)
}

// LoadCertificate reads a person's own certificate and key, in PEM.
func LoadCertificate(certFile, keyFile string) (tls.Certificate, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("the API certificate: %w", err)
	}
	return withLeaf(cert)
}

func withLeaf(cert tls.Certificate) (tls.Certificate, error) {
	if cert.Leaf == nil && len(cert.Certificate) > 0 {
		leaf, err := x509.ParseCertificate(cert.Certificate[0])
		if err != nil {
			return tls.Certificate{}, err
		}
		cert.Leaf = leaf
	}
	return cert, nil
}

// CertFingerprint is the SHA-256 of a certificate, as "sha256:<hex>", the
// form browsers show as its SHA-256 fingerprint.
func CertFingerprint(cert tls.Certificate) string {
	if len(cert.Certificate) == 0 {
		return ""
	}
	sum := sha256.Sum256(cert.Certificate[0])
	return "sha256:" + hex.EncodeToString(sum[:])
}

// ShortFingerprint is the start of a fingerprint for a person to compare at
// a glance, such as "4F2A-9C1B".
func ShortFingerprint(fp string) string {
	h := strings.ToUpper(strings.TrimPrefix(fp, "sha256:"))
	if len(h) < 8 {
		return h
	}
	return h[:4] + "-" + h[4:8]
}

// localAddresses are this computer's IPv4 addresses on its networks.
func localAddresses() []net.IP {
	var out []net.IP
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && !n.IP.IsLoopback() && n.IP.To4() != nil {
			out = append(out, n.IP.To4())
		}
	}
	return out
}
