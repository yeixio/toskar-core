package app

import (
	"context"
	"errors"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/yeixio/toskar-core/internal/gjallarhorn"
	"github.com/yeixio/toskar-core/internal/push"
)

// pushSender sends app destinations' pushes (#460) to a person's devices
// through the relay, and forgets a device the relay says is gone.
type pushSender struct {
	store  *push.Store
	client *push.Client

	// delivered is which devices took which notification, so a retry goes
	// only to the ones that didn't (#1267); kept a day, past the outbox's
	// last retry.
	mu        sync.Mutex
	delivered map[deliveryKey]time.Time
	now       func() time.Time
}

type deliveryKey struct{ notification, key string }

// deliveredFor is how long a device's delivery is remembered.
const deliveredFor = 24 * time.Hour

func (p *pushSender) clock() time.Time {
	if p.now != nil {
		return p.now()
	}
	return time.Now()
}

// took reports whether the device already took this notification.
func (p *pushSender) took(notification, key string) bool {
	if notification == "" {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	at, ok := p.delivered[deliveryKey{notification, key}]
	return ok && p.clock().Sub(at) < deliveredFor
}

// tookNow notes that the device took this notification, and forgets old ones.
func (p *pushSender) tookNow(notification, key string) {
	if notification == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.clock()
	if p.delivered == nil {
		p.delivered = map[deliveryKey]time.Time{}
	}
	for k, at := range p.delivered {
		if now.Sub(at) >= deliveredFor {
			delete(p.delivered, k)
		}
	}
	p.delivered[deliveryKey{notification, key}] = now
}

// newPushClient is this computer's push client: Toskar's relay, or the one
// in TOSKAR_PUSH_RELAY, for testing against a relay of your own.
func newPushClient(a *App) *push.Client {
	if a.identity == nil {
		return nil
	}
	return &push.Client{Relay: os.Getenv("TOSKAR_PUSH_RELAY"), Key: a.identity.PrivateKey}
}

func (p *pushSender) Host() string {
	u, err := url.Parse(p.client.RelayURL())
	if err != nil {
		return ""
	}
	return u.Host
}

// SendToPerson sends to each of the person's devices that hasn't taken this
// notification yet. A device that failed for a passing reason makes it an
// error, so the outbox tries again, and the retry skips the devices that
// took it (#1267). The count is the devices that have it.
func (p *pushSender) SendToPerson(ctx context.Context, personID string, m gjallarhorn.PushMessage) (int, error) {
	regs, err := p.store.ForPerson(ctx, personID)
	if err != nil {
		return 0, err
	}
	sent := 0
	var last error
	for _, r := range regs {
		if p.took(m.NotificationID, r.KeyID) {
			sent++
			continue
		}
		err := p.client.Send(ctx, push.Message{
			Handle: r.Handle, Title: m.Title, Body: m.Body, Link: m.Link, URL: m.URL, NotificationID: m.NotificationID,
			Category: m.Category, Severity: m.Severity, Collapse: m.NotificationID, Urgent: m.Urgent,
		})
		switch {
		case err == nil:
			sent++
			p.tookNow(m.NotificationID, r.KeyID)
		case errors.Is(err, push.ErrGone):
			_ = p.store.Remove(ctx, r.KeyID)
		default:
			last = err
		}
	}
	if last == nil {
		return sent, nil
	}
	var re *push.RelayError
	if errors.As(last, &re) && re.Permanent() {
		if sent > 0 {
			// The others have it; this one won't take it however often it's sent.
			return sent, nil
		}
		return 0, gjallarhorn.PermanentError{Err: last}
	}
	// Without an ID to tell the devices apart, a retry would repeat it on
	// the ones that took it.
	if sent > 0 && m.NotificationID == "" {
		return sent, nil
	}
	return sent, last
}
