// Package store is the pure persistence layer for whatnext. It owns the
// Title model and reads/writes the enriched watchlist pool as flat JSON
// under ~/.whatnext/data.json.
package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
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

	// AddedAt is when this title entered the whatnext pool, set the moment
	// Upsert first accepts it. Used by --weighted picks to favor titles
	// that have sat unwatched the longest.
	AddedAt time.Time `json:"added_at"`

	// Providers lists the streaming services offering this title on a
	// subscription (flatrate), for ProvidersRegion. Empty means "checked,
	// nothing found" — nil/unset ProvidersRegion means "never checked".
	Providers       []string `json:"providers,omitempty"`
	ProvidersRegion string   `json:"providers_region,omitempty"`
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

// undoPath returns the absolute path to undo.json.
func undoPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "undo.json"), nil
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
	if t.AddedAt.IsZero() {
		t.AddedAt = time.Now()
	}
	d.Titles = append(d.Titles, t)
	return true
}

// UndoRecord captures a title's watched state just before it was changed, so
// a single subsequent change can be reverted.
type UndoRecord struct {
	URI     string `json:"uri"`
	Name    string `json:"name"`
	Watched bool   `json:"watched"` // value to restore
}

// RecordUndo persists t's pre-change watched state, overwriting any previous
// record. Call it right before flipping t.Watched.
func RecordUndo(t Title) error {
	path, err := undoPath()
	if err != nil {
		return err
	}
	raw, err := json.Marshal(UndoRecord{URI: t.LetterboxdURI, Name: t.Name, Watched: t.Watched})
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

// Undo reverts the last watched-state change recorded by RecordUndo, if any,
// and clears the record so it can't be applied twice. ok reports whether
// there was anything to undo.
func (d *Data) Undo() (name string, ok bool, err error) {
	path, err := undoPath()
	if err != nil {
		return "", false, err
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	var rec UndoRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return "", false, err
	}
	for i := range d.Titles {
		if d.Titles[i].LetterboxdURI == rec.URI {
			d.Titles[i].Watched = rec.Watched
			name = d.Titles[i].Name
			ok = true
			break
		}
	}
	if !ok {
		return "", false, nil
	}
	_ = os.Remove(path)
	return name, true, nil
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

// NeedsProviders returns pointers to every enriched title whose cached
// watch-provider data is missing or was fetched for a different region than
// the one given.
func (d *Data) NeedsProviders(region string) []*Title {
	var out []*Title
	for i := range d.Titles {
		t := &d.Titles[i]
		if t.Enriched && t.ProvidersRegion != region {
			out = append(out, t)
		}
	}
	return out
}
