// Package config reads and writes ytrack's configuration directory:
//
//	config.yml  general preferences
//	hosts.yml   per-host metadata and the default host (never secrets)
//
// The directory is $YTRACK_CONFIG_DIR, else $XDG_CONFIG_HOME/ytrack, else
// %AppData%/ytrack on Windows, else ~/.config/ytrack.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"

	"gopkg.in/yaml.v3"
)

const (
	configFile = "config.yml"
	hostsFile  = "hosts.yml"
)

// Storage values record where a host's token lives.
const (
	StorageKeyring = "keyring"
	StorageFile    = "file"
)

// HostEntry is the non-secret metadata kept for a logged-in host.
type HostEntry struct {
	URL     string `yaml:"url"`
	User    string `yaml:"user,omitempty"`
	Auth    string `yaml:"auth,omitempty"`
	Storage string `yaml:"storage,omitempty"`
}

type preferences struct {
	Browser string `yaml:"browser,omitempty"`
	// Timezone is the IANA zone that decides today's date for work items
	// (`work-item add --date auto`); empty means the process timezone.
	Timezone string `yaml:"timezone,omitempty"`
}

type hostsDoc struct {
	DefaultHost string                `yaml:"default_host,omitempty"`
	Hosts       map[string]*HostEntry `yaml:"hosts,omitempty"`
}

// Config is the loaded configuration.
type Config struct {
	dir   string
	prefs preferences
	hosts hostsDoc
}

// Dir returns the configuration directory.
func Dir() (string, error) {
	if d := os.Getenv("YTRACK_CONFIG_DIR"); d != "" {
		return d, nil
	}
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "ytrack"), nil
	}
	if runtime.GOOS == "windows" {
		if d := os.Getenv("AppData"); d != "" {
			return filepath.Join(d, "ytrack"), nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot locate the config directory: %w", err)
	}
	return filepath.Join(home, ".config", "ytrack"), nil
}

// Load reads the configuration from Dir. Missing files mean empty config.
func Load() (*Config, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	return LoadFrom(dir)
}

// LoadFrom reads the configuration from dir.
func LoadFrom(dir string) (*Config, error) {
	c := &Config{dir: dir}
	if err := readYAML(filepath.Join(dir, configFile), &c.prefs); err != nil {
		return nil, err
	}
	if err := readYAML(filepath.Join(dir, hostsFile), &c.hosts); err != nil {
		return nil, err
	}
	if c.hosts.Hosts == nil {
		c.hosts.Hosts = map[string]*HostEntry{}
	}
	return c, nil
}

// Dir returns the directory this config was loaded from.
func (c *Config) Dir() string { return c.dir }

// Browser returns the configured browser launcher, if any.
func (c *Config) Browser() string { return c.prefs.Browser }

// Timezone returns the configured work timezone, or "".
func (c *Config) Timezone() string { return c.prefs.Timezone }

// DefaultHost returns the key of the default host, or "".
func (c *Config) DefaultHost() string { return c.hosts.DefaultHost }

// SetDefaultHost makes key the default host. The host must be known.
func (c *Config) SetDefaultHost(key string) error {
	if key != "" && c.hosts.Hosts[key] == nil {
		return fmt.Errorf("not logged in to %s", key)
	}
	c.hosts.DefaultHost = key
	return nil
}

// Host returns the entry for key, or nil.
func (c *Config) Host(key string) *HostEntry { return c.hosts.Hosts[key] }

// HostKeys returns the known host keys, sorted.
func (c *Config) HostKeys() []string {
	keys := make([]string, 0, len(c.hosts.Hosts))
	for k := range c.hosts.Hosts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// SetHost adds or replaces a host entry. The first host becomes the default.
func (c *Config) SetHost(key string, entry HostEntry) {
	c.hosts.Hosts[key] = &entry
	if c.hosts.DefaultHost == "" {
		c.hosts.DefaultHost = key
	}
}

// RemoveHost deletes a host entry. If it was the default, the first remaining
// host (sorted) becomes the default.
func (c *Config) RemoveHost(key string) {
	delete(c.hosts.Hosts, key)
	if c.hosts.DefaultHost == key {
		c.hosts.DefaultHost = ""
		if keys := c.HostKeys(); len(keys) > 0 {
			c.hosts.DefaultHost = keys[0]
		}
	}
}

// Save writes hosts.yml and config.yml with owner-only permissions.
func (c *Config) Save() error {
	if err := writeYAML(filepath.Join(c.dir, hostsFile), c.hosts); err != nil {
		return err
	}
	if c.prefs != (preferences{}) {
		return writeYAML(filepath.Join(c.dir, configFile), c.prefs)
	}
	return nil
}

func readYAML(path string, v any) error {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(b, v); err != nil {
		return fmt.Errorf("invalid %s: %w", path, err)
	}
	return nil
}

func writeYAML(path string, v any) error {
	b, err := yaml.Marshal(v)
	if err != nil {
		return err
	}
	return WriteFileAtomic(path, b)
}

// WriteFileAtomic writes data to path with mode 0600, creating the parent
// directory with mode 0700, via a temp file and rename.
func WriteFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }()
	if err := f.Chmod(0o600); err != nil && runtime.GOOS != "windows" {
		_ = f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
