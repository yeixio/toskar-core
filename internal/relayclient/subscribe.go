package relayclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// A subscription bought from this computer's own settings, with Stripe,
// rather than in a phone's store (#639): the relay checks the Checkout
// Session with Stripe and answers a route token for it, as it does for a
// store purchase (toskar-relay's docs/protocol.md,
// POST /v1/subscriptions/stripe). These need only Relay, RootCAs, and Dial.

// ExchangeStripe trades a Checkout Session for a route token for route.
// While the person is still paying the relay answers CHECKOUT_PENDING,
// which ErrorCode reports, as it does SUBSCRIPTION_INACTIVE once the
// subscription has ended.
func (c *Client) ExchangeStripe(ctx context.Context, route, session string) (string, error) {
	body, _ := json.Marshal(map[string]string{"route": route, "checkout_session": session})
	resp, err := c.do(ctx, http.MethodPost, "/v1/subscriptions/stripe", "", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<10)).Decode(&out); err != nil || out.Token == "" {
		return "", errors.New("the relay's answer had no token")
	}
	return out.Token, nil
}

// Availability reports whether the relay takes new subscriptions now: a
// full relay says no, rather than selling one it can't serve.
func (c *Client) Availability(ctx context.Context) (bool, error) {
	resp, err := c.do(ctx, http.MethodGet, "/v1/subscriptions/availability", "", nil)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	var out struct {
		Open bool `json:"open"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<10)).Decode(&out); err != nil {
		return false, err
	}
	return out.Open, nil
}

// ErrorCode is the relay's error code in err, or RELAY_UNREACHABLE when
// the relay didn't answer with one.
func ErrorCode(err error) string { return errorCode(err) }
