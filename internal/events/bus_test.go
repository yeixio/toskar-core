package events

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/auth"
)

func TestBusPublishSubscribe(t *testing.T) {
	bus := NewBus(8)
	id, ch := bus.Subscribe()
	defer bus.Unsubscribe(id)

	bus.Publish(New(TaskCreated, map[string]any{"task_id": "t1"}))

	evt := <-ch
	if evt.Type != TaskCreated {
		t.Fatalf("expected %s, got %s", TaskCreated, evt.Type)
	}
	if evt.Payload["task_id"] != "t1" {
		t.Fatalf("unexpected payload: %#v", evt.Payload)
	}
	if evt.ID == "" {
		t.Fatal("expected event ID")
	}
}

func TestBusUnsubscribe(t *testing.T) {
	bus := NewBus(2)
	id, ch := bus.Subscribe()
	bus.Unsubscribe(id)
	_, ok := <-ch
	if ok {
		t.Fatal("expected closed channel")
	}
}

// Each person sees their own chats and tasks, and everyone sees the
// computers and models (#206).
func TestEventsAreVisibleToTheirPerson(t *testing.T) {
	cases := []struct {
		evt    Event
		person string
		want   bool
	}{
		{New(ChatToken, nil).For("sam"), "sam", true},
		{New(ChatToken, nil).For("sam"), "owner", false},
		{New(ToolRequested, nil).For("owner"), "sam", false},
		{New("memory.saved", nil).For("ada"), "ada", true},
		{New(AutomationCompleted, nil).For("ada"), "sam", false},
		{New(ChatToken, nil), "owner", true},
		{New(ChatToken, nil), "sam", false},
		{New(ModelLoadCompleted, nil).For("sam"), "owner", true},
		{New(NodeOnline, nil), "sam", true},
	}
	for _, c := range cases {
		if got := c.evt.VisibleTo(c.person, "owner"); got != c.want {
			t.Errorf("%s for %q seen by %q: %v, want %v", c.evt.Type, c.evt.Person, c.person, got, c.want)
		}
	}
	if evt, _ := json.Marshal(New(ChatToken, nil).For("sam")); strings.Contains(string(evt), "sam") {
		t.Fatalf("the person is sent: %s", evt)
	}
}

func TestPublishForTagsTheContextsPerson(t *testing.T) {
	bus := NewBus(4)
	_, ch := bus.Subscribe()
	bus.PublishFor(auth.AsPerson(context.Background(), auth.Person{ID: "sam"}), New(ChatToken, nil))
	bus.PublishFor(context.Background(), New(ChatToken, nil))
	if a, b := <-ch, <-ch; a.Person != "sam" || b.Person != auth.OwnerID {
		t.Fatalf("persons %q %q", a.Person, b.Person)
	}
}

// A note already on the context hears what an inner one does: a swarm hears
// its agent's turn, which keeps its own note (#1135).
func TestNotesNest(t *testing.T) {
	var outer, inner []string
	ctx := WithNote(context.Background(), func(e Event) { outer = append(outer, e.Type) })
	ctx = WithNote(ctx, func(e Event) { inner = append(inner, e.Type) })
	NewBus(1).PublishFor(ctx, New("tool.started", nil))
	if len(outer) != 1 || len(inner) != 1 {
		t.Fatalf("outer heard %v, inner %v", outer, inner)
	}
	if !Personal("swarm.started") {
		t.Error("a swarm's events are its person's")
	}
}
