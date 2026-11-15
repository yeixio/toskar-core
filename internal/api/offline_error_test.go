package api

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Not reaching the internet is said plainly with its own code, not as
// "dial tcp: lookup huggingface.co: no such host" (#965).
func TestOfflineErrorsAreSaidPlainly(t *testing.T) {
	rec := httptest.NewRecorder()
	lookup := &net.DNSError{Err: "no such host", Name: "huggingface.co", IsNotFound: true}
	writeErrFrom(rec, http.StatusBadGateway, "BROWSE_FAILED", fmt.Errorf("Get \"https://huggingface.co/api/models\": dial tcp: lookup huggingface.co: %w", lookup))
	var body struct {
		Error struct{ Code, Message string } `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "OFFLINE" || strings.Contains(body.Error.Message, "dial tcp") || !strings.Contains(body.Error.Message, "huggingface.co couldn't be reached") {
		t.Fatalf("got %+v", body.Error)
	}
	rec = httptest.NewRecorder()
	writeErrFrom(rec, http.StatusBadGateway, "BROWSE_FAILED", fmt.Errorf("huggingface.co answered 503"))
	if !strings.Contains(rec.Body.String(), "BROWSE_FAILED") {
		t.Fatalf("an online failure lost its code: %s", rec.Body.String())
	}
}
