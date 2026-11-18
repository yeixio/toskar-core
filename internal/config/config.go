package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/yeixio/toskar-core/internal/auth"
)

// Config holds daemon runtime configuration.
type Config struct {
	DataDir       string `json:"data_dir"`
	DBPath        string `json:"db_path"`
	ModelsDir     string `json:"models_dir"`
	RuntimesDir   string `json:"runtimes_dir"`
	LogsDir       string `json:"logs_dir"`
	WebUIDir      string `json:"web_ui_dir"`
	APIHost       string `json:"api_host"`
	APIPort       int    `json:"api_port"`
	InternalHost  string `json:"internal_host"`
	InternalPort  int    `json:"internal_port"`
	LANAPIEnabled bool   `json:"lan_api_enabled"`
	// RemoteAccess is access from anywhere (#456): a listener for phones
	// outside the home network.
	RemoteAccess RemoteAccess `json:"remote_access,omitempty"`
	// APITLSCert and APITLSKey are the person's own certificate and key, in
	// PEM files, for HTTPS on the API (#213). Empty uses the certificate
	// Toskar makes.
	APITLSCert       string `json:"api_tls_cert,omitempty"`
	APITLSKey        string `json:"api_tls_key,omitempty"`
	WebUIEnabled     bool   `json:"web_ui_enabled"`
	DiscoveryEnabled bool   `json:"discovery_enabled"`
	NodeName         string `json:"node_name"`
	NodeID           string `json:"node_id,omitempty"`
	// AdvertiseHost is an optional reachable hostname/IP for Bifrost peers (e.g. Docker DNS name).
	AdvertiseHost string `json:"advertise_host,omitempty"`
	// StaticPeers are Bifrost host:port addresses to probe when mDNS is unavailable.
	StaticPeers []string `json:"static_peers,omitempty"`
	// Map services for places and routes; empty uses OpenStreetMap's public
	// Nominatim, Overpass, and routing.openstreetmap.de.
	PlacesGeocoderURL string `json:"places_geocoder_url,omitempty"`
	PlacesOverpassURL string `json:"places_overpass_url,omitempty"`
	PlacesRouterURL   string `json:"places_router_url,omitempty"`
	// Community model ratings (#37): the ratings service, and the public
	// summary used when it cannot be reached. Empty uses Yeix's.
	RatingsURL string `json:"ratings_url,omitempty"`
	// ExternalOpenAIURL is an OpenAI-compatible server whose models can be
	// chosen for a chat (#111); its API key is in the secrets folder.
	ExternalOpenAIURL string `json:"external_openai_url,omitempty"`
	// ComfyUIURL is a ComfyUI an Admin connected, whose saved workflows
	// run as a tool (#958); its token is in the secrets folder.
	ComfyUIURL        string `json:"comfyui_url,omitempty"`
	RatingsSummaryURL string `json:"ratings_summary_url,omitempty"`
	// TrustedProxies are the addresses (IPs or CIDRs) of reverse proxies
	// that sign people in and name them in headers (#206). Their headers
	// are believed only from these addresses.
	TrustedProxies []string `json:"trusted_proxies,omitempty"`
	// ProxyAuth says which headers a trusted proxy names the person in,
	// and how their groups become roles.
	ProxyAuth ProxyAuth `json:"proxy_auth,omitempty"`
	// OIDC is sign-in with an OpenID Connect provider (#206).
	OIDC OIDC `json:"oidc,omitempty"`
}

// OIDC is sign-in with an OpenID Connect provider, such as Google,
// Microsoft Entra ID, Okta, Authentik, Keycloak, or Authelia (#206). The
// client secret isn't kept here: it comes from TOSKAR_OIDC_CLIENT_SECRET
// or the file client_secret_file names.
type OIDC struct {
	Issuer           string   `json:"issuer,omitempty"`
	ClientID         string   `json:"client_id,omitempty"`
	ClientSecretFile string   `json:"client_secret_file,omitempty"`
	RedirectURL      string   `json:"redirect_url,omitempty"`
	Scopes           []string `json:"scopes,omitempty"`
	GroupsClaim      string   `json:"groups_claim,omitempty"`
	AdminGroups      []string `json:"admin_groups,omitempty"`
	MemberGroups     []string `json:"member_groups,omitempty"`
	DefaultRole      string   `json:"default_role,omitempty"`
	OwnerEmail       string   `json:"owner_email,omitempty"`
	OwnerSubject     string   `json:"owner_subject,omitempty"`
	Label            string   `json:"label,omitempty"`
}

// ProxyAuth is sign-in by a trusted reverse proxy, such as Authelia,
// Authentik, Cloudflare Access, or Tailscale (#206).
type ProxyAuth struct {
	// UserHeader names the signed-in person; empty is Remote-User.
	UserHeader string `json:"user_header,omitempty"`
	// NameHeader is their display name, when the proxy sends one; empty
	// is Remote-Name.
	NameHeader string `json:"name_header,omitempty"`
	// GroupsHeader lists their groups, separated by commas or |; empty is
	// Remote-Groups.
	GroupsHeader string `json:"groups_header,omitempty"`
	// AdminGroups and MemberGroups make someone in them an Admin or a
	// Member.
	AdminGroups  []string `json:"admin_groups,omitempty"`
	MemberGroups []string `json:"member_groups,omitempty"`
	// DefaultRole is everyone else's: member (the default), visitor, or
	// none to refuse them.
	DefaultRole string `json:"default_role,omitempty"`
	// OwnerUser is the proxy's name for the Owner.
	OwnerUser string `json:"owner_user,omitempty"`
}

// RemoteAccess is the listener for paired devices outside the home network
// (#456, docs/remote-access.md). It serves only the routes a phone uses,
// over TLS, to phone keys.
type RemoteAccess struct {
	Enabled bool `json:"enabled,omitempty"`
	// Port is the listener's port; 0 is DefaultRemotePort.
	Port int `json:"port,omitempty"`
	// Address is where it's reached from outside, when the person forwarded
	// a port by hand: a host name or IP, with a port when it differs.
	Address string `json:"address,omitempty"`
	// NoPortMapping stops Toskar asking the router to open a port; by
	// default it does, while access from anywhere is on and no forwarded
	// address is set.
	NoPortMapping bool `json:"no_port_mapping,omitempty"`
	// Relay is the relay's name, with a port when it isn't 443, for an
	// organization's own relay; empty is Toskar's. Its enrollment secret
	// is kept with the secrets, not here.
	Relay string `json:"relay,omitempty"`
}

// RemotePort is the remote listener's port.
func (c Config) RemotePort() int {
	if c.RemoteAccess.Port > 0 {
		return c.RemoteAccess.Port
	}
	return DefaultRemotePort
}

// Manager loads and persists configuration.
type Manager struct {
	mu   sync.RWMutex
	cfg  Config
	path string
}

// NewManager creates a config manager rooted at dataDir.
func NewManager(dataDir string) (*Manager, error) {
	if dataDir == "" {
		dataDir = DefaultDataDir()
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	path := filepath.Join(dataDir, "config.json")
	m := &Manager{path: path, cfg: DefaultConfig()}
	m.cfg.DataDir = dataDir
	m.cfg.DBPath = DBPathIn(dataDir)
	m.cfg.ModelsDir = filepath.Join(dataDir, "models")
	m.cfg.RuntimesDir = filepath.Join(dataDir, "runtimes")
	m.cfg.LogsDir = filepath.Join(dataDir, "logs")

	if _, err := os.Stat(path); err == nil {
		if err := m.load(); err != nil {
			return nil, err
		}
	} else {
		if err := m.Save(); err != nil {
			return nil, err
		}
	}

	for _, dir := range []string{m.cfg.ModelsDir, m.cfg.RuntimesDir, m.cfg.LogsDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create dir %s: %w", dir, err)
		}
	}
	return m, nil
}

// Get returns a copy of the current config.
func (m *Manager) Get() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg
}

// Update applies a mutator and persists.
func (m *Manager) Update(fn func(*Config)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	fn(&m.cfg)
	return m.saveLocked()
}

// Save persists the current config.
func (m *Manager) Save() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saveLocked()
}

func (m *Manager) load() error {
	data, err := os.ReadFile(m.path)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}
	// Preserve computed paths if empty in file. A file without data_dir
	// belongs to the directory it was read from, not the default one, so
	// --data-dir never falls back to the user's real data.
	defaults := DefaultConfig()
	if cfg.DataDir == "" {
		cfg.DataDir = filepath.Dir(m.path)
	}
	if cfg.DBPath == "" {
		cfg.DBPath = DBPathIn(cfg.DataDir)
	}
	if cfg.ModelsDir == "" {
		cfg.ModelsDir = filepath.Join(cfg.DataDir, "models")
	}
	if cfg.RuntimesDir == "" {
		cfg.RuntimesDir = filepath.Join(cfg.DataDir, "runtimes")
	}
	if cfg.LogsDir == "" {
		cfg.LogsDir = filepath.Join(cfg.DataDir, "logs")
	}
	if cfg.APIHost == "" {
		cfg.APIHost = defaults.APIHost
	}
	if cfg.APIPort == 0 {
		cfg.APIPort = defaults.APIPort
	}
	if cfg.InternalHost == "" {
		cfg.InternalHost = defaults.InternalHost
	}
	if cfg.InternalPort == 0 {
		cfg.InternalPort = defaults.InternalPort
	}
	if cfg.NodeName == "" {
		cfg.NodeName = defaults.NodeName
	}
	m.cfg = cfg
	return nil
}

func (m *Manager) saveLocked() error {
	data, err := json.MarshalIndent(m.cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	tmp := m.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err := os.Rename(tmp, m.path); err != nil {
		return fmt.Errorf("finalize config: %w", err)
	}
	return nil
}

// APIAddr returns host:port for the public API.
func (c Config) APIAddr() string {
	return fmt.Sprintf("%s:%d", c.APIHost, c.APIPort)
}

// InternalAddr returns host:port for the Bifrost node protocol.
func (c Config) InternalAddr() string {
	return fmt.Sprintf("%s:%d", c.InternalHost, c.InternalPort)
}

// Proxy is sign-in by the trusted reverse proxies (#206), or nil when none
// are listed.
func (c Config) Proxy() (*auth.Proxy, error) {
	return auth.NewProxy(auth.ProxySettings{
		Trusted:      c.TrustedProxies,
		UserHeader:   c.ProxyAuth.UserHeader,
		NameHeader:   c.ProxyAuth.NameHeader,
		GroupsHeader: c.ProxyAuth.GroupsHeader,
		AdminGroups:  c.ProxyAuth.AdminGroups,
		MemberGroups: c.ProxyAuth.MemberGroups,
		DefaultRole:  c.ProxyAuth.DefaultRole,
		OwnerUser:    c.ProxyAuth.OwnerUser,
	})
}

// OIDCSettings are the provider's settings with the client secret, from
// TOSKAR_OIDC_CLIENT_SECRET or client_secret_file.
func (c Config) OIDCSettings() (auth.OIDCSettings, error) {
	secret := Env("OIDC_CLIENT_SECRET")
	if secret == "" && c.OIDC.ClientSecretFile != "" {
		b, err := os.ReadFile(c.OIDC.ClientSecretFile)
		if err != nil {
			return auth.OIDCSettings{}, fmt.Errorf("oidc.client_secret_file: %w", err)
		}
		secret = strings.TrimSpace(string(b))
	}
	return auth.OIDCSettings{
		Issuer: c.OIDC.Issuer, ClientID: c.OIDC.ClientID, ClientSecret: secret, RedirectURL: c.OIDC.RedirectURL,
		Scopes: c.OIDC.Scopes, GroupsClaim: c.OIDC.GroupsClaim, AdminGroups: c.OIDC.AdminGroups, MemberGroups: c.OIDC.MemberGroups,
		DefaultRole: c.OIDC.DefaultRole, OwnerEmail: c.OIDC.OwnerEmail, OwnerSubject: c.OIDC.OwnerSubject, Label: c.OIDC.Label,
	}, nil
}
