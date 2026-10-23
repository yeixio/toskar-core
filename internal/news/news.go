// Package news reads the AI news toskar.ai publishes every morning
// (#750): new open models, runtime releases, and the labs'
// announcements, a few lines each. It's read when someone opens News, at
// most once every few hours, and kept on disk so it shows offline. It sends
// nothing but the request, and the setting turns it off.
package news

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DefaultURL is the feed toskar.ai's news job writes.
const DefaultURL = "https://toskar.ai/news.json"

// Setting turns the news off (on by default).
const Setting = "ai_news"

const (
	maxAge         = 6 * time.Hour
	requestTimeout = 15 * time.Second
	maxBody        = 2 << 20
	cacheFile      = "news.json"
)

// Item is one piece of news.
type Item struct {
	ID      string `json:"id"`
	Date    string `json:"date"`
	Kind    string `json:"kind"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
	Source  struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	} `json:"source"`
	// ModelID is the catalog model it's about, so an Admin can install it.
	ModelID string `json:"model_id,omitempty"`
}

// Feed is toskar.ai's news.json.
type Feed struct {
	Version int    `json:"version"`
	Updated string `json:"updated"`
	Items   []Item `json:"items"`
}

// View is what the app shows.
type View struct {
	// Enabled is false when the setting is off; nothing is read then.
	Enabled bool   `json:"enabled"`
	Items   []Item `json:"items"`
	// Updated is when toskar.ai last wrote the news.
	Updated string `json:"updated,omitempty"`
	// FetchedAt is when this computer last read it.
	FetchedAt *time.Time `json:"fetched_at,omitempty"`
	// Stale is set when the news couldn't be read just now and the copy
	// kept from before is shown.
	Stale bool `json:"stale,omitempty"`
}

// Reader reads the news and keeps the last copy in Dir.
type Reader struct {
	URL    string
	Dir    string
	Client *http.Client
	// On reports whether the setting is on. Nil means it is.
	On func(ctx context.Context) bool
	// Sent records the request in What left this computer.
	Sent func(ctx context.Context, host, detail string)
	Now  func() time.Time

	mu sync.Mutex
}

type kept struct {
	FetchedAt time.Time `json:"fetched_at"`
	Feed      Feed      `json:"feed"`
}

// Get is the news: the copy kept on disk when it's recent, else read from
// toskar.ai now. When toskar.ai can't be reached, the kept copy is shown
// as stale; with none, the error is returned.
func (r *Reader) Get(ctx context.Context) (View, error) {
	if r.On != nil && !r.On(ctx) {
		return View{Items: []Item{}}, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	old, haveOld := r.load()
	if haveOld && r.now().Sub(old.FetchedAt) < maxAge {
		return view(old, false), nil
	}
	feed, err := r.fetch(ctx)
	if err != nil {
		if haveOld {
			return view(old, true), nil
		}
		return View{}, err
	}
	fresh := kept{FetchedAt: r.now().UTC(), Feed: feed}
	r.save(fresh)
	return view(fresh, false), nil
}

func view(k kept, stale bool) View {
	at := k.FetchedAt
	items := k.Feed.Items
	if items == nil {
		items = []Item{}
	}
	return View{Enabled: true, Items: items, Updated: k.Feed.Updated, FetchedAt: &at, Stale: stale}
}

func (r *Reader) fetch(ctx context.Context) (Feed, error) {
	target := r.URL
	if target == "" {
		target = DefaultURL
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return Feed{}, err
	}
	req.Header.Set("Accept", "application/json")
	if r.Sent != nil {
		if u, err := url.Parse(target); err == nil {
			r.Sent(ctx, u.Host, "the AI news")
		}
	}
	client := r.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return Feed{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Feed{}, fmt.Errorf("news: %s answered %s", target, resp.Status)
	}
	var feed Feed
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&feed); err != nil {
		return Feed{}, fmt.Errorf("news: %w", err)
	}
	if feed.Version != 1 {
		return Feed{}, fmt.Errorf("news: version %d, which this Toskar doesn't read", feed.Version)
	}
	feed.Items = usable(feed.Items)
	return feed, nil
}

// usable keeps the items the app can show: a title, and a source link
// that's https, since the app opens it.
func usable(items []Item) []Item {
	out := make([]Item, 0, len(items))
	for _, it := range items {
		if strings.TrimSpace(it.Title) == "" || !strings.HasPrefix(it.Source.URL, "https://") {
			continue
		}
		out = append(out, it)
	}
	return out
}

func (r *Reader) load() (kept, bool) {
	if r.Dir == "" {
		return kept{}, false
	}
	b, err := os.ReadFile(filepath.Join(r.Dir, cacheFile))
	if err != nil {
		return kept{}, false
	}
	var k kept
	if json.Unmarshal(b, &k) != nil || k.FetchedAt.IsZero() {
		return kept{}, false
	}
	return k, true
}

func (r *Reader) save(k kept) {
	if r.Dir == "" {
		return
	}
	b, err := json.Marshal(k)
	if err != nil || os.MkdirAll(r.Dir, 0o700) != nil {
		return
	}
	tmp := filepath.Join(r.Dir, cacheFile+".tmp")
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, filepath.Join(r.Dir, cacheFile))
	}
}

func (r *Reader) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}
