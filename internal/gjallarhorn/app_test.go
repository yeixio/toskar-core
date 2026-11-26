package gjallarhorn

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/auth"
)

type fakePush struct {
	devices map[string]int
	got     []PushMessage
	to      []string
}

func (f *fakePush) Host() string { return "relay.example" }

func (f *fakePush) SendToPerson(_ context.Context, personID string, m PushMessage) (int, error) {
	f.to = append(f.to, personID)
	f.got = append(f.got, m)
	return f.devices[personID], nil
}

// An app destination pushes to the person's devices: by default the title,
// which the person's own automation wrote, and never the text unless they
// chose full content (§11, #460).
func TestAppPush(t *testing.T) {
	hub, eg, _ := deliveryHub(t)
	push := &fakePush{devices: map[string]int{auth.OwnerID: 2}}
	hub.SetPush(push)
	ctx := context.Background()
	d, _, err := hub.CreateDestination(ctx, DestinationInput{Kind: KindApp, Name: ptr("My phone and tablet")})
	if err != nil {
		t.Fatal(err)
	}
	if d.App == nil || d.App.Content != "title" {
		t.Fatalf("destination %+v", d)
	}
	n, _ := hub.Notify(ctx, Request{Title: "Back in stock: Trail runner, size 10", Body: "The size 10 is in stock at $89.", Severity: SeverityWarning, Link: "/automations?id=a1"})
	hub.DeliverDue(ctx)
	if dl := deliveryOf(t, hub, n.ID); dl.Status != DeliveryDelivered {
		t.Fatalf("delivery %+v", dl)
	}
	m := push.got[0]
	if m.Title != "Back in stock: Trail runner, size 10" || m.Body != "" || m.Link != "/automations?id=a1" || m.NotificationID != n.ID {
		t.Fatalf("pushed %+v", m)
	}
	if len(eg.adds) != 1 || !strings.Contains(eg.adds[0], "relay.example") || !strings.Contains(eg.adds[0], "Back in stock") {
		t.Fatalf("egress %v", eg.adds)
	}

	full := "full"
	if _, err := hub.UpdateDestination(ctx, d.ID, DestinationInput{App: &AppConfig{Content: full}}); err != nil {
		t.Fatal(err)
	}
	_ = hub.TestDestination(ctx, d.ID)
	if m := push.got[len(push.got)-1]; m.Body == "" {
		t.Fatalf("full content left out the text: %+v", m)
	}

	if _, err := hub.UpdateDestination(ctx, d.ID, DestinationInput{App: &AppConfig{Content: "private"}}); err != nil {
		t.Fatal(err)
	}
	_ = hub.TestDestination(ctx, d.ID)
	if m := push.got[len(push.got)-1]; m.Title != "Toskar" || strings.Contains(m.Body, "Test") {
		t.Fatalf("private push carried content: %+v", m)
	}
	if last := eg.adds[len(eg.adds)-1]; strings.Contains(last, "Test") {
		t.Fatalf("a private push recorded its title as sent: %q", last)
	}

	if _, err := hub.UpdateDestination(ctx, d.ID, DestinationInput{App: &AppConfig{Content: "loud"}}); err == nil {
		t.Fatal("an unknown content setting was taken")
	}
}

// With no device that takes pushes, the delivery says so and isn't retried.
func TestAppPushWithoutDevices(t *testing.T) {
	hub, eg, _ := deliveryHub(t)
	hub.SetPush(&fakePush{})
	ctx := context.Background()
	d, _, err := hub.CreateDestination(ctx, DestinationInput{Kind: KindApp, Name: ptr("Phones")})
	if err != nil {
		t.Fatal(err)
	}
	err = hub.TestDestination(ctx, d.ID)
	if !IsPermanent(err) || !strings.Contains(err.Error(), "turned on") {
		t.Fatalf("no devices: %v", err)
	}
	// Nothing went to the relay, so What left this computer doesn't say so.
	if len(eg.adds) != 0 {
		t.Fatalf("egress for a push no device took: %v", eg.adds)
	}
}

// A notice's web page is kept and sent with its title, so a tap opens the
// page an automation watched without reaching the computer, and a private
// push leaves it out, since it says what was watched (#460).
func TestAppPushCarriesThePage(t *testing.T) {
	hub, _, _ := deliveryHub(t)
	push := &fakePush{devices: map[string]int{auth.OwnerID: 1}}
	hub.SetPush(push)
	ctx := context.Background()
	d, _, err := hub.CreateDestination(ctx, DestinationInput{Kind: KindApp, Name: ptr("My phone")})
	if err != nil {
		t.Fatal(err)
	}
	const page = "https://shop.example/trail-runner?size=10"
	n, err := hub.Notify(ctx, Request{Title: "Back in stock: Trail runner, size 10", Severity: SeverityWarning, Link: "/automations?id=a1", URL: page})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := hub.Get(ctx, n.ID); err != nil || got.URL != page {
		t.Fatalf("stored url %q, %v", got.URL, err)
	}
	hub.DeliverDue(ctx)
	if m := push.got[0]; m.URL != page || m.Link != "/automations?id=a1" {
		t.Fatalf("pushed %+v", m)
	}
	if _, err := hub.UpdateDestination(ctx, d.ID, DestinationInput{App: &AppConfig{Content: "private"}}); err != nil {
		t.Fatal(err)
	}
	if m := pushMessageFor(AppConfig{Content: "private"}, n); m.URL != "" {
		t.Fatalf("a private push carried the page: %+v", m)
	}
}

// A person's own notice reaches only their own phones, through the app
// destination; never the desktop, a webhook, or a digest. Their Notify when
// tasks finish turns their automations' pushes off (#1391).
func TestPersonalNoticesPushOnlyToTheirPerson(t *testing.T) {
	hub, _, _ := deliveryHub(t)
	desktop := &recordingChannel{name: "desktop"}
	hub.channels["desktop"] = desktop
	push := &fakePush{devices: map[string]int{"sam": 1, auth.OwnerID: 1}}
	hub.SetPush(push)
	settings := &memSettings{m: map[string]string{}}
	hub.SetSettings(settings)
	ctx := context.Background()
	if _, _, err := hub.CreateDestination(ctx, DestinationInput{Kind: KindApp, Name: ptr("Phones")}); err != nil {
		t.Fatal(err)
	}
	hook := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hook++ }))
	t.Cleanup(srv.Close)
	if _, _, err := hub.CreateDestination(ctx, DestinationInput{Kind: KindWebhook, Name: ptr("Hook"), Webhook: &WebhookConfig{URL: srv.URL}}); err != nil {
		t.Fatal(err)
	}
	sam := auth.AsPerson(ctx, auth.Person{ID: "sam"})
	if _, err := hub.Notify(sam, Request{Category: CategoryAutomation, Title: "Sam's report", Channels: []string{"desktop"}}); err != nil {
		t.Fatal(err)
	}
	hub.DeliverDue(ctx)
	if len(push.to) != 1 || push.to[0] != "sam" || push.got[0].Title != "Sam's report" {
		t.Fatalf("pushed to %v: %+v", push.to, push.got)
	}
	if len(desktop.got) != 0 || hook != 0 {
		t.Fatalf("Sam's notice left for the desktop (%d) or a webhook (%d)", len(desktop.got), hook)
	}
	settings.m["notify_task_finish@sam"] = "false"
	if _, err := hub.Notify(sam, Request{Category: CategoryAutomation, Title: "Sam's second report"}); err != nil {
		t.Fatal(err)
	}
	hub.DeliverDue(ctx)
	if len(push.got) != 1 {
		t.Fatalf("pushed with Sam's notices off: %+v", push.got)
	}
}
