package linkcheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// Each answer from a site sorts as open, dead, or unknown; HEAD that isn't
// allowed is tried again as GET; results are remembered.
func TestCheck(t *testing.T) {
	var heads, gets atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			heads.Add(1)
		} else {
			gets.Add(1)
		}
		switch r.URL.Path {
		case "/ok":
			w.WriteHeader(http.StatusOK)
		case "/moved":
			http.Redirect(w, r, "/ok", http.StatusMovedPermanently)
		case "/gone":
			w.WriteHeader(http.StatusGone)
		case "/missing":
			w.WriteHeader(http.StatusNotFound)
		case "/signin":
			w.WriteHeader(http.StatusForbidden)
		case "/nohead":
			if r.Method == http.MethodHead {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			w.WriteHeader(http.StatusPartialContent)
		default:
			w.WriteHeader(http.StatusTooManyRequests)
		}
	}))
	defer srv.Close()
	var sent []string
	c := &Checker{AllowPrivate: true}
	note := func(h string) { sent = append(sent, h) }
	got := c.Check(context.Background(), []string{srv.URL + "/ok", srv.URL + "/moved", srv.URL + "/gone", srv.URL + "/missing", srv.URL + "/signin", srv.URL + "/nohead"}, note)
	want := map[string]Status{"/ok": Open, "/moved": Open, "/gone": Dead, "/missing": Dead, "/signin": Unknown, "/nohead": Open}
	for path, s := range want {
		if got[srv.URL+path] != s {
			t.Errorf("%s = %q, want %q", path, got[srv.URL+path], s)
		}
	}
	if len(sent) == 0 {
		t.Error("no host recorded as sent to")
	}
	before := heads.Load() + gets.Load()
	c.Check(context.Background(), []string{srv.URL + "/ok", srv.URL + "/gone"}, nil)
	if heads.Load()+gets.Load() != before {
		t.Error("remembered links were checked again")
	}
}

// A refused connection is a site that's gone, unless nothing answered at
// all, which may be this computer being offline.
func TestNothingReachedIsUnknown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	closed := srv.URL
	srv.Close()
	c := &Checker{AllowPrivate: true}
	if got := c.Check(context.Background(), []string{closed + "/a"}, nil); got[closed+"/a"] != Unknown {
		t.Fatalf("with nothing reached: %q", got[closed+"/a"])
	}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer up.Close()
	got := (&Checker{AllowPrivate: true}).Check(context.Background(), []string{closed + "/b", up.URL + "/c"}, nil)
	if got[closed+"/b"] != Dead || got[up.URL+"/c"] != Open {
		t.Fatalf("with one site reached: %v", got)
	}
}

// Addresses on this computer or its network are never checked.
func TestPrivateAddressesAreSkipped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("a private address was requested")
	}))
	defer srv.Close()
	var sent int
	c := &Checker{}
	got := c.Check(context.Background(), []string{srv.URL + "/x", "http://localhost/y", "http://printer.local/", "http://10.0.0.5/admin", "http://router/", "ftp://example.com/file"}, func(string) { sent++ })
	if len(got) != 0 || sent != 0 {
		t.Fatalf("checked %v, sent %d", got, sent)
	}
}

// At most MaxLinks links are checked.
func TestAtMostMaxLinks(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { n.Add(1) }))
	defer srv.Close()
	var links []string
	for i := range MaxLinks + 4 {
		links = append(links, srv.URL+"/p"+string(rune('a'+i)))
	}
	got := (&Checker{AllowPrivate: true}).Check(context.Background(), links, nil)
	if len(got) != MaxLinks || int(n.Load()) != MaxLinks {
		t.Fatalf("checked %d, requested %d", len(got), n.Load())
	}
}
