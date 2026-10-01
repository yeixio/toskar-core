package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yeixio/yggdrasil-core/pkg/contracts"
)

// Every response names the client contract, the version route describes
// it, and a client built for another major version is told so (§68).
func TestClientContractOnResponses(t *testing.T) {
	srv := NewServer(Dependencies{})
	do := func(header string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/version", nil)
		if header != "" {
			req.Header.Set(contracts.ClientContractHeader, header)
		}
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		return rec
	}
	rec := do("")
	if rec.Code != http.StatusOK || rec.Header().Get(contracts.ContractHeader) != contracts.ContractVersion {
		t.Fatalf("status=%d header=%q", rec.Code, rec.Header().Get(contracts.ContractHeader))
	}
	var v contracts.VersionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil || v.Contract.Version != contracts.ContractVersion || v.Contract.Major != 1 {
		t.Fatalf("version = %+v, %v", v, err)
	}
	if rec := do("1.3"); rec.Code != http.StatusOK {
		t.Fatalf("same major refused: %d", rec.Code)
	}
	rec = do("2.0")
	if rec.Code != http.StatusUpgradeRequired || !strings.Contains(rec.Body.String(), "Update Yggdrasil") || rec.Header().Get(contracts.ContractHeader) == "" {
		t.Fatalf("other major: %d %s", rec.Code, rec.Body)
	}
	if exposed := rec.Header().Get("Access-Control-Expose-Headers"); !strings.Contains(exposed, contracts.ContractHeader) {
		t.Fatalf("browsers cannot read the contract header: %q", exposed)
	}
}
