// Package mood maps human "mood" labels to lists of TMDb genre names. Defaults
// ship built-in and are written to ~/.whatnext/moods.json on first run; after
// that the file is the source of truth so users can add their own moods.
package mood

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"

	"github.com/jarvis1704/whatnext/internal/store"
)

// Defaults are the built-in mood → genre mappings.
func Defaults() map[string][]string {
	return map[string][]string{
		"cozy":        {"Comedy", "Family", "Animation", "Romance"},
		"intense":     {"Thriller", "Crime", "Action", "War"},
		"mindbending": {"Science Fiction", "Mystery", "Thriller"},
		"sad":         {"Drama", "Romance"},
		"scary":       {"Horror", "Thriller"},
		"epic":        {"Adventure", "Fantasy", "History", "Action"},
		"laugh":       {"Comedy"},
	}
}

// Config is the on-disk moods.json shape.
type Config struct {
	Moods map[string][]string `json:"moods"`
}

func path() (string, error) {
	dir, err := store.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "moods.json"), nil
}

// Load returns the mood config, writing defaults to disk on first run.
func Load() (*Config, error) {
	p, err := path()
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		cfg := &Config{Moods: Defaults()}
		if err := cfg.Save(); err != nil {
			return nil, err
		}
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	if cfg.Moods == nil {
		cfg.Moods = Defaults()
	}
	return &cfg, nil
}

// Save writes the config as pretty JSON.
func (c *Config) Save() error {
	p, err := path()
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, raw, 0o644)
}

// Genres returns the genre list for a mood, and whether the mood exists.
func (c *Config) Genres(name string) ([]string, bool) {
	g, ok := c.Moods[name]
	return g, ok
}

// Names returns the mood labels, sorted for stable display.
func (c *Config) Names() []string {
	names := make([]string, 0, len(c.Moods))
	for k := range c.Moods {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}
