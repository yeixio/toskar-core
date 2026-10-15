package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// Capabilities (#589): what each role, and each person, may use, finer than
// the routes a role reaches. Admins change a role's defaults and Allow or
// Block one thing for one person; the Owner always has everything, so
// nobody can lock them out.

// Capability is one thing someone may use.
type Capability string

const (
	// CapFiles is attaching and uploading files other than pictures.
	CapFiles Capability = "files"
	// CapPicturesIn is sending pictures for a model to look at.
	CapPicturesIn Capability = "pictures_in"
	// CapMakePictures is making and editing pictures.
	CapMakePictures Capability = "make_pictures"
	// CapMakeVideo is making clips.
	CapMakeVideo Capability = "make_video"
	// CapVoice is speaking a message and having answers read aloud.
	CapVoice Capability = "voice"
	// CapWeb is web lookup and reading pages.
	CapWeb Capability = "web"
	// CapKnowledge is Knowledge in answers; CapKnowledgeAdd is adding to it.
	CapKnowledge    Capability = "knowledge"
	CapKnowledgeAdd Capability = "knowledge_add"
	// CapAutomations is making automations.
	CapAutomations Capability = "automations"
	// CapChangingTools is tools that change things: commands, file
	// writes, and MCP and plugin actions.
	CapChangingTools Capability = "changing_tools"
	// CapChooseModel is choosing a model by hand; without it, Auto.
	CapChooseModel Capability = "choose_model"
	// CapRemoteAccess is using paired devices away from home.
	CapRemoteAccess Capability = "remote_access"
)

// AllCapabilities is every capability, in the order the app shows them.
var AllCapabilities = []Capability{CapFiles, CapPicturesIn, CapMakePictures, CapMakeVideo, CapVoice, CapWeb,
	CapKnowledge, CapKnowledgeAdd, CapAutomations, CapChangingTools, CapChooseModel, CapRemoteAccess}

// Valid reports a known capability.
func (c Capability) Valid() bool {
	for _, k := range AllCapabilities {
		if k == c {
			return true
		}
	}
	return false
}

// Capabilities is what someone may use: true is allowed.
type Capabilities map[Capability]bool

// Allows reports c allowed; an unknown or missing one is allowed, so a
// capability added later doesn't take anything away.
func (c Capabilities) Allows(cap Capability) bool {
	allowed, ok := c[cap]
	return !ok || allowed
}

// DefaultCapabilities are what each role ships with: the Owner and Admins
// everything, Members everything but tools that change things, and
// Visitors chat, the web, and pictures in, without files, making pictures
// or video, automations, or adding to Knowledge.
func DefaultCapabilities(role Role) Capabilities {
	out := Capabilities{}
	for _, c := range AllCapabilities {
		out[c] = true
	}
	switch role {
	case RoleMember:
		out[CapChangingTools] = false
	case RoleVisitor:
		for _, c := range []Capability{CapFiles, CapMakePictures, CapMakeVideo, CapAutomations, CapKnowledgeAdd, CapChangingTools} {
			out[c] = false
		}
	}
	return out
}

// ErrCapabilityRole is changing the Owner's capabilities, which are fixed.
var ErrCapabilityRole = errors.New("the Owner always has everything")

// ErrCapabilityUnknown is a capability Toskar doesn't know.
var ErrCapabilityUnknown = errors.New("unknown capability")

func cleanCapabilities(in map[Capability]bool) (map[Capability]bool, error) {
	out := map[Capability]bool{}
	for c, v := range in {
		if !c.Valid() {
			return nil, ErrCapabilityUnknown
		}
		out[c] = v
	}
	return out, nil
}

// RoleCapabilities are each role's capabilities: the shipped defaults with
// an Admin's changes.
func (p *People) RoleCapabilities(ctx context.Context) (map[Role]Capabilities, error) {
	out := map[Role]Capabilities{}
	for _, r := range []Role{RoleOwner, RoleAdmin, RoleMember, RoleVisitor} {
		out[r] = DefaultCapabilities(r)
	}
	rows, err := p.db.QueryContext(ctx, `SELECT role, capabilities_json FROM role_capabilities`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var role, raw string
		if err := rows.Scan(&role, &raw); err != nil {
			return nil, err
		}
		changed := map[Capability]bool{}
		if json.Unmarshal([]byte(raw), &changed) != nil || Role(role) == RoleOwner {
			continue
		}
		for c, v := range changed {
			if c.Valid() {
				out[Role(role)][c] = v
			}
		}
	}
	return out, rows.Err()
}

// SetRoleCapabilities changes a role's capabilities from the defaults;
// values equal to the default are kept as the default, so a later change
// to the defaults reaches them.
func (p *People) SetRoleCapabilities(ctx context.Context, role Role, caps map[Capability]bool) error {
	if role == RoleOwner {
		return ErrCapabilityRole
	}
	if !role.Valid() {
		return ErrBadRole
	}
	caps, err := cleanCapabilities(caps)
	if err != nil {
		return err
	}
	defaults := DefaultCapabilities(role)
	changed := map[Capability]bool{}
	for c, v := range caps {
		if defaults[c] != v {
			changed[c] = v
		}
	}
	if len(changed) == 0 {
		_, err := p.db.ExecContext(ctx, `DELETE FROM role_capabilities WHERE role = ?`, string(role))
		return err
	}
	b, _ := json.Marshal(changed)
	_, err = p.db.ExecContext(ctx, `INSERT INTO role_capabilities (role, capabilities_json) VALUES (?, ?)
		ON CONFLICT(role) DO UPDATE SET capabilities_json = excluded.capabilities_json`, string(role), string(b))
	return err
}

// PersonCapabilities are the Allows and Blocks set for one person, in
// place of their role's; a capability not in it is as their role's.
func (p *People) PersonCapabilities(ctx context.Context, id string) (map[Capability]bool, error) {
	var raw sql.NullString
	err := p.db.QueryRowContext(ctx, `SELECT capabilities_json FROM people WHERE id = ?`, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoPerson
	}
	if err != nil {
		return nil, err
	}
	out := map[Capability]bool{}
	if raw.Valid {
		_ = json.Unmarshal([]byte(raw.String), &out)
	}
	return out, nil
}

// SetPersonCapabilities sets one person's Allows and Blocks: a capability
// missing from the map, or set to nil, is as their role's.
func (p *People) SetPersonCapabilities(ctx context.Context, id string, set map[Capability]*bool) error {
	person, err := p.Get(ctx, id)
	if err != nil {
		return err
	}
	if person.Role == RoleOwner {
		return ErrCapabilityRole
	}
	out := map[Capability]bool{}
	for c, v := range set {
		if !c.Valid() {
			return ErrCapabilityUnknown
		}
		if v != nil {
			out[c] = *v
		}
	}
	var raw any
	if len(out) > 0 {
		b, _ := json.Marshal(out)
		raw = string(b)
	}
	_, err = p.db.ExecContext(ctx, `UPDATE people SET capabilities_json = ? WHERE id = ?`, raw, id)
	return err
}

// CapabilitiesOf is what person may use now: the Owner everything, anyone
// else their role's with their own Allows and Blocks. A portal's guest has
// the Visitor's; the portal's own settings narrow it further.
func (p *People) CapabilitiesOf(ctx context.Context, person Person) (Capabilities, error) {
	if person.Role == RoleOwner || person.ID == OwnerID {
		return DefaultCapabilities(RoleOwner), nil
	}
	roles, err := p.RoleCapabilities(ctx)
	if err != nil {
		return nil, err
	}
	role := person.Role
	if person.PortalID != "" {
		role = RoleVisitor
	}
	out := Capabilities{}
	for c, v := range roles[role] {
		out[c] = v
	}
	if person.PortalID == "" && person.ID != "" {
		own, err := p.PersonCapabilities(ctx, person.ID)
		if err != nil && !errors.Is(err, ErrNoPerson) {
			return nil, err
		}
		for c, v := range own {
			out[c] = v
		}
	}
	return out, nil
}
