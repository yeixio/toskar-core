package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Subscription is access from anywhere on Toskar's relay, as this computer
// has it (#639): bought here in Settings, with Stripe, or in the Toskar app
// on a phone, with its store.
type Subscription struct {
	// State is none, pending (a checkout opened here isn't paid yet),
	// active, or ended (the subscription bought here has ended).
	State string `json:"state"`
	// Store is where it was bought: stripe here, app in the Toskar app,
	// or grant for access the relay's operator granted by hand.
	Store string `json:"store,omitempty"`
	// Reason is why it ended before its time: revoked (moved to another
	// computer, or refunded) or blocked (the relay's operator blocked this
	// computer), from the relay refusing its token (#1256).
	Reason  string    `json:"reason,omitempty"`
	Expires time.Time `json:"expires,omitzero"`
	// Open is whether Toskar's relay takes new subscriptions now; nil
	// before it's been asked.
	Open *bool `json:"open,omitempty"`
	// Checkout is the open checkout's page, while it's pending.
	Checkout string `json:"checkout,omitempty"`
	// Error is the last exchange's code when it didn't work, such as
	// RELAY_UNREACHABLE.
	Error string `json:"error,omitempty"`
	// InAppOnly is this copy being the Mac App Store's, which doesn't sell
	// or manage it: the Toskar app on a phone or tablet does (#630).
	InAppOnly bool `json:"in_app_only,omitempty"`
	// Relay is the relay the subscription is for when it isn't Toskar's
	// own, such as a developer's set with TOSKAR_RELAY_URL, so the page
	// doesn't call it Toskar's relay (#1266).
	Relay string `json:"relay,omitempty"`
	// Prices are what the checkout charges for each plan, monthly and
	// yearly, from billing; empty until billing has answered (#1257).
	Prices map[string]PlanPrice `json:"prices,omitempty"`
}

// PlanPrice is a plan's price: the amount in the currency's major unit,
// such as 4.99, and its ISO currency code.
type PlanPrice struct {
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
}

// handleSubscriptionCheckout starts a subscription to access from
// anywhere for this computer, paid with Stripe (#639), and answers the
// checkout page to open. Toskar watches for the payment and turns access
// from anywhere on when it's through.
func (s *Server) handleSubscriptionCheckout(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Plan string `json:"plan"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "invalid JSON", nil)
		return
	}
	if s.deps.StartCheckout == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Access from anywhere isn't available.", nil)
		return
	}
	url, err := s.deps.StartCheckout(r.Context(), body.Plan)
	if err != nil {
		writeSubscriptionErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"url": url})
}

// handleSubscriptionManage answers the billing page for the subscription
// bought here: change the card, download receipts, cancel.
func (s *Server) handleSubscriptionManage(w http.ResponseWriter, r *http.Request) {
	if s.deps.ManageSubscription == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Access from anywhere isn't available.", nil)
		return
	}
	url, err := s.deps.ManageSubscription(r.Context())
	if err != nil {
		writeSubscriptionErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"url": url})
}

// handleSubscriptionMove moves the subscription bought here onto this
// computer, when the relay says it's tied to another (#1257).
func (s *Server) handleSubscriptionMove(w http.ResponseWriter, r *http.Request) {
	if s.deps.MoveSubscription == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Access from anywhere isn't available.", nil)
		return
	}
	if err := s.deps.MoveSubscription(r.Context()); err != nil {
		writeSubscriptionErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleSubscriptionCancel forgets the checkout opened here, unpaid, so
// another plan can be chosen (#1257).
func (s *Server) handleSubscriptionCancel(w http.ResponseWriter, r *http.Request) {
	if s.deps.CancelCheckout == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Access from anywhere isn't available.", nil)
		return
	}
	if err := s.deps.CancelCheckout(r.Context()); err != nil {
		writeSubscriptionErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// subscriptionStatus is each subscription error's HTTP status.
var subscriptionStatus = map[string]int{
	"SUBSCRIPTION_COVERED":      http.StatusConflict,
	"CHECKOUT_NONE":             http.StatusNotFound,
	"MOVE_TOO_SOON":             http.StatusTooManyRequests,
	"MOVE_NEEDS_COMPUTER":       http.StatusForbidden,
	"MOVE_FAILED":               http.StatusBadGateway,
	"RELAY_UNREACHABLE":         http.StatusBadGateway,
	"SUBSCRIPTION_PLAN_INVALID": http.StatusBadRequest,
	"SUBSCRIPTION_OWN_RELAY":    http.StatusConflict,
	"SUBSCRIPTION_ACTIVE":       http.StatusConflict,
	"SUBSCRIPTION_IN_APP_ONLY":  http.StatusConflict,
	"SUBSCRIPTION_NONE":         http.StatusNotFound,
	"RELAY_SOLD_OUT":            http.StatusServiceUnavailable,
	"BILLING_UNAVAILABLE":       http.StatusBadGateway,
}

func writeSubscriptionErr(w http.ResponseWriter, err error) {
	code, _ := contracts.ErrorCode(err)
	if status, ok := subscriptionStatus[code]; ok {
		writeErrFrom(w, status, code, err)
		return
	}
	writeErr(w, http.StatusInternalServerError, "SUBSCRIPTION_FAILED", "Couldn't start the subscription.", map[string]any{"cause": err.Error()})
}
