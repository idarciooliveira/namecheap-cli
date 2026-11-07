// Package config loads and saves Namecheap API credentials.
//
// Credentials live in <user config dir>/ncli/config.json (0600). Environment
// variables override the file so agents and CI can run without it.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrNotConfigured means no API user or key was found in the file or env.
var ErrNotConfigured = errors.New("namecheap credentials not configured")

type Config struct {
	APIUser  string `json:"apiUser"`
	APIKey   string `json:"apiKey"`
	Username string `json:"username"`
	Sandbox  bool   `json:"sandbox"`
	ClientIP string `json:"clientIp,omitempty"`
}

// Dir returns the config directory. NCLI_CONFIG_DIR overrides it.
func Dir() (string, error) {
	if d := os.Getenv("NCLI_CONFIG_DIR"); d != "" {
		return d, nil
	}
	d, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "ncli"), nil
}

func Path() (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "config.json"), nil
}

// LoadFile reads the config file only. A missing file returns an empty config.
func LoadFile() (*Config, error) {
	p, err := Path()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return &Config{}, nil
	}
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("config %s is not valid JSON: %w", p, err)
	}
	return &c, nil
}

// Load reads the config file and applies environment overrides.
func Load() (*Config, error) {
	c, err := LoadFile()
	if err != nil {
		return nil, err
	}
	if v := os.Getenv("NAMECHEAP_API_USER"); v != "" {
		c.APIUser = v
	}
	if v := os.Getenv("NAMECHEAP_API_KEY"); v != "" {
		c.APIKey = v
	}
	if v := os.Getenv("NAMECHEAP_USERNAME"); v != "" {
		c.Username = v
	}
	if v := os.Getenv("NAMECHEAP_CLIENT_IP"); v != "" {
		c.ClientIP = v
	}
	if v := os.Getenv("NAMECHEAP_SANDBOX"); v != "" {
		c.Sandbox = isTrue(v)
	}
	if c.Username == "" {
		c.Username = c.APIUser
	}
	return c, nil
}

func (c *Config) Validate() error {
	if c.APIUser == "" || c.APIKey == "" {
		return ErrNotConfigured
	}
	return nil
}

func (c *Config) Environment() string {
	if c.Sandbox {
		return "sandbox"
	}
	return "production"
}

func Save(c *Config) (string, error) {
	p, err := Path()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return "", err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return "", err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, p); err != nil {
		return "", err
	}
	return p, os.Chmod(p, 0o600)
}

func Remove() (string, error) {
	p, err := Path()
	if err != nil {
		return "", err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return p, nil
}

func isTrue(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
