package gjallarhorn

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// KindApp is push to the Toskar apps on the person's paired phones and
// tablets, through Toskar's push relay (#460, docs/push.md). The watch shows
// what the phone gets.
const KindApp = "app"

// AppConfig is what an app push carries (§11). Content is title (the
// default): the notification's title, which the person's own automation or
// Toskar wrote, with the text left on the computer; full: the title and
// text; or private: only "You have a new Toskar notification".
type AppConfig struct {
	Content string `json:"content,omitempty"`
}

// PushMessage is one push to a person's devices.
type PushMessage struct {
	Title          string
	Body           string
	Link           string
	NotificationID string
	Category       string
	Severity       string
	Urgent         bool
}

// PushSender sends a push to every device of a person's that has turned
// pushes on, and says how many took it.
type PushSender interface {
	SendToPerson(ctx context.Context, personID string, m PushMessage) (int, error)
	// Host is the relay's host, for What left this computer.
	Host() string
}

// ErrNoDevices is a person with no device that takes pushes yet.
var ErrNoDevices = errors.New("no phone or tablet has notifications turned on in the Toskar app yet")

// SetPush sends app destinations' pushes.
func (h *Hub) SetPush(p PushSender) { h.push = p }

func applyAppDefaults(c *AppConfig) {
	c.Content = strings.TrimSpace(c.Content)
	if c.Content == "" {
		c.Content = "title"
	}
}

func validateApp(c AppConfig) error {
	switch c.Content {
	case "title", "full", "private":
		return nil
	}
	return fmt.Errorf("content must be title, full, or private")
}

// pushMessageFor is n as a push, by what the destination lets through.
func pushMessageFor(c AppConfig, n Notification) PushMessage {
	m := PushMessage{
		Title: "Toskar", Body: n.text("notifications:sent.privatePush", nil),
		Link: n.Link, NotificationID: n.ID, Category: n.Category, Severity: n.Severity,
		// An error goes out at once and breaks through Focus (§6).
		Urgent: n.Severity == SeverityError,
	}
	if c.Content == "private" {
		return m
	}
	m.Title = n.Title
	if n.RepeatCount > 1 {
		m.Title = n.text("notifications:sent.times", map[string]any{"title": n.Title, "count": n.RepeatCount})
	}
	m.Body = ""
	if c.Content == "full" {
		m.Body = n.Body
	}
	return m
}

func (h *Hub) sendApp(ctx context.Context, c AppConfig, n Notification) error {
	if h.push == nil {
		return PermanentError{Err: errors.New("push to the Toskar apps isn't available on this computer")}
	}
	sent, err := h.push.SendToPerson(ctx, n.PersonID, pushMessageFor(c, n))
	if err != nil {
		return err
	}
	if sent == 0 {
		return PermanentError{Err: ErrNoDevices}
	}
	return nil
}
