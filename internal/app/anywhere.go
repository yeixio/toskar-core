package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/yeixio/toskar-core/internal/api"
	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/internal/pyenv"
	"github.com/yeixio/toskar-core/internal/relayclient"
	"github.com/yeixio/toskar-core/internal/rendezvous"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Access from anywhere bought here, in Settings, rather than in a phone's
// store (#639): yeix.io, which bills for Toskar, opens a Stripe checkout
// for this computer's route; Toskar keeps the Checkout Session's ID and
// trades it with Toskar's relay for a route token once it's paid, and
// again as the subscription renews. The session's ID is the proof of
// purchase, as a store's signed receipt is on a phone, so it's kept with
// the other secrets: as the open checkout until the relay takes it, then
// as the subscription's. A checkout never paid is forgotten.

const (
	// stripeSessionName is the paid Checkout Session's ID in the secret
	// store, and stripeCheckoutName an open one's.
	stripeSessionName  = "relay-stripe-session"
	stripeCheckoutName = "relay-stripe-checkout"
	// defaultBilling is where checkouts and the billing page come from;
	// TOSKAR_BILLING_URL replaces it, for a test.
	defaultBilling = "https://yeix.io"
	// checkoutQuick is how long after a checkout opens the relay is asked
	// every few seconds, so access from anywhere turns on soon after
	// paying; after it, every minute until the checkout expires.
	checkoutQuick = 30 * time.Minute
	// renewBefore is how long before the token runs out it's renewed.
	anywhereRenewBefore = 7 * 24 * time.Hour
	// availabilityFor is how long the relay's answer to whether it takes
	// new subscriptions is kept.
	availabilityFor = 10 * time.Minute
)

var (
	errPlanInvalid  = contracts.NewError("SUBSCRIPTION_PLAN_INVALID", nil, errors.New("the plan is monthly or yearly"))
	errOwnRelay     = contracts.NewError("SUBSCRIPTION_OWN_RELAY", nil, errors.New("this computer uses its organization's relay; a subscription is for Toskar's relay"))
	errActive       = contracts.NewError("SUBSCRIPTION_ACTIVE", nil, errors.New("this computer already has access from anywhere"))
	errNoneHere     = contracts.NewError("SUBSCRIPTION_NONE", nil, errors.New("this computer has no subscription bought in its settings; one bought in the Toskar app is managed in the app's store"))
	errSoldOut      = contracts.NewError("RELAY_SOLD_OUT", nil, errors.New("the relay isn't taking new subscriptions right now"))
	errInAppOnly    = contracts.NewError("SUBSCRIPTION_IN_APP_ONLY", nil, errors.New("this copy of Toskar is from the App Store, so the subscription is sold in the Toskar app on a phone or tablet"))
	errBillingReach = func(cause error) error {
		return contracts.NewError("BILLING_UNAVAILABLE", nil, fmt.Errorf("billing couldn't be reached; try again in a minute: %w", cause))
	}
)

// anywhere is the subscription bought here, as Toskar last found it.
type anywhere struct {
	mu sync.Mutex
	// checkout is the open checkout's page and when it was opened and
	// expires, while it's open in this run.
	checkout        string
	opened, expires time.Time
	// ended is the subscription bought here having ended.
	ended     bool
	lastError string
	open      *bool
	openAt    time.Time
	asking    bool
	wake      chan struct{}
	// For tests: whether this is the App Store's copy, yeix.io, its client,
	// the relay, and the clock.
	inAppOnly    *bool
	billing      string
	httpClient   *http.Client
	relayForTest *relayclient.Client
	nowForTest   func() time.Time
}

func (s *anywhere) now() time.Time {
	if s.nowForTest != nil {
		return s.nowForTest()
	}
	return time.Now()
}

// inAppOnly is whether this copy of Toskar may only point to the Toskar
// app for the subscription: the Mac App Store's, which runs in the App
// Sandbox, where a digital subscription is sold with in-app purchase
// (App Review Guideline 3.1.1). A token the app bought still works here.
func (a *App) inAppOnly() bool {
	if a.anywhere.inAppOnly != nil {
		return *a.anywhere.inAppOnly
	}
	return pyenv.Sandboxed()
}

func (a *App) billingURL() string {
	if a.anywhere.billing != "" {
		return a.anywhere.billing
	}
	if u := strings.TrimSuffix(strings.TrimSpace(config.Env("BILLING_URL")), "/"); u != "" {
		return u
	}
	return defaultBilling
}

// relayAPI is a relay client for the subscription calls, whether or not
// access from anywhere is on.
func (a *App) relayAPI() *relayclient.Client {
	if a.anywhere.relayForTest != nil {
		return a.anywhere.relayForTest
	}
	return &relayclient.Client{Relay: relayName(a.Config.Get()), RootCAs: relayRoots(a)}
}

// wakeAnywhere asks the loop to look again now.
func (a *App) wakeAnywhere() {
	a.anywhere.mu.Lock()
	ch := a.anywhere.wake
	a.anywhere.mu.Unlock()
	if ch != nil {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// billingCall posts to yeix.io's Toskar billing API and decodes its answer.
func (a *App) billingCall(ctx context.Context, path string, body, out any) error {
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.billingURL()+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := a.anywhere.httpClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return errBillingReach(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode >= 300 {
		var e struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(data, &e)
		if resp.StatusCode == http.StatusNotFound && e.Error.Code == "NOT_FOUND" {
			return errNoneHere
		}
		return errBillingReach(fmt.Errorf("%d %s %s", resp.StatusCode, e.Error.Code, e.Error.Message))
	}
	if err := json.Unmarshal(data, out); err != nil {
		return errBillingReach(err)
	}
	return nil
}

// routeID is this computer's route on the relay.
func (a *App) routeID() (string, error) {
	secret, err := rendezvous.RouteSecret(auth.NewSecretStore(a.Config.Get().DataDir))
	if err != nil {
		return "", err
	}
	return rendezvous.RouteID(secret), nil
}

// startCheckout opens a Stripe checkout for access from anywhere on this
// computer, monthly or yearly, and answers its page.
func (a *App) startCheckout(ctx context.Context, plan string) (string, error) {
	if a.inAppOnly() {
		return "", errInAppOnly
	}
	if plan != "monthly" && plan != "yearly" {
		return "", errPlanInvalid
	}
	cfg := a.Config.Get()
	if cfg.RemoteAccess.Relay != "" {
		return "", errOwnRelay
	}
	if a.relayToken() != "" {
		return "", errActive
	}
	if open := a.relayOpen(ctx); open != nil && !*open {
		return "", errSoldOut
	}
	route, err := a.routeID()
	if err != nil {
		return "", err
	}
	var out struct {
		URL       string `json:"url"`
		SessionID string `json:"session_id"`
		Expires   int64  `json:"expires"`
	}
	if err := a.billingCall(ctx, "/api/toskar/anywhere/checkout", map[string]string{"route": route, "plan": plan}, &out); err != nil {
		return "", err
	}
	if out.URL == "" || !strings.HasPrefix(out.SessionID, "cs_") {
		return "", errBillingReach(errors.New("the checkout answer had no session"))
	}
	if err := auth.NewSecretStore(cfg.DataDir).Write(stripeCheckoutName, out.SessionID); err != nil {
		return "", err
	}
	now := a.anywhere.now()
	expires := time.Unix(out.Expires, 0)
	if out.Expires == 0 {
		expires = now.Add(24 * time.Hour)
	}
	a.anywhere.mu.Lock()
	a.anywhere.checkout, a.anywhere.opened, a.anywhere.expires = out.URL, now, expires
	a.anywhere.ended, a.anywhere.lastError = false, ""
	a.anywhere.mu.Unlock()
	a.Logger.Info("access from anywhere: checkout opened", "plan", plan)
	a.wakeAnywhere()
	return out.URL, nil
}

// manageSubscription answers the billing page for the subscription bought
// here.
func (a *App) manageSubscription(ctx context.Context) (string, error) {
	if a.inAppOnly() {
		return "", errInAppOnly
	}
	session := a.stripeSession()
	if session == "" {
		return "", errNoneHere
	}
	var out struct {
		URL string `json:"url"`
	}
	if err := a.billingCall(ctx, "/api/toskar/anywhere/portal", map[string]string{"checkout_session": session}, &out); err != nil {
		return "", err
	}
	return out.URL, nil
}

// stripeSession is the paid Checkout Session's ID, or "".
func (a *App) stripeSession() string { return a.readSecret(stripeSessionName) }

// stripeCheckout is the open checkout's session ID, or "".
func (a *App) stripeCheckout() string { return a.readSecret(stripeCheckoutName) }

func (a *App) readSecret(name string) string {
	s, err := auth.NewSecretStore(a.Config.Get().DataDir).Read(name)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(s)
}

// relayOpen is whether Toskar's relay takes new subscriptions, asked at
// most every availabilityFor; nil when it couldn't be asked.
func (a *App) relayOpen(ctx context.Context) *bool {
	a.anywhere.mu.Lock()
	if a.anywhere.open != nil && a.anywhere.now().Sub(a.anywhere.openAt) < availabilityFor {
		open := *a.anywhere.open
		a.anywhere.mu.Unlock()
		return &open
	}
	a.anywhere.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	open, err := a.relayAPI().Availability(ctx)
	if err != nil {
		return nil
	}
	a.anywhere.mu.Lock()
	a.anywhere.open, a.anywhere.openAt = &open, a.anywhere.now()
	a.anywhere.mu.Unlock()
	return &open
}

// subscription is access from anywhere on Toskar's relay as this computer
// has it, for API Access; nil with an organization's relay.
func (a *App) subscription() *api.Subscription {
	cfg := a.Config.Get()
	if cfg.RemoteAccess.Relay != "" {
		return nil
	}
	out := &api.Subscription{State: "none", InAppOnly: a.inAppOnly()}
	session := a.stripeSession()
	if tok := a.relayToken(); tok != "" {
		_, exp, _ := relayclient.ParseToken(tok)
		out.State, out.Expires, out.Store = "active", exp, "app"
		if session != "" {
			out.Store = "stripe"
		}
		return out
	}
	pending := a.stripeCheckout() != ""
	a.anywhere.mu.Lock()
	defer a.anywhere.mu.Unlock()
	switch {
	case pending:
		out.State, out.Store = "pending", "stripe"
		if a.anywhere.now().Before(a.anywhere.expires) {
			out.Checkout = a.anywhere.checkout
		}
	case session != "" && a.anywhere.ended:
		out.State, out.Store = "ended", "stripe"
	}
	// A token that ran out, from the app's store or a Stripe subscription
	// that wasn't renewed, is a subscription that ended, on the day it
	// ran out (#692).
	if out.State == "none" || out.State == "ended" {
		if exp := a.expiredRelayToken(); !exp.IsZero() {
			out.State, out.Expires, out.Store = "ended", exp, "app"
			if session != "" {
				out.Store = "stripe"
			}
		}
	}
	out.Open, out.Error = a.anywhere.open, a.anywhere.lastError
	// Whether the relay takes new subscriptions is asked only when Settings
	// offers one (none yet, or one ended), never on its own, and answered
	// on the next look; never by the App Store's copy, which doesn't offer
	// it.
	if (out.State == "none" || out.State == "ended") && !out.InAppOnly && !a.anywhere.asking && (a.anywhere.open == nil || a.anywhere.now().Sub(a.anywhere.openAt) >= availabilityFor) {
		a.anywhere.asking = true
		go func() {
			a.relayOpen(context.Background())
			a.anywhere.mu.Lock()
			a.anywhere.asking = false
			a.anywhere.mu.Unlock()
		}()
	}
	return out
}

// anywhereLoop trades the Checkout Session for a token once it's paid, and
// again before each token runs out.
func (a *App) anywhereLoop(ctx context.Context) {
	wake := make(chan struct{}, 1)
	a.anywhere.mu.Lock()
	a.anywhere.wake = wake
	a.anywhere.mu.Unlock()
	for {
		next := a.anywhereTick(ctx)
		timer := time.NewTimer(next)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-wake:
			timer.Stop()
		case <-timer.C:
		}
	}
}

// anywhereTick looks once, and says when to look again.
func (a *App) anywhereTick(ctx context.Context) time.Duration {
	if a.Config.Get().RemoteAccess.Relay != "" {
		return time.Hour
	}
	if checkout := a.stripeCheckout(); checkout != "" {
		return a.collectCheckout(ctx, checkout)
	}
	session := a.stripeSession()
	if session == "" {
		return time.Hour
	}
	now := a.anywhere.now()
	if tok := a.relayToken(); tok != "" {
		if _, exp, _ := relayclient.ParseToken(tok); exp.Sub(now) > anywhereRenewBefore {
			return min(exp.Sub(now)-anywhereRenewBefore, 12*time.Hour)
		}
	}
	tok, code := a.exchange(ctx, session)
	a.anywhere.mu.Lock()
	a.anywhere.ended = code == "SUBSCRIPTION_INACTIVE"
	a.anywhere.lastError = ""
	if code != "" && code != "SUBSCRIPTION_INACTIVE" {
		a.anywhere.lastError = code
	}
	a.anywhere.mu.Unlock()
	switch code {
	case "":
		a.keepToken(ctx, tok, false)
	case "SUBSCRIPTION_INACTIVE":
		return 24 * time.Hour
	}
	return time.Hour
}

// collectCheckout asks the relay for the open checkout's token: once it's
// paid, it becomes the subscription's session and access from anywhere
// turns on. One the relay says can't buy anything, such as a checkout that
// expired unpaid, is forgotten.
func (a *App) collectCheckout(ctx context.Context, checkout string) time.Duration {
	tok, code := a.exchange(ctx, checkout)
	secrets := auth.NewSecretStore(a.Config.Get().DataDir)
	now := a.anywhere.now()
	a.anywhere.mu.Lock()
	quick := now.Sub(a.anywhere.opened) < checkoutQuick
	a.anywhere.lastError = ""
	done := false
	switch code {
	case "":
		a.anywhere.checkout, a.anywhere.ended, done = "", false, true
	case "CHECKOUT_PENDING":
	case "SUBSCRIPTION_INACTIVE", "SUBSCRIPTION_INVALID", "SUBSCRIPTION_OTHER":
		a.anywhere.checkout, done = "", true
	default:
		a.anywhere.lastError = code
	}
	a.anywhere.mu.Unlock()
	if code == "" {
		if err := secrets.Write(stripeSessionName, checkout); err != nil {
			a.Logger.Warn("access from anywhere: keeping the subscription", "error", err)
			return time.Minute
		}
		// What was bought turns access from anywhere on.
		a.keepToken(ctx, tok, true)
	} else if done {
		a.Logger.Info("access from anywhere: checkout closed without paying", "code", code)
	}
	if done {
		_ = secrets.Delete(stripeCheckoutName)
		return time.Hour
	}
	if quick {
		return 5 * time.Second
	}
	return time.Minute
}

// exchange trades a session for a token; code is the relay's error code,
// "" when it worked.
func (a *App) exchange(ctx context.Context, session string) (string, string) {
	route, err := a.routeID()
	if err != nil {
		return "", "ROUTE_FAILED"
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	tok, err := a.relayAPI().ExchangeStripe(ctx, route, session)
	if err != nil {
		return "", relayclient.ErrorCode(err)
	}
	return tok, ""
}

// keepToken keeps a token from the subscription, turning access from
// anywhere on with enable.
func (a *App) keepToken(ctx context.Context, tok string, enable bool) {
	if _, err := a.setRelayToken(ctx, tok, enable); err != nil {
		a.Logger.Warn("access from anywhere: keeping the relay token", "error", err)
		return
	}
	a.Logger.Info("access from anywhere: token from the subscription", "first", enable)
}
