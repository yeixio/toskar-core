package relayclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"
)

// The outside check (#546): the relay reaches this computer from the
// internet, through its tunnel and each direct way in, the way a paired
// device away from home does, so an Admin can tell whether access from
// anywhere works and, when it doesn't, which way fails and why. The relay
// side is toskar-relay's POST /v1/routes/{route}/check.

// Way is how one way in did from outside.
type Way struct {
	Address string `json:"address,omitempty"`
	OK      bool   `json:"ok"`
	// Reason is the relay's word for a failure: no_tunnel, not_public,
	// unresolved, refused, timeout, unreachable, tls, wrong_computer, http.
	Reason string `json:"reason,omitempty"`
	MS     int64  `json:"ms,omitempty"`
}

// CheckResult is one outside check.
type CheckResult struct {
	At     time.Time `json:"at"`
	Relay  Way       `json:"relay"`
	Direct []Way     `json:"direct"`
}

// Errors the check can end with before it reaches the computer.
var (
	// ErrCheckUnsupported is a relay from before the check.
	ErrCheckUnsupported = errors.New("CHECK_UNSUPPORTED")
	// ErrCheckTooSoon is a second check within the relay's minute.
	ErrCheckTooSoon = errors.New("CHECK_TOO_SOON")
)

// Check asks the relay to reach this computer from outside now.
func (c *Client) Check(ctx context.Context) (CheckResult, error) {
	cert, ok := c.Certificate()
	if !ok || len(cert.Certificate) == 0 {
		return CheckResult{}, errors.New("the API has no certificate")
	}
	tok, err := c.token(ctx, false)
	if err != nil {
		return CheckResult{}, err
	}
	sum := sha256.Sum256(cert.Certificate[0])
	body, _ := json.Marshal(map[string]any{"fingerprint": "sha256:" + hex.EncodeToString(sum[:]), "addresses": c.Addresses()})
	resp, err := c.do(ctx, http.MethodPost, "/v1/routes/"+c.Route()+"/check", tok, bytes.NewReader(body))
	var ae *apiError
	switch {
	case errors.As(err, &ae) && (ae.Status == http.StatusNotFound || ae.Status == http.StatusMethodNotAllowed):
		return CheckResult{}, ErrCheckUnsupported
	case errors.As(err, &ae) && ae.Code == "CHECK_TOO_SOON":
		return CheckResult{}, ErrCheckTooSoon
	case err != nil:
		return CheckResult{}, err
	}
	defer resp.Body.Close()
	var out CheckResult
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&out); err != nil {
		return CheckResult{}, err
	}
	out.At = time.Now()
	return out, nil
}

// CheckCode is a check's failure as a code for Settings: CHECK_UNSUPPORTED,
// CHECK_TOO_SOON, NO_TOKEN, the relay's own code, or RELAY_UNREACHABLE.
func CheckCode(err error) string {
	switch {
	case errors.Is(err, ErrCheckUnsupported):
		return "CHECK_UNSUPPORTED"
	case errors.Is(err, ErrCheckTooSoon):
		return "CHECK_TOO_SOON"
	case errors.Is(err, errNoToken):
		return "NO_TOKEN"
	}
	return errorCode(err)
}
