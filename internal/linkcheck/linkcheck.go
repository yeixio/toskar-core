// Package linkcheck finds which links in an answer don't open (#209): a
// page gone since it was indexed, a site that's gone, a page the person
// pasted that no longer loads. The text check before it (#208) catches
// guessed links; this one asks each site.
package linkcheck

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Status is what a check found.
type Status string

const (
	// Open is a page that answered.
	Open Status = "open"
	// Dead is a page that's gone (404 or 410), a site whose name doesn't
	// exist, or one that refused the connection.
	Dead Status = "dead"
	// Unknown is a page that couldn't be judged: it asked for a sign-in,
	// refused or limited the check, failed on its side, or took too long.
	Unknown Status = "unknown"
)

const (
	// MaxLinks is the most links one answer has checked.
	MaxLinks = 6
	// perLink and perAnswer bound how long checking takes.
	perLink   = 4 * time.Second
	perAnswer = 6 * time.Second
	// cacheFor is how long a page's result is remembered.
	cacheFor = time.Hour
)

// errPrivate refuses an address on this computer or its network.
var errPrivate = errors.New("a private or local address isn't checked")

// Checker checks links, remembering what it found for a while.
type Checker struct {
	// AllowPrivate lets tests check a server on this computer. Toskar
	// never checks addresses on this computer or its network.
	AllowPrivate bool

	once   sync.Once
	client *http.Client
	mu     sync.Mutex
	cache  map[string]cached
	now    func() time.Time
}

type cached struct {
	status Status
	at     time.Time
}

func (c *Checker) init() {
	c.once.Do(func() {
		dialer := &net.Dialer{Timeout: perLink}
		dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
			conn, err := dialer.DialContext(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			// Checked after the name is looked up, so a public name that
			// points inside is refused too.
			if !c.AllowPrivate {
				if ap, perr := netip.ParseAddrPort(conn.RemoteAddr().String()); perr == nil && private(ap.Addr()) {
					conn.Close()
					return nil, errPrivate
				}
			}
			return conn, nil
		}
		c.client = &http.Client{
			Transport: &http.Transport{DialContext: dial, TLSHandshakeTimeout: perLink, ResponseHeaderTimeout: perLink},
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return http.ErrUseLastResponse
				}
				return nil
			},
		}
		c.cache = map[string]cached{}
		if c.now == nil {
			c.now = time.Now
		}
	})
}

// Check checks up to MaxLinks links at once and says what each is. Links
// it doesn't check, such as one on this computer's network, aren't in the
// result. When no request reached any site, as when this computer is
// offline, nothing is called dead.
// sent, when set, is told each host a request goes to, for What left this
// computer.
func (c *Checker) Check(ctx context.Context, links []string, sent func(host string)) map[string]Status {
	c.init()
	ctx, cancel := context.WithTimeout(ctx, perAnswer)
	defer cancel()
	out := map[string]Status{}
	var todo []string
	seen := map[string]bool{}
	for _, l := range links {
		if seen[l] || len(todo)+len(out) >= MaxLinks {
			continue
		}
		seen[l] = true
		u, err := url.Parse(l)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || c.localName(u.Hostname()) {
			continue
		}
		if s, ok := c.remembered(l); ok {
			out[l] = s
			continue
		}
		todo = append(todo, l)
	}
	type found struct {
		link    string
		status  Status
		reached bool
	}
	results := make(chan found, len(todo))
	for _, l := range todo {
		go func(l string) {
			s, reached := c.one(ctx, l, sent)
			results <- found{l, s, reached}
		}(l)
	}
	var got []found
	anyReached := false
	for range todo {
		f := <-results
		got = append(got, f)
		anyReached = anyReached || f.reached
	}
	for _, f := range got {
		s := f.status
		if s == Dead && !f.reached && !anyReached {
			// Nothing answered: this computer, not the sites, may be
			// offline (#965).
			s = Unknown
		}
		out[f.link] = s
		if s != Unknown {
			c.remember(f.link, s)
		}
	}
	return out
}

// one checks a link: HEAD, then a one-byte GET where HEAD isn't allowed.
// reached says a server answered.
func (c *Checker) one(ctx context.Context, link string, sent func(string)) (Status, bool) {
	status, err := c.request(ctx, http.MethodHead, link, sent)
	if err == nil && (status == http.StatusMethodNotAllowed || status == http.StatusNotImplemented) {
		status, err = c.request(ctx, http.MethodGet, link, sent)
	}
	if err != nil {
		if errors.Is(err, errPrivate) {
			return Unknown, false
		}
		var dns *net.DNSError
		if (errors.As(err, &dns) && dns.IsNotFound) || errors.Is(err, syscall.ECONNREFUSED) {
			return Dead, false
		}
		return Unknown, false
	}
	switch {
	case status == http.StatusNotFound || status == http.StatusGone:
		return Dead, true
	case status >= 200 && status < 400:
		return Open, true
	}
	return Unknown, true
}

func (c *Checker) request(ctx context.Context, method, link string, sent func(string)) (int, error) {
	req, err := http.NewRequestWithContext(ctx, method, link, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "Toskar link check")
	if method == http.MethodGet {
		req.Header.Set("Range", "bytes=0-0")
	}
	if sent != nil {
		sent(req.URL.Hostname())
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("%s %s: %w", method, req.URL.Hostname(), err)
	}
	resp.Body.Close()
	return resp.StatusCode, nil
}

// localName is a name on this computer or its network, checked before any
// request: localhost, a .local or .internal name, or a private address.
func (c *Checker) localName(host string) bool {
	if c.AllowPrivate {
		return false
	}
	h := strings.ToLower(strings.TrimSuffix(host, "."))
	if h == "localhost" || strings.HasSuffix(h, ".localhost") || strings.HasSuffix(h, ".local") || strings.HasSuffix(h, ".internal") || strings.HasSuffix(h, ".lan") || !strings.Contains(h, ".") {
		return true
	}
	if a, err := netip.ParseAddr(h); err == nil {
		return private(a)
	}
	return false
}

func private(a netip.Addr) bool {
	a = a.Unmap()
	return a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() || a.IsUnspecified() || a.IsMulticast() ||
		netip.MustParsePrefix("100.64.0.0/10").Contains(a)
}

func (c *Checker) remembered(link string) (Status, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.cache[link]
	if !ok || c.now().Sub(e.at) > cacheFor {
		return "", false
	}
	return e.status, true
}

func (c *Checker) remember(link string, s Status) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache[link] = cached{status: s, at: c.now()}
}

// Forget clears what was remembered, such as when chats are deleted.
func (c *Checker) Forget() {
	c.init()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache = map[string]cached{}
}
