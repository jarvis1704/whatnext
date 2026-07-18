// Package store is the pure persistence layer for whatnext. It owns the
// Title model and reads/writes the enriched watchlist pool as flat JSON
// under ~/.whatnext/data.json.
package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Title is one entry in the watchlist pool.
type Title struct {
	Name          string   `json:"name"`
	Year          string   `json:"year"`
	LetterboxdURI string   `json:"letterboxd_uri"` // unique key, used to dedupe on import
	TMDbID        int      `json:"tmdb_id"`
	MediaType     string   `json:"media_type"` // "movie" | "tv"
	Genres        []string `json:"genres"`
	RuntimeMins   int      `json:"runtime_mins"`
	Enriched      bool     `json:"enriched"`
	Watched       bool     `json:"watched"`
}

// Data is the on-disk shape of data.json.
type Data struct {
	Titles []Title `json:"titles"`
}

// Dir returns the whatnext home directory (~/.whatnext), creating it if
// necessary.
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".whatnext")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// dataPath returns the absolute path to data.json.
func dataPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "data.json"), nil
}

// Load reads the pool from disk. A missing file is not an error: the first
// run simply starts with an empty pool.
func Load() (*Data, error) {
	path, err := dataPath()
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &Data{}, nil
	}
	if err != nil {
		return nil, err
	}
	var d Data
	if len(raw) == 0 {
		return &Data{}, nil
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// Save writes the pool back to disk as pretty-printed JSON, atomically via a
// temp file + rename so a crash mid-write never corrupts the pool.
func (d *Data) Save() error {
	path, err := dataPath()
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Upsert appends t only if its LetterboxdURI is not already present. It
// reports whether the title was newly added. This is what makes re-import
// idempotent: existing enrichment and watched flags are never clobbered.
func (d *Data) Upsert(t Title) bool {
	for i := range d.Titles {
		if d.Titles[i].LetterboxdURI != "" && d.Titles[i].LetterboxdURI == t.LetterboxdURI {
			return false
		}
	}
	d.Titles = append(d.Titles, t)
	return true
}

// Unenriched returns pointers to every pool entry still awaiting a
// successful TMDb lookup.
func (d *Data) Unenriched() []*Title {
	var out []*Title
	for i := range d.Titles {
		if !d.Titles[i].Enriched {
			out = append(out, &d.Titles[i])
		}
	}
	return out
}
