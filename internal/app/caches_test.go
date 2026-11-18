package app

import (
	"context"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/cache"
	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/internal/inventory"
)

func TestWebCacheKeysAndPrivacy(t *testing.T) {
	a := &App{}
	a.setupCaches()
	web := webCache{search: cache.New[map[string]any](webSearchPolicy), pages: cache.New[map[string]any](webPagePolicy)}
	web.Store("internet.search", map[string]any{"query": "Tide  Times"}, map[string]any{"results": []any{"x"}})
	if _, ok := web.Lookup("internet.search", map[string]any{"query": "tide times"}); !ok {
		t.Fatal("same query, different case and spacing, missed")
	}
	type hit struct{ Title string }
	web.Store("internet.search", map[string]any{"query": "typed"}, map[string]any{"results": []hit{{"a"}}})
	if _, ok := web.Lookup("internet.search", map[string]any{"query": "typed"}); !ok {
		t.Fatal("results of the search tool's own type were not cached")
	}
	web.Store("internet.search", map[string]any{"query": "nothing"}, map[string]any{"results": []any{}})
	if _, ok := web.Lookup("internet.search", map[string]any{"query": "nothing"}); ok {
		t.Fatal("an empty search was cached")
	}
	if _, ok := web.Lookup("terminal", map[string]any{"command": "ls"}); ok {
		t.Fatal("a command was cached")
	}
	names := map[string]cache.Info{}
	for _, info := range a.CacheList() {
		names[info.Name] = info
	}
	for _, want := range []string{"web_search", "web_pages", "capabilities", "knowledge_index"} {
		if _, ok := names[want]; !ok {
			t.Errorf("missing cache %s in %v", want, names)
		}
	}
	if names["web_search"].Privacy != cache.Personal || names["capabilities"].Privacy != cache.Public || !names["knowledge_index"].Persistent {
		t.Fatalf("policies = %+v", names)
	}
	if !a.ClearCache("web_search") || a.ClearCache("nope") {
		t.Fatal("clear")
	}
}

// A chat turn says which model answered it, every turn; that keeps the
// capability inventory, which only a model that really loads drops (#795).
func TestAnsweredTurnKeepsCapabilities(t *testing.T) {
	a := &App{Bus: events.NewBus(16)}
	a.setupCaches()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.watchCapabilities(ctx)
	a.capCache.Put("snapshot", inventory.Snapshot{})
	a.Bus.Publish(events.New(events.ModelLoadCompleted, map[string]any{"model_id": "m", "node_id": "", "role": "chat"}))
	time.Sleep(50 * time.Millisecond)
	if _, ok := a.capCache.Get("snapshot"); !ok {
		t.Fatal("a turn's answer dropped the inventory")
	}
	a.Bus.Publish(events.New(events.ModelLoadCompleted, map[string]any{"model_id": "m", "runtime_id": "llamacpp"}))
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, ok := a.capCache.Get("snapshot"); !ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("a model that loaded kept the old inventory")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
