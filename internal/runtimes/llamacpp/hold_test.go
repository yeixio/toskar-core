package llamacpp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHeld(t *testing.T) {
	h := releaseHold{From: "b11950"}
	for tag, want := range map[string]bool{
		"b11949": false,
		"b11950": true,
		"b12000": true,
		"v0.7.0": true, // not a build number: can't be placed before the hold
	} {
		if got := h.held(tag); got != want {
			t.Errorf("held(%s) = %v, want %v", tag, got, want)
		}
	}
	if (releaseHold{}).held("b11950") || (releaseHold{From: "soon"}).active() {
		t.Error("an empty or unreadable hold holds something")
	}
}

// A hold skips the held releases and installs the newest one before them.
func TestChooseAssetHold(t *testing.T) {
	newer := tagged("b11952", "llama-b11952-bin-macos-arm64.tar.gz")
	held := tagged("b11950", "llama-b11950-bin-macos-arm64.tar.gz")
	good := tagged("b11949", "llama-b11949-bin-macos-arm64.tar.gz")
	r := testRuntime(t, "darwin", "arm64", false, newer, held, good)
	r.hold = func(context.Context) (releaseHold, error) {
		return releaseHold{From: "b11950", Reason: "Vulkan answers differ"}, nil
	}
	got, err := r.chooseAsset(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "llama-b11949-bin-macos-arm64.tar.gz" {
		t.Fatalf("got %s, want b11949", got.Name)
	}

	// When it can't be read, installs go ahead with the newest.
	r.hold = func(context.Context) (releaseHold, error) { return releaseHold{}, errors.New("offline") }
	if got, err = r.chooseAsset(context.Background()); err != nil || got.Name != "llama-b11952-bin-macos-arm64.tar.gz" {
		t.Fatalf("unreadable hold: got %s, %v; want b11952", got.Name, err)
	}
}

// When every one of the newest releases is held, the hold looks further
// back, a page at a time.
func TestChooseAssetHoldLooksBack(t *testing.T) {
	r := testRuntime(t, "linux", "amd64", true, tagged("b11960", "llama-b11960-bin-ubuntu-vulkan-x64.tar.gz"))
	r.hold = func(context.Context) (releaseHold, error) { return releaseHold{From: "b11900"}, nil }
	var pages []int
	r.releasePage = func(_ context.Context, page int) ([]ghRelease, error) {
		pages = append(pages, page)
		switch page {
		case 1:
			return []ghRelease{tagged("b11930", "llama-b11930-bin-ubuntu-vulkan-x64.tar.gz")}, nil
		case 2:
			return []ghRelease{tagged("b11899", "llama-b11899-bin-ubuntu-vulkan-x64.tar.gz")}, nil
		}
		return nil, nil
	}
	got, err := r.chooseAsset(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "llama-b11899-bin-ubuntu-vulkan-x64.tar.gz" || len(pages) != 2 {
		t.Fatalf("got %s after pages %v; want b11899 from page 2", got.Name, pages)
	}

	r.releasePage = func(context.Context, int) ([]ghRelease, error) { return nil, nil }
	if _, err := r.chooseAsset(context.Background()); err == nil || !strings.Contains(err.Error(), "b11900 and every release since are held back") {
		t.Fatalf("nothing before the hold: %v", err)
	}
}

func TestFetchHold(t *testing.T) {
	body := `{"hold_from":"b11950","reason":"Vulkan answers differ (#999)"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	old := holdURL
	holdURL = srv.URL
	defer func() { holdURL = old }()

	h, err := fetchHold(context.Background())
	if err != nil || h.From != "b11950" || !h.active() {
		t.Fatalf("fetchHold = %+v, %v", h, err)
	}
	body = `{"hold_from":""}`
	if h, err = fetchHold(context.Background()); err != nil || h.active() {
		t.Fatalf("no hold: %+v, %v", h, err)
	}
}

// tagged is a release with its tag, as GitHub lists it.
func tagged(tag string, names ...string) ghRelease {
	rel := release(names...)
	rel.Tag = tag
	return rel
}
