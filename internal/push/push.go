// Package push sends notifications to the Toskar apps through Toskar's push
// relay (#460, docs/push.md). A paired device registers a sealed handle the
// relay made from its push token; this computer keeps the handle by the
// device's key and never sees the token. Each push is signed with this
// computer's key, which the relay checks against the one sealed in the
// handle, so only the computer the device paired with can push to it.
package push

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// DefaultRelay is Toskar's push relay, the same service as access from
// anywhere's (yeixio/toskar-relay).
const DefaultRelay = "https://relay.toskar.ai:4443"

// Platforms a handle can be for.
const (
	PlatformIOS     = "ios"
	PlatformAndroid = "android"
)

// Registration is one paired device's push handle.
type Registration struct {
	KeyID     string    `json:"key_id"`
	PersonID  string    `json:"-"`
	Handle    string    `json:"-"`
	Platform  string    `json:"platform"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Store keeps the registrations.
type Store struct{ db *sql.DB }

// NewStore is the registrations in db.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// maxHandle bounds a handle: the relay's are well under it.
const maxHandle = 4096

// Set registers or replaces a device's handle.
func (s *Store) Set(ctx context.Context, r Registration) error {
	r.Handle = strings.TrimSpace(r.Handle)
	if r.Handle == "" || len(r.Handle) > maxHandle {
		return errors.New("the push handle is missing or too long")
	}
	if r.Platform != PlatformIOS && r.Platform != PlatformAndroid {
		return errors.New("platform must be ios or android")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO push_registrations (key_id, person_id, handle, platform, updated_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(key_id) DO UPDATE SET person_id = excluded.person_id, handle = excluded.handle,
			platform = excluded.platform, updated_at = excluded.updated_at`,
		r.KeyID, r.PersonID, r.Handle, r.Platform, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

// Get is a device's registration, if it has one.
func (s *Store) Get(ctx context.Context, keyID string) (Registration, bool, error) {
	var r Registration
	var updated string
	err := s.db.QueryRowContext(ctx, `SELECT key_id, person_id, handle, platform, updated_at FROM push_registrations WHERE key_id = ?`, keyID).
		Scan(&r.KeyID, &r.PersonID, &r.Handle, &r.Platform, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Registration{}, false, nil
	}
	if err != nil {
		return Registration{}, false, err
	}
	r.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return r, true, nil
}

// Remove forgets a device's handle.
func (s *Store) Remove(ctx context.Context, keyID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM push_registrations WHERE key_id = ?`, keyID)
	return err
}

// ForPerson is the person's devices that can be pushed to: registered, with
// a key that's still good. "" is the Owner's, as for keys from before people.
func (s *Store) ForPerson(ctx context.Context, personID string) ([]Registration, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT r.key_id, r.person_id, r.handle, r.platform, r.updated_at
		FROM push_registrations r JOIN api_keys k ON k.id = r.key_id
		WHERE k.revoked_at IS NULL AND r.person_id = ?
		ORDER BY r.updated_at`, personID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Registration
	for rows.Next() {
		var r Registration
		var updated string
		if err := rows.Scan(&r.KeyID, &r.PersonID, &r.Handle, &r.Platform, &updated); err != nil {
			return nil, err
		}
		r.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
		out = append(out, r)
	}
	return out, rows.Err()
}

// Message is one push, as the relay sends it on.
type Message struct {
	Handle string `json:"handle"`
	Title  string `json:"title"`
	Body   string `json:"body,omitempty"`
	// Link is the app path the notification opens, such as /automations?id=….
	Link string `json:"link,omitempty"`
	// NotificationID lets the app fetch the whole notification from the
	// computer, and mark it read.
	NotificationID string `json:"notification_id,omitempty"`
	Category       string `json:"category,omitempty"`
	Severity       string `json:"severity,omitempty"`
	// Collapse replaces an earlier push with the same key on the device.
	Collapse string `json:"collapse,omitempty"`
	// Urgent asks for a time-sensitive interruption and high priority.
	Urgent bool `json:"urgent,omitempty"`
}

// ErrGone is a handle the relay says no longer reaches a device: the app
// was removed, or its token changed.
var ErrGone = errors.New("the device no longer takes pushes")

// RelayError is a refusal from the relay, with its code.
type RelayError struct {
	Status int
	Code   string
	Text   string
}

func (e *RelayError) Error() string {
	if e.Text != "" {
		return fmt.Sprintf("the push relay refused it (%d %s): %s", e.Status, e.Code, e.Text)
	}
	return fmt.Sprintf("the push relay refused it (%d %s)", e.Status, e.Code)
}

// Permanent reports a refusal that trying again won't change.
func (e *RelayError) Permanent() bool {
	return e.Status >= 400 && e.Status < 500 && e.Status != http.StatusTooManyRequests && e.Status != http.StatusRequestTimeout
}

// Client sends pushes to a relay as this computer.
type Client struct {
	// Relay is the relay's base URL; empty is DefaultRelay.
	Relay string
	// Key is this computer's identity key.
	Key  ed25519.PrivateKey
	HTTP *http.Client
	// Now is the clock, for tests.
	Now func() time.Time
}

// PublicKey is this computer's key as the apps register it with the relay.
func (c *Client) PublicKey() string {
	return base64.StdEncoding.EncodeToString(c.Key.Public().(ed25519.PublicKey))
}

// RelayURL is the relay the apps register with.
func (c *Client) RelayURL() string {
	if c.Relay != "" {
		return strings.TrimRight(c.Relay, "/")
	}
	return DefaultRelay
}

// Signed is what the signature covers: the time, then the body's SHA-256.
func Signed(at int64, body []byte) []byte {
	sum := sha256.Sum256(body)
	return []byte(strconv.FormatInt(at, 10) + "\n" + hex.EncodeToString(sum[:]))
}

// Send sends one push.
func (c *Client) Send(ctx context.Context, m Message) error {
	body, err := json.Marshal(m)
	if err != nil {
		return err
	}
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	at := now().Unix()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.RelayURL()+"/v1/push", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Toskar-Key", c.PublicKey())
	req.Header.Set("Toskar-Time", strconv.FormatInt(at, 10))
	req.Header.Set("Toskar-Signature", base64.StdEncoding.EncodeToString(ed25519.Sign(c.Key, Signed(at, body))))
	httpc := c.HTTP
	if httpc == nil {
		httpc = &http.Client{Timeout: 20 * time.Second}
	}
	resp, err := httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 300 {
		return nil
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var e struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(raw, &e)
	if resp.StatusCode == http.StatusGone || e.Error.Code == "HANDLE_GONE" {
		return ErrGone
	}
	return &RelayError{Status: resp.StatusCode, Code: e.Error.Code, Text: e.Error.Message}
}
