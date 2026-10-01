package cache

import (
	"strings"
	"testing"
	"time"
)

func policy() Policy {
	return Policy{Name: "web", Label: "Web", Key: "query", TTL: time.Minute, Invalidation: "age", Scope: "this computer", Privacy: Personal, MaxEntries: 2}
}

func TestCacheExpiryLRUAndStats(t *testing.T) {
	c := New[string](policy())
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return now }
	if _, ok := c.Get("a"); ok {
		t.Fatal("hit on an empty cache")
	}
	c.Put("a", "A")
	c.Put("b", "B")
	if v, ok := c.Get("a"); !ok || v != "A" {
		t.Fatal("miss")
	}
	c.Put("c", "C") // evicts b, the least recently used
	if _, ok := c.Get("b"); ok {
		t.Fatal("b was not evicted")
	}
	now = now.Add(2 * time.Minute)
	if _, ok := c.Get("a"); ok {
		t.Fatal("expired entry returned")
	}
	s := c.Stats()
	if s.Hits != 1 || s.Misses != 3 || s.Evictions != 1 || s.Entries != 1 {
		t.Fatalf("stats = %+v", s)
	}
	c.Clear()
	if s := c.Stats(); s.Entries != 0 || s.Hits != 0 || s.LastCleared == nil {
		t.Fatalf("after clear = %+v", s)
	}
	var nilCache *Cache[string]
	nilCache.Put("x", "y")
	if _, ok := nilCache.Get("x"); ok {
		t.Fatal("nil cache hit")
	}
}

func TestPoliciesRefuseSecretsAndForever(t *testing.T) {
	for _, bad := range []Policy{
		{Name: "creds", Privacy: Secret, TTL: time.Minute, MaxEntries: 1},
		{Name: "forever", Privacy: Public, MaxEntries: 1},
		{Name: "unbounded", Privacy: Public, TTL: time.Minute},
		{Name: "odd", Privacy: "shared", TTL: time.Minute, MaxEntries: 1},
	} {
		if err := bad.Validate(); err == nil {
			t.Errorf("%s accepted", bad.Name)
		}
	}
	if err := (Policy{Name: "index", Privacy: Personal, Persistent: true}).Validate(); err != nil {
		t.Fatalf("persistent store refused: %v", err)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("a secret cache was created")
		}
	}()
	New[string](Policy{Name: "creds", Privacy: Secret, TTL: time.Minute, MaxEntries: 1})
}

func TestRegistry(t *testing.T) {
	r := NewRegistry()
	web := New[string](policy())
	pub := New[int](Policy{Name: "models", Privacy: Public, TTL: time.Minute, MaxEntries: 5, Scope: "this computer"})
	cleared := false
	if err := r.Add(web); err != nil {
		t.Fatal(err)
	}
	_ = r.Add(pub)
	_ = r.Add(Described{P: Policy{Name: "index", Privacy: Personal, Persistent: true}, Count: func() int { return 42 }, ClearFn: func() { cleared = true }})
	web.Put("q", "r")
	pub.Put("m", 1)
	list := r.List()
	if len(list) != 3 || list[0].Name != "index" || list[0].Entries != 42 || !strings.Contains(list[0].TTL, "invalidated") || list[2].TTL != "1m0s" {
		t.Fatalf("list = %+v", list)
	}
	// Deleting run records clears personal in-memory caches, not the index.
	if names := r.ClearPrivacy(Personal); len(names) != 1 || names[0] != "web" || web.Stats().Entries != 0 || pub.Stats().Entries != 1 || cleared {
		t.Fatalf("cleared %v", names)
	}
	if !r.Clear("index") || !cleared || r.Clear("nope") {
		t.Fatal("clear by name")
	}
	if err := r.Add(Described{P: Policy{Name: "keys", Privacy: Secret, Persistent: true}}); err == nil {
		t.Fatal("secret store registered")
	}
}
