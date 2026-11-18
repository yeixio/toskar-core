package app

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/yeixio/toskar-core/internal/auth"
)

// Accepting Gemma's terms in Toskar keeps who and when, for the Admin, and
// the models list says the person accepted them; a model with nothing to
// accept keeps no record (#593).
func TestAcceptModelLicense(t *testing.T) {
	t.Setenv("TOSKAR_STUB_INFERENCE", "1")
	t.Setenv("TOSKAR_DISCOVERY_ENABLED", "false")
	a, err := New(Options{DataDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.DB.Close() })
	dana := auth.WithPrincipal(context.Background(), auth.Principal{Person: auth.Person{ID: "p-dana", Name: "Dana", Role: auth.RoleAdmin}})

	if err := a.AcceptModelLicense(dana, "qwen3.5-4b-q4"); err != nil {
		t.Fatal(err)
	}
	if err := a.AcceptModelLicense(dana, "gemma-3-4b-q4"); err != nil {
		t.Fatal(err)
	}
	if err := a.AcceptModelLicense(dana, "no-such-model"); err == nil {
		t.Error("accepted a model that isn't in the catalog")
	}
	list, err := a.ModelLicenseAcceptances(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ModelID != "gemma-3-4b-q4" || list[0].LicenseID != "gemma" || list[0].PersonName != "Dana" || list[0].AcceptedAt.IsZero() {
		t.Fatalf("records %+v", list)
	}

	models, err := a.listModelsCluster(dana)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range models {
		if m.ID == "gemma-3-4b-q4" && (m.License == nil || m.License.AcceptedAt == nil) {
			t.Errorf("Gemma's acceptance isn't on the model: %+v", m.License)
		}
		if m.ID == "gemma-2-2b-q4" && m.License != nil && m.License.AcceptedAt != nil {
			t.Error("accepting one Gemma marked another")
		}
	}
	// Someone else hasn't accepted them.
	sam := auth.WithPrincipal(context.Background(), auth.Principal{Person: auth.Person{ID: "p-sam", Role: auth.RoleMember}})
	models, _ = a.listModelsCluster(sam)
	for _, m := range models {
		if m.ID == "gemma-3-4b-q4" && m.License != nil && m.License.AcceptedAt != nil {
			t.Error("Dana's acceptance showed as Sam's")
		}
	}
}
