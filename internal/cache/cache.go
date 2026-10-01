// Package cache holds Yggdrasil's caches (spec §36). Every cache declares
// its policy: what the key is, how long entries live, what invalidates
// them, where they apply, and how private their contents are. Secret data
// is never cached. Caches are in memory unless a policy says the store is
// persistent (such as Mimir's indexes), and every one can be listed and
// cleared.
package cache

import (
	"container/list"
	"fmt"
	"sort"
	"sync"
	"time"
)

// Privacy levels.
const (
	// Public data is the same for anyone, such as a model's catalog entry.
	Public = "public"
	// Personal data says something about the person, such as their web
	// searches or files. It stays on this computer and is cleared with run
	// records.
	Personal = "personal"
	// Secret data, such as credentials, is never cached.
	Secret = "secret"
)

// Policy is what a cache promises.
type Policy struct {
	Name string `json:"name"`
	// Label is how the cache is shown.
	Label string `json:"label"`
	// Key says what an entry is looked up by.
	Key string        `json:"key"`
	TTL time.Duration `json:"ttl_ns"`
	// Invalidation says what removes entries besides age.
	Invalidation string `json:"invalidation"`
	// Scope says where the cache applies, such as "this computer".
	Scope   string `json:"scope"`
	Privacy string `json:"privacy"`
	// MaxEntries bounds the cache; the least recently used entry goes first.
	MaxEntries int `json:"max_entries,omitempty"`
	// Persistent marks a store kept on disk by its owner, which this
	// package describes but does not hold.
	Persistent bool `json:"persistent,omitempty"`
}

// Validate refuses a policy that would cache secrets or never expire.
func (p Policy) Validate() error {
	switch {
	case p.Name == "":
		return fmt.Errorf("a cache needs a name")
	case p.Privacy == Secret:
		return fmt.Errorf("cache %s: secret data is never cached", p.Name)
	case p.Privacy != Public && p.Privacy != Personal:
		return fmt.Errorf("cache %s: privacy must be public or personal", p.Name)
	case !p.Persistent && p.TTL <= 0:
		return fmt.Errorf("cache %s: an in-memory cache needs a TTL", p.Name)
	case !p.Persistent && p.MaxEntries <= 0:
		return fmt.Errorf("cache %s: an in-memory cache needs a size limit", p.Name)
	}
	return nil
}

// Stats are a cache's counts since it started or was last cleared.
type Stats struct {
	Entries     int        `json:"entries"`
	Hits        int64      `json:"hits"`
	Misses      int64      `json:"misses"`
	Evictions   int64      `json:"evictions"`
	LastCleared *time.Time `json:"last_cleared,omitempty"`
}

// Info is a cache's policy and stats, for listing.
type Info struct {
	Policy
	TTL string `json:"ttl"`
	Stats
}

// Store is anything the registry can list and clear.
type Store interface {
	Policy() Policy
	Stats() Stats
	Clear()
}

type entry[V any] struct {
	key     string
	value   V
	expires time.Time
}

// Cache is a bounded in-memory cache with expiry.
type Cache[V any] struct {
	policy  Policy
	mu      sync.Mutex
	items   map[string]*list.Element
	order   *list.List
	stats   Stats
	now     func() time.Time
	cleared *time.Time
}

// New returns a cache, or panics on a policy that breaks the rules above:
// that is a programming error, caught by tests.
func New[V any](p Policy) *Cache[V] {
	if err := p.Validate(); err != nil {
		panic(err)
	}
	return &Cache[V]{policy: p, items: map[string]*list.Element{}, order: list.New(), now: time.Now}
}

// Get returns a live entry.
func (c *Cache[V]) Get(key string) (V, bool) {
	var zero V
	if c == nil {
		return zero, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		c.stats.Misses++
		return zero, false
	}
	e := el.Value.(*entry[V])
	if c.now().After(e.expires) {
		c.order.Remove(el)
		delete(c.items, key)
		c.stats.Misses++
		return zero, false
	}
	c.order.MoveToFront(el)
	c.stats.Hits++
	return e.value, true
}

// Put stores an entry for the policy's TTL.
func (c *Cache[V]) Put(key string, v V) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	expires := c.now().Add(c.policy.TTL)
	if el, ok := c.items[key]; ok {
		e := el.Value.(*entry[V])
		e.value, e.expires = v, expires
		c.order.MoveToFront(el)
		return
	}
	c.items[key] = c.order.PushFront(&entry[V]{key: key, value: v, expires: expires})
	for c.order.Len() > c.policy.MaxEntries {
		last := c.order.Back()
		c.order.Remove(last)
		delete(c.items, last.Value.(*entry[V]).key)
		c.stats.Evictions++
	}
}

// Delete removes one entry.
func (c *Cache[V]) Delete(key string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		c.order.Remove(el)
		delete(c.items, key)
	}
}

// Clear removes every entry and resets the counts.
func (c *Cache[V]) Clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = map[string]*list.Element{}
	c.order.Init()
	now := c.now().UTC()
	c.stats = Stats{}
	c.cleared = &now
}

// Policy returns the cache's policy.
func (c *Cache[V]) Policy() Policy { return c.policy }

// Stats returns the counts.
func (c *Cache[V]) Stats() Stats {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.stats
	s.Entries = c.order.Len()
	s.LastCleared = c.cleared
	return s
}

// Described is a persistent store kept by its owner, listed with a policy
// and an entry count.
type Described struct {
	P       Policy
	Count   func() int
	ClearFn func()
}

// Policy returns the store's policy.
func (d Described) Policy() Policy { return d.P }

// Stats returns its size; hits are not counted for stores held elsewhere.
func (d Described) Stats() Stats {
	s := Stats{}
	if d.Count != nil {
		s.Entries = d.Count()
	}
	return s
}

// Clear clears it, when its owner allows.
func (d Described) Clear() {
	if d.ClearFn != nil {
		d.ClearFn()
	}
}

// Registry lists every cache.
type Registry struct {
	mu     sync.Mutex
	stores map[string]Store
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{stores: map[string]Store{}} }

// Add registers a store; a policy that breaks the rules is refused.
func (r *Registry) Add(s Store) error {
	if err := s.Policy().Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stores[s.Policy().Name] = s
	return nil
}

// List returns every cache's policy and stats, by name.
func (r *Registry) List() []Info {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Info, 0, len(r.stores))
	for _, s := range r.stores {
		p := s.Policy()
		ttl := "kept until invalidated"
		if p.TTL > 0 {
			ttl = p.TTL.String()
		}
		out = append(out, Info{Policy: p, TTL: ttl, Stats: s.Stats()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Clear clears one cache by name.
func (r *Registry) Clear(name string) bool {
	r.mu.Lock()
	s, ok := r.stores[name]
	r.mu.Unlock()
	if ok {
		s.Clear()
	}
	return ok
}

// ClearPrivacy clears every in-memory cache of a privacy level, such as all
// personal caches when run records are deleted.
func (r *Registry) ClearPrivacy(privacy string) []string {
	r.mu.Lock()
	var names []string
	var stores []Store
	for name, s := range r.stores {
		if p := s.Policy(); p.Privacy == privacy && !p.Persistent {
			names = append(names, name)
			stores = append(stores, s)
		}
	}
	r.mu.Unlock()
	for _, s := range stores {
		s.Clear()
	}
	sort.Strings(names)
	return names
}
