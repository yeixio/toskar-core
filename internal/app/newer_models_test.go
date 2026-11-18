package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/yeixio/toskar-core/internal/gjallarhorn"
	"github.com/yeixio/toskar-core/internal/models"
)

// Admins hear once per new generation that an installed model has a newer
// one, and not at all with the notice turned off (#593).
func TestNoticeNewerModels(t *testing.T) {
	t.Setenv("TOSKAR_DISCOVERY_ENABLED", "false")
	a, err := New(Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Shutdown(context.Background()) })
	ctx := context.Background()
	storage := a.Models.Storage()
	for _, e := range []models.CatalogEntry{
		{ID: "tst-old-3b", DisplayName: "Testa 1 3B", Family: "testa", Generation: "1"},
		{ID: "tst-old-7b", DisplayName: "Testa 1 7B", Family: "testa", Generation: "1"},
		{ID: "tst-new-4b", DisplayName: "Testa 2 4B", Family: "testa", Generation: "2", Supersedes: []string{"tst-old-3b"}},
		{ID: "tst-new-9b", DisplayName: "Testa 2 9B", Family: "testa", Generation: "2", Supersedes: []string{"tst-old-7b"}},
	} {
		a.Models.Catalog().Upsert(e)
		if err := storage.UpsertCatalogEntry(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	file := filepath.Join(t.TempDir(), "m.gguf")
	if err := os.WriteFile(file, []byte("GGUF"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"tst-old-3b", "tst-old-7b"} {
		if err := storage.MarkInstalled(ctx, id, file, "", 4); err != nil {
			t.Fatal(err)
		}
	}
	newer := func() []gjallarhorn.Notification {
		list, _, err := a.Notifications.List(ctx, false, 0, gjallarhorn.CategoryModel)
		if err != nil {
			t.Fatal(err)
		}
		var out []gjallarhorn.Notification
		for _, n := range list {
			if n.SourceType == "model" && n.SourceID == "testa@2" {
				out = append(out, n)
			}
		}
		return out
	}

	// Off: nothing.
	if err := a.Settings.SetBool(ctx, settingNotifyNewer, false); err != nil {
		t.Fatal(err)
	}
	a.noticeNewerModels(ctx)
	if got := newer(); len(got) != 0 {
		t.Fatalf("with the notice off: %+v", got)
	}

	// On: one notice for the generation, though two sizes are newer.
	if err := a.Settings.SetBool(ctx, settingNotifyNewer, true); err != nil {
		t.Fatal(err)
	}
	a.noticeNewerModels(ctx)
	got := newer()
	if len(got) != 1 || got[0].Title != "Newer Testa models are available" || got[0].Link != "/models?tab=installed" {
		t.Fatalf("notice: %+v", got)
	}
	// Once only, even after the notice is read and gone.
	if _, err := a.DB.SQL.Exec(`DELETE FROM notifications`); err != nil {
		t.Fatal(err)
	}
	a.noticeNewerModels(ctx)
	if got := newer(); len(got) != 0 {
		t.Fatalf("a second notice: %+v", got)
	}
}
