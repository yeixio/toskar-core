package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Installing with accept_license records the acceptance before the
// install; without it the install goes ahead with no record. The records
// list for the Admin (#593).
func TestInstallAcceptsLicense(t *testing.T) {
	mgr, err := config.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var accepted, installed []string
	srv := NewServer(Dependencies{
		Config: mgr,
		InstallModel: func(_ context.Context, id string, _ bool, _ string) error {
			installed = append(installed, id)
			return nil
		},
		AcceptModelLicense: func(_ context.Context, id string) error {
			accepted = append(accepted, id)
			return nil
		},
		ModelLicenseAcceptances: func(context.Context) ([]contracts.LicenseAcceptance, error) {
			return []contracts.LicenseAcceptance{{ModelID: "gemma-3-4b-q4", LicenseID: "gemma", PersonID: "owner", AcceptedAt: time.Now()}}, nil
		},
	})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	post := func(id, body string) {
		t.Helper()
		res, err := http.Post(ts.URL+"/api/v1/models/"+id+"/install", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("install %s: %d", id, res.StatusCode)
		}
	}
	post("gemma-3-4b-q4", `{"accept_license": true}`)
	post("qwen3.5-4b-q4", `{}`)
	if strings.Join(accepted, ",") != "gemma-3-4b-q4" || strings.Join(installed, ",") != "gemma-3-4b-q4,qwen3.5-4b-q4" {
		t.Fatalf("accepted %v, installed %v", accepted, installed)
	}

	res, err := http.Get(ts.URL + "/api/v1/models/license-acceptances")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var list []contracts.LicenseAcceptance
	if err := json.NewDecoder(res.Body).Decode(&list); err != nil || len(list) != 1 || list[0].LicenseID != "gemma" {
		t.Fatalf("records %v %+v", err, list)
	}
}
