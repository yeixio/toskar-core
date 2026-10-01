package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// Use levels for memory and connected knowledge.
const (
	UseNever     = "never"
	UseOnRequest = "on_request"
	UseAlways    = "always"
)

// Tool levels.
const (
	ToolsProfile  = "profile"
	ToolsReadOnly = "read_only"
	ToolsNone     = "none"
)

// APIKeyPermissions is what a key may ask of the assistant (spec §62).
// Requests can only narrow it.
type APIKeyPermissions struct {
	// Memory is never, on_request (the request opts in), or always.
	Memory string `json:"memory"`
	// Knowledge is the profile's connected knowledge, and extra sources a
	// request names: never, on_request, or always.
	Knowledge string `json:"knowledge"`
	// Tools is profile (what the profile allows), read_only, or none.
	Tools string `json:"tools"`
	// Placement lets a request choose where it runs.
	Placement bool `json:"placement"`
}

// DefaultAPIKeyPermissions is what a key has until it is changed, and what
// a request on this computer without a key has: memory only when asked,
// the profile's knowledge and tools, and a choice of placement.
func DefaultAPIKeyPermissions() APIKeyPermissions {
	return APIKeyPermissions{Memory: UseOnRequest, Knowledge: UseAlways, Tools: ToolsProfile, Placement: true}
}

// Validate checks the levels.
func (p APIKeyPermissions) Validate() error {
	for name, v := range map[string]string{"memory": p.Memory, "knowledge": p.Knowledge} {
		if v != UseNever && v != UseOnRequest && v != UseAlways {
			return fmt.Errorf("%s must be never, on_request, or always", name)
		}
	}
	if p.Tools != ToolsProfile && p.Tools != ToolsReadOnly && p.Tools != ToolsNone {
		return fmt.Errorf("tools must be profile, read_only, or none")
	}
	return nil
}

func parsePermissions(raw sql.NullString) APIKeyPermissions {
	p := DefaultAPIKeyPermissions()
	if raw.Valid && raw.String != "" {
		_ = json.Unmarshal([]byte(raw.String), &p)
		if p.Validate() != nil {
			p = DefaultAPIKeyPermissions()
		}
	}
	return p
}

// SetPermissions changes what a key may do.
func (m *APIKeyManager) SetPermissions(ctx context.Context, id string, p APIKeyPermissions) (APIKeyRecord, error) {
	if err := p.Validate(); err != nil {
		return APIKeyRecord{}, err
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return APIKeyRecord{}, err
	}
	res, err := m.db.ExecContext(ctx, `UPDATE api_keys SET permissions = ? WHERE id = ? AND revoked_at IS NULL`, string(raw), id)
	if err != nil {
		return APIKeyRecord{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return APIKeyRecord{}, fmt.Errorf("api key %q not found", id)
	}
	list, err := m.List(ctx)
	if err != nil {
		return APIKeyRecord{}, err
	}
	for _, rec := range list {
		if rec.ID == id {
			return rec, nil
		}
	}
	return APIKeyRecord{}, fmt.Errorf("api key %q not found", id)
}
