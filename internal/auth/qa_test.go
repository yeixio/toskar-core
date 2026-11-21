package auth

import (
	"crypto/x509"
	"os"
	"strings"
	"testing"
)

// The certificate the remote listener shows anyone on the internet, before
// any key is checked, doesn't carry the computer's name.
func TestQACertificateHidesTheComputersName(t *testing.T) {
	// Regression: toskar-core#1263
	host, _ := os.Hostname()
	host = strings.TrimSuffix(host, ".local")
	if host == "" {
		t.Skip("this computer has no name to leak")
	}
	cert, err := APICertificate(NewSecretStore(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(leaf.Subject.String()), strings.ToLower(host)) {
		t.Errorf("the certificate's subject names the computer: %s", leaf.Subject)
	}
	for _, name := range leaf.DNSNames {
		if strings.Contains(strings.ToLower(name), strings.ToLower(host)) {
			t.Errorf("the certificate's names include the computer's: %v", leaf.DNSNames)
		}
	}
	if NamesComputer(cert) {
		t.Error("a new certificate counts as one that names the computer")
	}
}
