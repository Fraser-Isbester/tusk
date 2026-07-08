package config

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/fraser-isbester/tusk/internal/rules"
)

// Config holds all configuration profiles for Tusk.
type Config struct {
	Profiles       map[string]Profile `yaml:"profiles"`
	DefaultProfile string             `yaml:"default_profile"`
}

// Profile represents a single PostgreSQL connection profile. Optional fields
// use omitempty so profiles written back to disk (by the setup screen) stay
// clean and don't bake in defaults.
type Profile struct {
	Host            string             `yaml:"host,omitempty"`
	Port            int                `yaml:"port,omitempty"`
	User            string             `yaml:"user,omitempty"`
	Password        string             `yaml:"password,omitempty"`
	Database        string             `yaml:"database,omitempty"`
	SSLMode         string             `yaml:"sslmode,omitempty"`
	URL             string             `yaml:"url,omitempty"`
	Readonly        bool               `yaml:"readonly,omitempty"`
	Color           string             `yaml:"color,omitempty"`
	RefreshInterval time.Duration      `yaml:"refresh_interval,omitempty"`
	Connect         *ConnectConfig     `yaml:"connect,omitempty"`
	Rules           []rules.RuleConfig `yaml:"rules,omitempty"`
}

// ConnectConfig describes how tusk should reach a database that isn't directly
// accessible — e.g. a kubectl port-forward into a VPC. When Via is empty or
// "direct", the profile's URL/fields are used as-is. For tunnel methods
// (kube-port-forward, exec) the profile supplies credentials and database name
// while the tunnel supplies the local host:port.
type ConnectConfig struct {
	Via        string   `yaml:"via,omitempty"`         // "" | direct | kube-port-forward | exec
	Context    string   `yaml:"context,omitempty"`     // kube: kubeconfig context
	Namespace  string   `yaml:"namespace,omitempty"`   // kube: namespace
	Target     string   `yaml:"target,omitempty"`      // kube: svc/pod/deploy/statefulset
	RemotePort int      `yaml:"remote_port,omitempty"` // kube: remote port (default 5432)
	LocalPort  int      `yaml:"local_port,omitempty"`  // 0 = auto-pick a free port
	Command    []string `yaml:"command,omitempty"`     // exec: argv, {local_port} substituted
}

// ConnectionString returns a PostgreSQL connection string for this profile.
// If URL is set directly, it is returned as-is. Otherwise, the string is
// assembled from the individual fields using localhost/5432 defaults.
func (p Profile) ConnectionString() string {
	if p.URL != "" {
		return p.URL
	}
	host := p.Host
	if host == "" {
		host = "localhost"
	}
	port := p.Port
	if port == 0 {
		port = 5432
	}
	return p.DSN(host, port)
}

// DSN builds a connection string against the given host and port using the
// profile's credentials, database, and sslmode (ignoring Host/Port/URL). It is
// used when a tunnel supplies the local endpoint.
func (p Profile) DSN(host string, port int) string {
	user := p.User
	if user == "" {
		user = "postgres"
	}
	db := p.Database
	if db == "" {
		db = "postgres"
	}
	sslmode := p.SSLMode
	if sslmode == "" {
		sslmode = "disable"
	}
	if p.Password != "" {
		return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
			url.PathEscape(user), url.PathEscape(p.Password), host, port, db, sslmode)
	}
	return fmt.Sprintf("postgres://%s@%s:%d/%s?sslmode=%s",
		user, host, port, db, sslmode)
}

// configPath returns the default configuration file path (~/.config/tusk/config.yaml).
func configPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("determining home directory: %w", err)
	}
	return filepath.Join(home, ".config", "tusk", "config.yaml"), nil
}

// Load reads the Tusk configuration. It tries, in order:
//  1. ~/.config/tusk/config.yaml
//  2. DATABASE_URL environment variable
//  3. PG* environment variables (PGHOST, PGPORT, PGUSER, PGPASSWORD, PGDATABASE)
//  4. Sensible defaults (localhost:5432, user postgres)
func Load() (*Config, error) {
	// Try config file first.
	cfgPath, err := configPath()
	if err == nil {
		data, readErr := os.ReadFile(cfgPath) //nolint:gosec // path from configPath(), not user input
		if readErr == nil {
			cfg := &Config{}
			if parseErr := yaml.Unmarshal(data, cfg); parseErr != nil {
				return nil, fmt.Errorf("parsing config %s: %w", cfgPath, parseErr)
			}
			applyProfileDefaults(cfg)
			return cfg, nil
		}
	}

	// Fall back to environment / defaults.
	profile := profileFromEnv()
	cfg := &Config{
		Profiles:       map[string]Profile{"default": profile},
		DefaultProfile: "default",
	}
	return cfg, nil
}

// profileFromEnv builds a Profile from environment variables, falling back to
// sensible defaults when the variables are not set.
func profileFromEnv() Profile {
	p := Profile{
		Host:            envOrDefault("PGHOST", "localhost"),
		User:            envOrDefault("PGUSER", "postgres"),
		Password:        os.Getenv("PGPASSWORD"),
		Database:        envOrDefault("PGDATABASE", "postgres"),
		SSLMode:         envOrDefault("PGSSLMODE", "disable"),
		Port:            5432,
		RefreshInterval: 2 * time.Second,
	}

	if url := os.Getenv("DATABASE_URL"); url != "" {
		p.URL = url
	}

	if portStr := os.Getenv("PGPORT"); portStr != "" {
		if v, err := strconv.Atoi(portStr); err == nil {
			p.Port = v
		}
	}

	return p
}

// ResolveProfile returns the named profile from the config. If name is empty,
// the default profile is used. If no profiles are configured, a profile is
// built from environment variables and defaults.
func (c *Config) ResolveProfile(name string) (Profile, error) {
	if name == "" {
		name = c.DefaultProfile
	}
	if name == "" {
		name = "default"
	}
	p, ok := c.Profiles[name]
	if !ok {
		return Profile{}, fmt.Errorf("profile %q not found", name)
	}
	return p, nil
}

// applyProfileDefaults fills in zero-value fields with sensible defaults for
// every profile in the config.
func applyProfileDefaults(cfg *Config) {
	if cfg.Profiles == nil {
		cfg.Profiles = make(map[string]Profile)
	}
	for name, p := range cfg.Profiles {
		if p.Port == 0 {
			p.Port = 5432
		}
		if p.RefreshInterval == 0 {
			p.RefreshInterval = 2 * time.Second
		}
		cfg.Profiles[name] = p
	}
	if cfg.DefaultProfile == "" && len(cfg.Profiles) > 0 {
		// If there is exactly one profile, make it the default.
		if len(cfg.Profiles) == 1 {
			for k := range cfg.Profiles {
				cfg.DefaultProfile = k
			}
		}
	}
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// SaveProfileRules writes ruleConfigs back to the given profile in
// ~/.config/tusk/config.yaml, replacing only that profile's `rules:` sequence.
//
// It edits the file's YAML node tree in place rather than re-marshaling the
// whole Config, so comments, key ordering, and every other field (other
// profiles, connection URLs, env-derived values) are preserved untouched.
// Because only the rules subtree is rewritten, defaults injected by
// applyProfileDefaults (port, refresh_interval) and env-derived passwords are
// never persisted. The write is atomic (temp file + rename).
func SaveProfileRules(profileName string, ruleConfigs []rules.RuleConfig) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	root, err := loadConfigRoot(path)
	if err != nil {
		return err
	}

	profiles := ensureProfiles(root)
	profile := mapValue(profiles, profileName)
	if profile == nil || profile.Kind != yaml.MappingNode {
		profile = &yaml.Node{Kind: yaml.MappingNode}
		setMapValue(profiles, profileName, profile)
	}

	rulesNode, err := toNode(ruleConfigs)
	if err != nil {
		return fmt.Errorf("encoding rules: %w", err)
	}
	setMapValue(profile, "rules", rulesNode)
	ensureDefaultProfile(root, profileName)

	return writeConfigRoot(path, root)
}

// SaveProfile writes p to profiles.<name> in ~/.config/tusk/config.yaml,
// preserving comments and every other profile (same node-edit approach as
// SaveProfileRules). An existing profile's rules: node is carried over when p
// has no rules, so saving connection details from the setup screen never drops
// rules a user added via the editor. The write is atomic.
func SaveProfile(name string, p Profile) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	root, err := loadConfigRoot(path)
	if err != nil {
		return err
	}

	profiles := ensureProfiles(root)
	profNode, err := toNode(p)
	if err != nil {
		return fmt.Errorf("encoding profile: %w", err)
	}
	// Preserve existing rules if we're only updating connection details.
	if existing := mapValue(profiles, name); existing != nil && existing.Kind == yaml.MappingNode && len(p.Rules) == 0 {
		if r := mapValue(existing, "rules"); r != nil {
			setMapValue(profNode, "rules", r)
		}
	}
	setMapValue(profiles, name, profNode)
	ensureDefaultProfile(root, name)

	return writeConfigRoot(path, root)
}

// loadConfigRoot reads the config file into its root mapping node (preserving
// comments), or returns a fresh mapping node if the file does not exist.
func loadConfigRoot(path string) (*yaml.Node, error) {
	root := &yaml.Node{Kind: yaml.MappingNode}
	data, err := os.ReadFile(path) //nolint:gosec // path from configPath(), not user input
	if err != nil {
		if os.IsNotExist(err) {
			return root, nil
		}
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}
	if len(doc.Content) > 0 && doc.Content[0].Kind == yaml.MappingNode {
		root = doc.Content[0]
	}
	return root, nil
}

// writeConfigRoot atomically writes root to path with 2-space indent (matching
// the documented config style and minimizing diffs).
func writeConfigRoot(path string, root *yaml.Node) error {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(root); err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	_ = enc.Close()

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	return os.Rename(tmp, path)
}

// ensureProfiles returns the profiles mapping node, creating it if absent.
func ensureProfiles(root *yaml.Node) *yaml.Node {
	profiles := mapValue(root, "profiles")
	if profiles == nil || profiles.Kind != yaml.MappingNode {
		profiles = &yaml.Node{Kind: yaml.MappingNode}
		setMapValue(root, "profiles", profiles)
	}
	return profiles
}

// ensureDefaultProfile sets default_profile to name when it is not already set,
// so a freshly created file round-trips.
func ensureDefaultProfile(root *yaml.Node, name string) {
	if mapValue(root, "default_profile") == nil {
		setMapValue(root, "default_profile", &yaml.Node{
			Kind: yaml.ScalarNode, Tag: "!!str", Value: name,
		})
	}
}

// mapValue returns the value node for key in a mapping node, or nil if absent.
// Mapping content alternates key, value, key, value, ...
func mapValue(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// setMapValue replaces the value for key in a mapping node, or appends the
// key/value pair if the key is absent.
func setMapValue(m *yaml.Node, key string, val *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1] = val
			return
		}
	}
	m.Content = append(m.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		val,
	)
}

// toNode marshals v and returns its top-level node (unwrapping the document).
func toNode(v any) (*yaml.Node, error) {
	data, err := yaml.Marshal(v)
	if err != nil {
		return nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if len(doc.Content) == 0 {
		return &yaml.Node{Kind: yaml.SequenceNode}, nil
	}
	return doc.Content[0], nil
}
