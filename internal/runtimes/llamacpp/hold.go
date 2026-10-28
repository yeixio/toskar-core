package llamacpp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// holdURL is where the llama.cpp canary publishes a hold (#833,
// tests/canary): when a release computes worse answers than the known-good
// one on a backend Toskar installs, installs stay on the releases before
// it until one passes again. Empty turns holds off, as tests do.
var holdURL = "https://raw.githubusercontent.com/yeixio/toskar-core/main/runtimes/llama-cpp.json"

// olderReleasesURL lists releases a page at a time, for going back past
// the newest twenty when they're all held.
var olderReleasesURL = "https://api.github.com/repos/ggml-org/llama.cpp/releases?per_page=100&page=%d"

// holdPages is how many pages of olderReleasesURL a hold looks through:
// llama.cpp tags several releases a day, so about a month.
const holdPages = 3

// releaseHold is runtimes/llama-cpp.json.
type releaseHold struct {
	// From is the first release held, such as b11950; it and every newer
	// release are skipped. Empty means nothing is held.
	From string `json:"hold_from"`
	// Reason says why, for the log.
	Reason string `json:"reason,omitempty"`
}

// fetchHold reads the hold. It's a few bytes from GitHub, so it gets a
// short time; when it can't be read, installs go ahead without it, as
// before there were holds.
func fetchHold(ctx context.Context) (releaseHold, error) {
	var h releaseHold
	if holdURL == "" {
		return h, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, holdURL, nil)
	if err != nil {
		return h, err
	}
	req.Header.Set("User-Agent", "toskar-daemon")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return h, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return h, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	err = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&h)
	return h, err
}

// tagNumber reads a release tag such as b11946 as 11946.
func tagNumber(tag string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimPrefix(tag, "b"))
	if err != nil || !strings.HasPrefix(tag, "b") {
		return 0, false
	}
	return n, true
}

// held reports whether the hold covers a release. Tags that aren't build
// numbers are held too while a hold is on, since they can't be placed
// before it.
func (h releaseHold) held(tag string) bool {
	from, ok := tagNumber(h.From)
	if !ok {
		return false
	}
	n, ok := tagNumber(tag)
	return !ok || n >= from
}

func (h releaseHold) active() bool {
	_, ok := tagNumber(h.From)
	return ok
}

func (h releaseHold) filter(rels []ghRelease) []ghRelease {
	var out []ghRelease
	for _, rel := range rels {
		if !h.held(rel.Tag) {
			out = append(out, rel)
		}
	}
	return out
}

// releasesBefore lists releases older than the hold, newest first, going
// back up to holdPages pages.
func (r *Runtime) releasesBefore(ctx context.Context, h releaseHold) ([]ghRelease, error) {
	var out []ghRelease
	for page := 1; page <= holdPages; page++ {
		rels, err := r.releasePage(ctx, page)
		if err != nil {
			return nil, err
		}
		out = append(out, h.filter(rels)...)
		if _, err := pickAsset(out, r.wantBuilds(ctx)); err == nil || len(rels) == 0 {
			break
		}
	}
	return out, nil
}

func githubReleasePage(ctx context.Context, page int) ([]ghRelease, error) {
	return listReleases(ctx, fmt.Sprintf(olderReleasesURL, page))
}

// holdReleases applies the hold to the newest releases, or, when it holds
// them all, looks further back.
func (r *Runtime) holdReleases(ctx context.Context, rels []ghRelease) ([]ghRelease, error) {
	h, err := r.hold(ctx)
	if err != nil {
		slog.Warn("llama.cpp: couldn't read the release hold; installing the newest release", "err", err)
		return rels, nil
	}
	if !h.active() {
		return rels, nil
	}
	slog.Info("llama.cpp: installing a release from before the hold", "hold_from", h.From, "reason", h.Reason)
	if kept := h.filter(rels); len(kept) > 0 {
		if _, err := pickAsset(kept, r.wantBuilds(ctx)); err == nil {
			return kept, nil
		}
	}
	older, err := r.releasesBefore(ctx, h)
	if err != nil {
		return nil, err
	}
	if len(older) == 0 {
		return nil, fmt.Errorf("llama.cpp %s and every release since are held back (%s), and no earlier release was found", h.From, h.Reason)
	}
	return older, nil
}
