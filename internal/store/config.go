package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Config holds user settings persisted to ~/.whatnext/config.json. Its presence
// on disk also marks that first-run onboarding has completed.
type Config struct {
	TMDbKey string `json:"tmdb_api_key"`
}

func configPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// ConfigExists reports whether config.json is present (i.e. onboarding is done).
func ConfigExists() bool {
	p, err := configPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

// LoadConfig reads config.json, returning an empty Config if it doesn't exist.
func LoadConfig() (*Config, error) {
	p, err := configPath()
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return &Config{}, nil
	}
	if err != nil {
		return nil, err
	}
	var c Config
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &c); err != nil {
			return nil, err
		}
	}
	return &c, nil
}

// Save writes config.json with 0600 perms since it may hold an API key.
func (c *Config) Save() error {
	p, err := configPath()
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, raw, 0o600)
}

// ResolveKey returns the TMDb API key, preferring the TMDB_API_KEY environment
// variable and falling back to the stored config. Empty string means no key.
func ResolveKey() string {
	if k := os.Getenv("TMDB_API_KEY"); k != "" {
		return k
	}
	if c, err := LoadConfig(); err == nil {
		return c.TMDbKey
	}
	return ""
}
