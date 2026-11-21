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
	return c.exchangeStripe(ctx, route, session, false, nil)
}

// MoveStripe trades the session for a token for route, moving the
// subscription here from the route it's tied to (toskar-core#1257). tokens
// are ones this computer kept, which may be for the old route: the relay
// moves it only with one of those, or once the old route has been quiet
// for 3 days (toskar-relay#72).
func (c *Client) MoveStripe(ctx context.Context, route, session string, tokens []string) (string, error) {
	return c.exchangeStripe(ctx, route, session, true, tokens)
}

func (c *Client) exchangeStripe(ctx context.Context, route, session string, move bool, tokens []string) (string, error) {
	req := map[string]any{"route": route, "checkout_session": session}
	if move {
		req["move"] = true
		if len(tokens) > 0 {
			req["tokens"] = tokens
		}
	}
	body, _ := json.Marshal(req)
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

// Covered asks the relay whether a subscription already covers route,
// bought from any device.
func (c *Client) Covered(ctx context.Context, route string) (bool, error) {
	resp, err := c.do(ctx, http.MethodGet, "/v1/routes/"+route+"/coverage", "", nil)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	var out struct {
		Covered bool `json:"covered"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<10)).Decode(&out); err != nil {
		return false, err
	}
	return out.Covered, nil
}

// CheckToken asks the relay whether it takes tok for route, by reading the
// route's usage with it, which any current token for the route may.
func (c *Client) CheckToken(ctx context.Context, route, tok string) error {
	resp, err := c.do(ctx, http.MethodGet, "/v1/routes/"+route+"/usage", tok, nil)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// Refused reports whether err is the relay refusing a token: expired,
// revoked, unsigned, or for another route or a blocked one.
func Refused(err error) bool {
	var ae *apiError
	return errors.As(err, &ae) && (ae.Status == http.StatusUnauthorized || ae.Status == http.StatusForbidden)
}

// ErrorCode is the relay's error code in err, or RELAY_UNREACHABLE when
// the relay didn't answer with one.
func ErrorCode(err error) string { return errorCode(err) }
