package app

import (
	"context"
	"errors"
	"net/url"
	"os"

	"github.com/yeixio/toskar-core/internal/gjallarhorn"
	"github.com/yeixio/toskar-core/internal/push"
)

// pushSender sends app destinations' pushes (#460) to a person's devices
// through the relay, and forgets a device the relay says is gone.
type pushSender struct {
	store  *push.Store
	client *push.Client
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

// SendToPerson sends to each of the person's devices. It counts as sent when
// any device took it, so a retry doesn't repeat it on the others.
func (p *pushSender) SendToPerson(ctx context.Context, personID string, m gjallarhorn.PushMessage) (int, error) {
	regs, err := p.store.ForPerson(ctx, personID)
	if err != nil {
		return 0, err
	}
	sent := 0
	var last error
	for _, r := range regs {
		err := p.client.Send(ctx, push.Message{
			Handle: r.Handle, Title: m.Title, Body: m.Body, Link: m.Link, NotificationID: m.NotificationID,
			Category: m.Category, Severity: m.Severity, Collapse: m.NotificationID, Urgent: m.Urgent,
		})
		switch {
		case err == nil:
			sent++
		case errors.Is(err, push.ErrGone):
			_ = p.store.Remove(ctx, r.KeyID)
		default:
			last = err
		}
	}
	if sent > 0 || last == nil {
		return sent, nil
	}
	var re *push.RelayError
	if errors.As(last, &re) && re.Permanent() {
		return 0, gjallarhorn.PermanentError{Err: last}
	}
	return 0, last
}
