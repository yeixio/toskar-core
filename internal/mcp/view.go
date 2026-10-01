package mcp

import (
	"context"
	"net/url"
	"sort"
	"strings"
	"time"
)

// View is what may be shown about a source: never a secret value.
type View struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Preset      string `json:"preset,omitempty"`
	// Where is "local" for a program on this computer, "remote" for a web
	// address.
	Where   string     `json:"where"`
	Command string     `json:"command,omitempty"`
	Args    []string   `json:"args,omitempty"`
	URL     string     `json:"url,omitempty"`
	Env     []Variable `json:"env"`
	Headers []Variable `json:"headers"`

	Enabled       bool     `json:"enabled"`
	AllowSampling bool     `json:"allow_sampling"`
	AlwaysOffer   bool     `json:"always_offer"`
	Keywords      []string `json:"keywords"`

	// Status is ready, sign_in, error, or off.
	Status   string `json:"status"`
	Running  bool   `json:"running"`
	Error    string `json:"error,omitempty"`
	SignedIn bool   `json:"signed_in,omitempty"`
	// Missing names a program this source needs that is not installed,
	// such as Node.js.
	Missing string `json:"missing,omitempty"`

	ServerName    string `json:"server_name,omitempty"`
	ServerVersion string `json:"server_version,omitempty"`
	Protocol      string `json:"protocol,omitempty"`
	Instructions  string `json:"instructions,omitempty"`
	HasResources  bool   `json:"has_resources"`
	HasPrompts    bool   `json:"has_prompts"`

	Tools     []ToolView `json:"tools"`
	AddedAt   time.Time  `json:"added_at"`
	CheckedAt time.Time  `json:"checked_at,omitzero"`
	LastUsed  time.Time  `json:"last_used,omitzero"`
}

// Variable is an environment variable or header; secret values show only
// their last four characters.
type Variable struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Secret bool   `json:"secret"`
}

// ToolView is one of a source's tools.
type ToolView struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Remote      string `json:"remote_name"`
	Description string `json:"description"`
	Risk        string `json:"risk"`
	// Policy is allow or ask; Changed is true when the person set it.
	Policy  string `json:"policy"`
	Changed bool   `json:"changed"`
	Enabled bool   `json:"enabled"`
}

// List returns every source.
func (m *Manager) List(ctx context.Context) []View {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]View, 0, len(m.servers))
	for _, s := range m.servers {
		out = append(out, m.viewLocked(s))
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

// Get returns one source.
func (m *Manager) Get(id string) (View, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.servers[id]
	if !ok {
		return View{}, false
	}
	return m.viewLocked(s), true
}

func (m *Manager) view(s *server) View {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.viewLocked(s)
}

func (m *Manager) viewLocked(s *server) View {
	sec, _ := m.readSecrets(s.id)
	v := View{
		ID: s.id, Name: s.spec.Name, Preset: s.spec.Preset, Command: s.spec.Command, URL: s.spec.URL,
		Enabled: s.enabled, AllowSampling: s.sampling, AlwaysOffer: s.always, Keywords: append([]string{}, s.spec.Keywords...),
		Status: s.status, Running: s.client != nil, Error: s.errText, SignedIn: sec.OAuth.SignedIn(),
		ServerName: s.info.Name, ServerVersion: s.info.Version, Protocol: s.info.Protocol, Instructions: s.info.Instructions,
		HasResources: s.info.Resources, HasPrompts: s.info.Prompts,
		AddedAt: s.added, CheckedAt: s.checked, Env: []Variable{}, Headers: []Variable{}, Tools: []ToolView{},
	}
	if p, ok := presetByID(s.spec.Preset); ok {
		v.Description = p.Description
	}
	if s.client != nil {
		v.LastUsed = s.lastUsed
	}
	if !s.enabled {
		v.Status = StatusOff
	}
	v.Where = "local"
	if s.spec.Remote() {
		v.Where = "remote"
	} else {
		v.Missing = MissingRuntime(s.spec.Command)
	}
	v.Args = make([]string, len(s.spec.Args))
	for i, a := range s.spec.Args {
		if stored := sec.Args[i]; a == "" && stored != "" {
			a = mask(stored)
		}
		v.Args[i] = a
	}
	for _, k := range sortedKeys(s.spec.Env) {
		val, secret := s.spec.Env[k], false
		if stored := sec.Env[k]; val == "" && stored != "" {
			val, secret = mask(stored), true
		}
		v.Env = append(v.Env, Variable{Key: k, Value: val, Secret: secret})
	}
	for _, k := range sortedKeys(s.spec.Headers) {
		v.Headers = append(v.Headers, Variable{Key: k, Value: mask(sec.Headers[k]), Secret: true})
	}
	for _, def := range m.definitionsLocked(s) {
		remote := ""
		for _, t := range s.tools {
			if toolID(s.id, t.Name) == def.ID {
				remote = t.Name
				break
			}
		}
		_, changed := s.policies[remote]
		v.Tools = append(v.Tools, ToolView{
			ID: def.ID, Name: def.Name, Remote: remote, Description: def.Description, Risk: def.Risk,
			Policy: def.DefaultPolicy, Changed: changed && remote != "", Enabled: !m.registry.IsDisabled(def.ID),
		})
	}
	return v
}

// mask shows only the last four characters of a secret.
func mask(v string) string {
	v = strings.TrimPrefix(v, "Bearer ")
	if len(v) <= 8 {
		return "••••"
	}
	return "••••" + v[len(v)-4:]
}

// GalleryEntry is a gallery entry with what this computer has.
type GalleryEntry struct {
	Preset
	// Missing names a program the entry needs that is not installed.
	Missing string `json:"missing,omitempty"`
	// Added is the id of a source already added from this entry.
	Added string `json:"added,omitempty"`
}

// GalleryView lists the gallery with what is installed and added.
func (m *Manager) GalleryView() []GalleryEntry {
	m.mu.Lock()
	added := map[string]string{}
	for _, s := range m.servers {
		if s.spec.Preset != "" {
			added[s.spec.Preset] = s.id
		}
	}
	m.mu.Unlock()
	missing := map[string]string{}
	out := make([]GalleryEntry, 0, len(gallery))
	for _, p := range gallery {
		e := GalleryEntry{Preset: p, Added: added[p.ID]}
		if e.Fields == nil {
			e.Fields = []Field{}
		}
		if p.command != "" {
			if _, ok := missing[p.command]; !ok {
				missing[p.command] = MissingRuntime(p.command)
			}
			e.Missing = missing[p.command]
		}
		out = append(out, e)
	}
	return out
}

// ImportCandidate is a server another app has, and whether it is here.
type ImportCandidate struct {
	Found
	Missing string `json:"missing,omitempty"`
	// Added is true when a source with the same command or address is
	// already here.
	Added bool `json:"added"`
}

// ImportCandidates lists servers other apps on this computer have.
func (m *Manager) ImportCandidates() []ImportCandidate {
	m.mu.Lock()
	have := map[string]bool{}
	for _, s := range m.servers {
		have[sameKey(s.spec)] = true
	}
	m.mu.Unlock()
	var out []ImportCandidate
	seen := map[string]bool{}
	for _, f := range Discover() {
		key := f.App + "\x00" + f.Spec.Name
		if seen[key] {
			continue
		}
		seen[key] = true
		c := ImportCandidate{Found: f, Added: have[sameKey(f.Spec)]}
		if !f.Spec.Remote() {
			c.Missing = MissingRuntime(f.Spec.Command)
		}
		out = append(out, c)
	}
	return out
}

// sameKey identifies a server by what it runs, ignoring secrets.
func sameKey(s Spec) string {
	if s.Remote() {
		return strings.TrimRight(s.URL, "/")
	}
	return s.Command + " " + packageOf(s.Args)
}

// Destination says where a source's calls go: its name and host, and
// whether it is on the web rather than this computer.
func (m *Manager) Destination(id string) (name, host string, remote bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.servers[id]
	if !ok || !s.spec.Remote() {
		return "", "", false
	}
	host = s.spec.URL
	if u, err := url.Parse(s.spec.URL); err == nil {
		host = u.Hostname()
	}
	return s.spec.Name, host, true
}
