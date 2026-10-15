package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// People and roles (#206). Toskar starts with one person, the Owner: the
// person at this computer. Others are added later with a role, which
// decides what they may see and change.

// Role is what a person may do.
type Role string

// Roles, from the most to the least.
const (
	// RoleOwner runs this Toskar: everything, including other people.
	RoleOwner Role = "owner"
	// RoleAdmin administers it: models, tools, computers, people other
	// than the Owner.
	RoleAdmin Role = "admin"
	// RoleMember uses it: their own chats, memories, files, and
	// automations.
	RoleMember Role = "member"
	// RoleVisitor chats in what they're given, such as a portal (#205).
	RoleVisitor Role = "visitor"
)

var rank = map[Role]int{RoleOwner: 4, RoleAdmin: 3, RoleMember: 2, RoleVisitor: 1}

// Valid reports a known role.
func (r Role) Valid() bool { return rank[r] > 0 }

// AtLeast reports a role that may do what min may.
func (r Role) AtLeast(min Role) bool { return rank[r] >= rank[min] && rank[min] > 0 }

// OwnerID is the Owner's person id, which the install's existing data
// belongs to.
const OwnerID = "owner"

// Person is someone who uses this Toskar.
type Person struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Username  string     `json:"username,omitempty"`
	Role      Role       `json:"role"`
	CreatedAt time.Time  `json:"created_at"`
	Disabled  *time.Time `json:"disabled_at,omitempty"`
	// SignIn is true once they've set a username and password.
	SignIn bool `json:"sign_in"`
	// External shows who they are where they sign in, at a trusted proxy
	// or an OpenID Connect provider (#206).
	External string `json:"external,omitempty"`
	// PortalID is the chat portal a guest belongs to (#205); a guest
	// reaches only that portal's chat.
	PortalID string `json:"portal_id,omitempty"`
	// Profiles pins them to these profiles (#345), in place of their
	// role's: nil is their role's, empty is any profile.
	Profiles *[]string `json:"profiles,omitempty"`
	// Email is where to reach them (#592), and MustChange says the
	// password an Admin set must be changed at their next sign-in.
	Email      string `json:"email,omitempty"`
	MustChange bool   `json:"must_change,omitempty"`
}

// ErrNoPerson is an unknown or disabled person.
var ErrNoPerson = errors.New("no such person")

// People finds people.
type People struct {
	db *sql.DB
}

// NewPeople reads people from db.
func NewPeople(db *sql.DB) *People { return &People{db: db} }

const personColumns = `id, name, COALESCE(username, ''), role, created_at, disabled_at, password_hash IS NOT NULL, COALESCE(external_label, external_id, ''), COALESCE(portal_id, ''), profiles_json, COALESCE(email, ''), must_change`

func scanPerson(row interface{ Scan(...any) error }) (Person, error) {
	var p Person
	var created string
	var disabled, profiles sql.NullString
	if err := row.Scan(&p.ID, &p.Name, &p.Username, &p.Role, &created, &disabled, &p.SignIn, &p.External, &p.PortalID, &profiles, &p.Email, &p.MustChange); err != nil {
		return Person{}, err
	}
	if profiles.Valid {
		list := []string{}
		if json.Unmarshal([]byte(profiles.String), &list) == nil {
			p.Profiles = &list
		}
	}
	p.CreatedAt = parseTime(created)
	if disabled.Valid {
		t := parseTime(disabled.String)
		p.Disabled = &t
	}
	return p, nil
}

// Get is one person, who may be disabled.
func (p *People) Get(ctx context.Context, id string) (Person, error) {
	person, err := scanPerson(p.db.QueryRowContext(ctx, `SELECT `+personColumns+` FROM people WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Person{}, ErrNoPerson
	}
	return person, err
}

// Active is a person who may use Toskar now: known and not disabled.
func (p *People) Active(ctx context.Context, id string) (Person, error) {
	person, err := p.Get(ctx, id)
	if err != nil {
		return Person{}, err
	}
	if person.Disabled != nil {
		return Person{}, ErrNoPerson
	}
	return person, nil
}

// List is everyone, the Owner first, without portals' guests.
func (p *People) List(ctx context.Context) ([]Person, error) {
	rows, err := p.db.QueryContext(ctx, `SELECT `+personColumns+` FROM people WHERE portal_id IS NULL
		ORDER BY CASE role WHEN 'owner' THEN 0 WHEN 'admin' THEN 1 WHEN 'member' THEN 2 ELSE 3 END, created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Person{}
	for rows.Next() {
		person, err := scanPerson(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, person)
	}
	return out, rows.Err()
}

// Errors from changing people.
var (
	ErrUsernameTaken = errors.New("that username is taken")
	ErrBadUsername   = errors.New("a username is 3 to 32 letters, digits, dots, dashes, or underscores")
	ErrBadRole       = errors.New("unknown role")
	ErrOwnerFixed    = errors.New("the Owner can't be given another role or disabled")
	ErrOneOwner      = errors.New("there is only one Owner")
	ErrBadName       = errors.New("a name is 1 to 80 characters")
	ErrBadEmail      = errors.New("that isn't an email address")
	ErrEmailTaken    = errors.New("someone else has that email address")
)

var emailRe = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

// cleanEmail is an email address, trimmed, or "" for none.
func cleanEmail(email string) (string, error) {
	email = strings.TrimSpace(email)
	if email != "" && (len(email) > 254 || !emailRe.MatchString(email)) {
		return "", ErrBadEmail
	}
	return email, nil
}

// emailTaken reports someone other than id with email.
func (p *People) emailTaken(ctx context.Context, id, email string) (bool, error) {
	if email == "" {
		return false, nil
	}
	var other string
	err := p.db.QueryRowContext(ctx, `SELECT id FROM people WHERE email = ? COLLATE NOCASE AND id <> ?`, email, id).Scan(&other)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

var usernameRe = regexp.MustCompile(`^[A-Za-z0-9._-]{3,32}$`)

func cleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 80 {
		return "", ErrBadName
	}
	return name, nil
}

// Create adds a person with a role other than Owner. They sign in once
// they accept an invite.
func (p *People) Create(ctx context.Context, name string, role Role) (Person, error) {
	return p.CreateWith(ctx, name, role, "")
}

// CreateWith adds a person with an email address (#592).
func (p *People) CreateWith(ctx context.Context, name string, role Role, email string) (Person, error) {
	name, err := cleanName(name)
	if err != nil {
		return Person{}, err
	}
	if email, err = cleanEmail(email); err != nil {
		return Person{}, err
	}
	if taken, err := p.emailTaken(ctx, "", email); err != nil {
		return Person{}, err
	} else if taken {
		return Person{}, ErrEmailTaken
	}
	if !role.Valid() {
		return Person{}, ErrBadRole
	}
	if role == RoleOwner {
		return Person{}, ErrOneOwner
	}
	id := uuid.NewString()
	if _, err := p.db.ExecContext(ctx, `INSERT INTO people (id, name, role, created_at, email) VALUES (?, ?, ?, ?, ?)`,
		id, name, string(role), stamp(time.Now()), nullable(email)); err != nil {
		return Person{}, err
	}
	return p.Get(ctx, id)
}

// Change is a change to a person; nil fields stay.
type Change struct {
	Name     *string `json:"name,omitempty"`
	Role     *Role   `json:"role,omitempty"`
	Disabled *bool   `json:"disabled,omitempty"`
	// Email is their address; "" removes it (#592).
	Email *string `json:"email,omitempty"`
}

// Update changes a person. The Owner keeps their role and can't be
// disabled; nobody else becomes Owner.
func (p *People) Update(ctx context.Context, id string, c Change) (Person, error) {
	person, err := p.Get(ctx, id)
	if err != nil {
		return Person{}, err
	}
	if c.Name != nil {
		name, err := cleanName(*c.Name)
		if err != nil {
			return Person{}, err
		}
		person.Name = name
	}
	if c.Email != nil {
		email, err := cleanEmail(*c.Email)
		if err != nil {
			return Person{}, err
		}
		if taken, err := p.emailTaken(ctx, id, email); err != nil {
			return Person{}, err
		} else if taken {
			return Person{}, ErrEmailTaken
		}
		person.Email = email
	}
	if c.Role != nil && *c.Role != person.Role {
		if !c.Role.Valid() {
			return Person{}, ErrBadRole
		}
		if person.Role == RoleOwner {
			return Person{}, ErrOwnerFixed
		}
		if *c.Role == RoleOwner {
			return Person{}, ErrOneOwner
		}
		person.Role = *c.Role
	}
	disabled := person.Disabled != nil
	if c.Disabled != nil {
		if *c.Disabled && person.Role == RoleOwner {
			return Person{}, ErrOwnerFixed
		}
		disabled = *c.Disabled
	}
	var disabledAt any
	if disabled {
		if person.Disabled != nil {
			disabledAt = stamp(*person.Disabled)
		} else {
			disabledAt = stamp(time.Now())
		}
	}
	if _, err := p.db.ExecContext(ctx, `UPDATE people SET name = ?, role = ?, disabled_at = ?, email = ? WHERE id = ?`,
		person.Name, string(person.Role), disabledAt, nullable(person.Email), id); err != nil {
		return Person{}, err
	}
	return p.Get(ctx, id)
}

// CheckSignIn reports what's wrong with a username and password for
// person, without setting them: a bad or taken username, or a short
// password.
func (p *People) CheckSignIn(ctx context.Context, id, username, password string) error {
	username = strings.TrimSpace(username)
	if !usernameRe.MatchString(username) {
		return ErrBadUsername
	}
	if len([]rune(password)) < MinPassword {
		return ErrWeakPassword
	}
	var other string
	err := p.db.QueryRowContext(ctx, `SELECT id FROM people WHERE username = ? COLLATE NOCASE AND id <> ?`, username, id).Scan(&other)
	if err == nil {
		return ErrUsernameTaken
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	return nil
}

// SetSignIn sets a person's username and password. A taken username, by
// someone else, is refused.
func (p *People) SetSignIn(ctx context.Context, id, username, password string) error {
	if err := p.CheckSignIn(ctx, id, username, password); err != nil {
		return err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	_, err = p.db.ExecContext(ctx, `UPDATE people SET username = ?, password_hash = ? WHERE id = ?`, strings.TrimSpace(username), hash, id)
	return err
}

// SetPassword gives a person a new password, keeping their username. It's
// their own, so they needn't change it again.
func (p *People) SetPassword(ctx context.Context, id, password string) error {
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	_, err = p.db.ExecContext(ctx, `UPDATE people SET password_hash = ?, must_change = 0 WHERE id = ?`, hash, id)
	return err
}

// SetPasswordFor is a password an Admin sets for someone (#592): with a
// username when they have none yet, and, with mustChange, one they must
// change at their next sign-in.
func (p *People) SetPasswordFor(ctx context.Context, id, username, password string, mustChange bool) error {
	if p.HasSignIn(ctx, id) {
		if err := p.SetPassword(ctx, id, password); err != nil {
			return err
		}
	} else if err := p.SetSignIn(ctx, id, username, password); err != nil {
		return err
	}
	_, err := p.db.ExecContext(ctx, `UPDATE people SET must_change = ? WHERE id = ?`, mustChange, id)
	return err
}

// Delete removes a person who isn't the Owner, with their sessions and
// sign-in links; their data must be removed or given away first.
func (p *People) Delete(ctx context.Context, id string) error {
	if id == OwnerID {
		return ErrOwnerFixed
	}
	res, err := p.db.ExecContext(ctx, `DELETE FROM people WHERE id = ? AND role <> 'owner'`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNoPerson
	}
	return nil
}

// SignIn is the active person with this username and password. A wrong
// username takes as long as a wrong password, and both say the same.
func (p *People) SignIn(ctx context.Context, username, password string) (Person, error) {
	var id string
	var hash sql.NullString
	err := p.db.QueryRowContext(ctx, `SELECT id, password_hash FROM people WHERE username = ? COLLATE NOCASE AND disabled_at IS NULL`,
		strings.TrimSpace(username)).Scan(&id, &hash)
	if err != nil || !hash.Valid {
		CheckPassword(dummyHash, password)
		return Person{}, ErrSignIn
	}
	if !CheckPassword(hash.String, password) {
		return Person{}, ErrSignIn
	}
	return p.Active(ctx, id)
}

// HasSignIn reports whether a person has set a username and password.
func (p *People) HasSignIn(ctx context.Context, id string) bool {
	var hash sql.NullString
	err := p.db.QueryRowContext(ctx, `SELECT password_hash FROM people WHERE id = ?`, id).Scan(&hash)
	return err == nil && hash.Valid
}

// Principal is who a request is from, and how Toskar knows.
type Principal struct {
	Person Person `json:"person"`
	// Via is this_computer (a request from this computer, which is the
	// Owner's), api_key, or, later, a session.
	Via string `json:"via"`
	// KeyID is the API key the request used, when Via is api_key.
	KeyID string `json:"key_id,omitempty"`
	// KeyProfile is the profile that key is pinned to (#345), or "".
	KeyProfile string `json:"key_profile,omitempty"`
}

// Ways a request is known to be from someone.
const (
	ViaThisComputer = "this_computer"
	ViaAPIKey       = "api_key"
	ViaSession      = "session"
	// ViaNone is a request from no one known yet.
	ViaNone = "none"
	// ViaSystem is Toskar's own work across everyone, such as the
	// scheduler finding what's due. It reaches every person's data; what it
	// then does for one person runs as them.
	ViaSystem = "system"
)

// Anonymous is a request no one is known to have made, such as signing in:
// it reaches no one's data.
func Anonymous() Principal { return Principal{Via: ViaNone} }

// WithSystem marks Toskar's own work across everyone.
func WithSystem(ctx context.Context) context.Context {
	return WithPrincipal(ctx, Principal{Person: Person{ID: OwnerID, Name: "Owner", Role: RoleOwner}, Via: ViaSystem})
}

// Scope is whose data ctx may reach: one person's, or everyone's for
// Toskar's own work.
func Scope(ctx context.Context) (personID string, everyone bool) {
	p := PrincipalFrom(ctx)
	return p.Person.ID, p.Via == ViaSystem
}

// AsPerson is ctx acting for person, such as an automation running as the
// person whose it is.
func AsPerson(ctx context.Context, person Person) context.Context {
	return WithPrincipal(ctx, Principal{Person: person, Via: ViaSystem + ":" + person.ID})
}

// PersonID is whose a request or job is: the person whose data it reads
// and writes (#206).
func PersonID(ctx context.Context) string { return PrincipalFrom(ctx).Person.ID }

type principalKey struct{}

// WithPrincipal carries who a request is from.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// PrincipalFrom is who a request is from. A context without one, such as
// the scheduler's own work, is the Owner's.
func PrincipalFrom(ctx context.Context) Principal {
	if p, ok := ctx.Value(principalKey{}).(Principal); ok {
		return p
	}
	return Principal{Person: Person{ID: OwnerID, Name: "Owner", Role: RoleOwner}, Via: ViaThisComputer}
}

// CreateGuest adds an anonymous visitor to a chat portal (#205): a person
// who belongs to it, so their chats are their own, and who reaches only
// that portal.
func (p *People) CreateGuest(ctx context.Context, portalID string) (Person, error) {
	return p.createGuest(ctx, portalID, "Guest", false)
}

// CreateInvitedGuest adds a chat portal's visitor an Admin invites by name
// (#205).
func (p *People) CreateInvitedGuest(ctx context.Context, portalID, name string) (Person, error) {
	return p.createGuest(ctx, portalID, name, true)
}

func (p *People) createGuest(ctx context.Context, portalID, name string, invited bool) (Person, error) {
	name, err := cleanName(name)
	if err != nil {
		return Person{}, err
	}
	id := uuid.NewString()
	if _, err := p.db.ExecContext(ctx, `INSERT INTO people (id, name, role, created_at, portal_id, invited) VALUES (?, ?, ?, ?, ?, ?)`,
		id, name, string(RoleVisitor), stamp(time.Now()), portalID, invited); err != nil {
		return Person{}, err
	}
	return p.Get(ctx, id)
}

// Guests is a chat portal's visitors who were invited by name, newest
// first (#205).
func (p *People) Guests(ctx context.Context, portalID string) ([]Person, error) {
	rows, err := p.db.QueryContext(ctx, `SELECT `+personColumns+` FROM people WHERE portal_id = ? AND invited = 1 ORDER BY created_at DESC`, portalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Person{}
	for rows.Next() {
		person, err := scanPerson(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, person)
	}
	return out, rows.Err()
}

// nullable is s, or NULL when it's empty.
func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
