// Package gjallarhorn is Yggdrasil's notification system
// (docs/features/gjallarhorn-notification-system.md). Every notice first
// becomes a durable notification in the in-app notification center; delivery
// channels such as desktop notifications are attempts recorded beside it.
package gjallarhorn

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/yeixio/yggdrasil-core/internal/events"
)

// Categories.
const (
	CategoryAutomation = "automation"
	CategoryApproval   = "approval"
	CategoryModel      = "model"
	CategoryTraining   = "training"
	CategoryHealth     = "health"
	CategorySystem     = "system"
)

// Severities.
const (
	SeverityInfo    = "info"
	SeveritySuccess = "success"
	SeverityWarning = "warning"
	SeverityError   = "error"
)

// Delivery statuses.
const (
	DeliveryDelivered  = "delivered"
	DeliveryFailed     = "failed"
	DeliverySuppressed = "suppressed"
)

// EventCreated tells clients a notification was stored.
const EventCreated = "notification.created"

// dedupeWindow is how long a repeated notice with the same key is folded
// into the first one instead of stored again.
const dedupeWindow = 10 * time.Minute

// Delivery is one channel's attempt to deliver a notification.
type Delivery struct {
	Channel     string     `json:"channel"`
	Status      string     `json:"status"`
	Attempts    int        `json:"attempts"`
	DeliveredAt *time.Time `json:"delivered_at,omitempty"`
	Error       string     `json:"error,omitempty"`
}

// Notification is a stored notice.
type Notification struct {
	ID         string    `json:"id"`
	CreatedAt  time.Time `json:"created_at"`
	SourceType string    `json:"source_type"`
	SourceID   string    `json:"source_id,omitempty"`
	Category   string    `json:"category"`
	Severity   string    `json:"severity"`
	Title      string    `json:"title"`
	Body       string    `json:"body"`
	// Link is an app path to the source, such as /automations?id=….
	Link       string     `json:"link,omitempty"`
	ReadAt     *time.Time `json:"read_at,omitempty"`
	Deliveries []Delivery `json:"deliveries,omitempty"`
}

// Request asks for a notification.
type Request struct {
	SourceType string
	SourceID   string
	Category   string
	Severity   string
	Title      string
	Body       string
	Link       string
	// DedupeKey folds repeats within dedupeWindow into one notification.
	DedupeKey string
	// Channels to deliver to besides the notification center, such as "desktop".
	Channels []string
}

// Channel delivers a stored notification somewhere outside the app.
type Channel interface {
	Name() string
	Deliver(ctx context.Context, n Notification) error
}

// ErrSuppressed means a channel chose not to deliver, such as desktop
// notifications turned off in settings. The notification is still stored.
var ErrSuppressed = errors.New("delivery suppressed")

// Hub stores notifications and fans them out.
type Hub struct {
	db       *sql.DB
	bus      *events.Bus
	channels map[string]Channel
	now      func() time.Time
}

// NewHub returns a hub using db and announcing on bus.
func NewHub(db *sql.DB, bus *events.Bus, channels ...Channel) *Hub {
	h := &Hub{db: db, bus: bus, channels: map[string]Channel{}, now: time.Now}
	for _, c := range channels {
		h.channels[c.Name()] = c
	}
	return h
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parseTS(s sql.NullString) *time.Time {
	if !s.Valid || s.String == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, s.String)
	if err != nil {
		return nil
	}
	return &t
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Notify stores a notification, announces it to the app, and delivers it to
// the requested channels. A repeat with the same dedupe key within ten
// minutes returns the existing notification, marked unread again.
func (h *Hub) Notify(ctx context.Context, req Request) (Notification, error) {
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" {
		return Notification{}, fmt.Errorf("title required")
	}
	if req.Category == "" {
		req.Category = CategorySystem
	}
	if req.Severity == "" {
		req.Severity = SeverityInfo
	}
	now := h.now().UTC()
	if req.DedupeKey != "" {
		var id string
		err := h.db.QueryRowContext(ctx, `SELECT id FROM notifications WHERE dedupe_key = ? AND created_at >= ? AND dismissed_at IS NULL ORDER BY created_at DESC LIMIT 1`,
			req.DedupeKey, ts(now.Add(-dedupeWindow))).Scan(&id)
		if err == nil {
			_, _ = h.db.ExecContext(ctx, `UPDATE notifications SET updated_at = ?, read_at = NULL, body = ? WHERE id = ?`, ts(now), req.Body, id)
			return h.Get(ctx, id)
		}
	}
	n := Notification{
		ID: uuid.NewString(), CreatedAt: now, SourceType: req.SourceType, SourceID: req.SourceID,
		Category: req.Category, Severity: req.Severity, Title: req.Title, Body: strings.TrimSpace(req.Body), Link: req.Link,
	}
	_, err := h.db.ExecContext(ctx, `
		INSERT INTO notifications (id, created_at, updated_at, source_type, source_id, category, severity, title, body, link, dedupe_key)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		n.ID, ts(now), ts(now), n.SourceType, nullable(n.SourceID), n.Category, n.Severity, n.Title, n.Body, nullable(n.Link), nullable(req.DedupeKey))
	if err != nil {
		return Notification{}, err
	}
	if h.bus != nil {
		h.bus.Publish(events.New(EventCreated, map[string]any{
			"id": n.ID, "category": n.Category, "severity": n.Severity, "title": n.Title, "body": n.Body, "link": n.Link,
		}))
	}
	for _, name := range req.Channels {
		n.Deliveries = append(n.Deliveries, h.deliver(ctx, n, name))
	}
	return n, nil
}

// deliver makes one channel's attempt and records it.
func (h *Hub) deliver(ctx context.Context, n Notification, name string) Delivery {
	d := Delivery{Channel: name, Attempts: 1}
	ch, ok := h.channels[name]
	var err error
	switch {
	case !ok:
		err = fmt.Errorf("channel %q is not available", name)
	default:
		dctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		err = ch.Deliver(dctx, n)
		cancel()
	}
	now := h.now().UTC()
	switch {
	case err == nil:
		d.Status, d.DeliveredAt = DeliveryDelivered, &now
	case errors.Is(err, ErrSuppressed):
		d.Status = DeliverySuppressed
	default:
		d.Status, d.Error = DeliveryFailed, err.Error()
	}
	var delivered any
	if d.DeliveredAt != nil {
		delivered = ts(*d.DeliveredAt)
	}
	_, _ = h.db.ExecContext(ctx, `
		INSERT INTO notification_deliveries (id, notification_id, channel, status, attempts, last_attempt_at, delivered_at, error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		uuid.NewString(), n.ID, name, d.Status, d.Attempts, ts(now), delivered, nullable(d.Error))
	return d
}

const columns = `id, created_at, source_type, COALESCE(source_id, ''), category, severity, title, body, COALESCE(link, ''), read_at`

func scan(row interface{ Scan(...any) error }) (Notification, error) {
	var n Notification
	var created string
	var read sql.NullString
	if err := row.Scan(&n.ID, &created, &n.SourceType, &n.SourceID, &n.Category, &n.Severity, &n.Title, &n.Body, &n.Link, &read); err != nil {
		return Notification{}, err
	}
	if t := parseTS(sql.NullString{String: created, Valid: true}); t != nil {
		n.CreatedAt = *t
	}
	n.ReadAt = parseTS(read)
	return n, nil
}

// Get returns one notification with its deliveries.
func (h *Hub) Get(ctx context.Context, id string) (Notification, error) {
	n, err := scan(h.db.QueryRowContext(ctx, `SELECT `+columns+` FROM notifications WHERE id = ?`, id))
	if err != nil {
		return Notification{}, err
	}
	rows, err := h.db.QueryContext(ctx, `SELECT channel, status, attempts, delivered_at, COALESCE(error, '') FROM notification_deliveries WHERE notification_id = ?`, id)
	if err != nil {
		return n, err
	}
	defer rows.Close()
	for rows.Next() {
		var d Delivery
		var delivered sql.NullString
		if err := rows.Scan(&d.Channel, &d.Status, &d.Attempts, &delivered, &d.Error); err != nil {
			return n, err
		}
		d.DeliveredAt = parseTS(delivered)
		n.Deliveries = append(n.Deliveries, d)
	}
	return n, rows.Err()
}

// List returns recent notifications that were not dismissed, newest first,
// and how many are unread.
func (h *Hub) List(ctx context.Context, unreadOnly bool, limit int) ([]Notification, int, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	where := `dismissed_at IS NULL`
	if unreadOnly {
		where += ` AND read_at IS NULL`
	}
	rows, err := h.db.QueryContext(ctx, `SELECT `+columns+` FROM notifications WHERE `+where+` ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Notification{}
	for rows.Next() {
		n, err := scan(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	var unread int
	err = h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notifications WHERE dismissed_at IS NULL AND read_at IS NULL`).Scan(&unread)
	return out, unread, err
}

// MarkRead marks notifications read; no ids marks all of them.
func (h *Hub) MarkRead(ctx context.Context, ids []string) error {
	now := ts(h.now())
	if len(ids) == 0 {
		_, err := h.db.ExecContext(ctx, `UPDATE notifications SET read_at = ? WHERE read_at IS NULL`, now)
		return err
	}
	for _, id := range ids {
		if _, err := h.db.ExecContext(ctx, `UPDATE notifications SET read_at = COALESCE(read_at, ?) WHERE id = ?`, now, id); err != nil {
			return err
		}
	}
	return nil
}

// Dismiss hides a notification from the notification center.
func (h *Hub) Dismiss(ctx context.Context, id string) error {
	res, err := h.db.ExecContext(ctx, `UPDATE notifications SET dismissed_at = ?, read_at = COALESCE(read_at, ?) WHERE id = ?`, ts(h.now()), ts(h.now()), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
