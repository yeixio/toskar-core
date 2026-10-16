package gjallarhorn

import (
	"context"
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
	hub, _, _ := deliveryHub(t)
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
}
