// Package store is the pure persistence layer for whatnext. It owns the
// Title model and reads/writes the enriched watchlist pool as flat JSON
// under ~/.whatnext/data.json.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
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
	VoteAverage   float64  `json:"vote_average,omitempty"` // TMDb rating, 0-10
	Popularity    float64  `json:"popularity,omitempty"`   // TMDb popularity score

	// Director is the movie's director, or a tv show's "created by" credit
	// (joined) since tv has no single director. Cast is the top-billed cast,
	// in credited order. Language/Country are friendly display names.
	Director string   `json:"director,omitempty"`
	Cast     []string `json:"cast,omitempty"`
	Language string   `json:"language,omitempty"`
	Country  string   `json:"country,omitempty"`

	// TV commitment data (zero for movies). For tv, RuntimeMins is one
	// episode's length; TotalRuntimeMins estimates the whole show.
	// TVStatus is TMDb's status ("Ended", "Returning Series", …); an empty
	// TVStatus on an enriched show means it predates these fields and needs
	// a details refresh.
	Seasons  int    `json:"seasons,omitempty"`
	Episodes int    `json:"episodes,omitempty"`
	TVStatus string `json:"tv_status,omitempty"`
	TVType   string `json:"tv_type,omitempty"` // "Scripted", "Miniseries", …

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

// IsTV reports whether t is a tv show.
func (t Title) IsTV() bool { return t.MediaType == "tv" }

// TotalRuntimeMins estimates the time to watch all of t: the runtime for a
// movie, episodes × episode runtime for a show. 0 means unknown.
func (t Title) TotalRuntimeMins() int {
	if t.IsTV() {
		return t.Episodes * t.RuntimeMins
	}
	return t.RuntimeMins
}

// IsFinished reports whether a show has stopped airing (ended or canceled),
// i.e. there is a fixed amount of it to watch.
func (t Title) IsFinished() bool {
	return t.TVStatus == "Ended" || t.TVStatus == "Canceled"
}

// IsMiniseries reports whether a show is a limited series: TMDb types it
// "Miniseries", or it finished after a single season.
func (t Title) IsMiniseries() bool {
	return t.IsTV() && (t.TVType == "Miniseries" || (t.Seasons == 1 && t.IsFinished()))
}

// SeriesSummary describes a show's size for display, e.g.
// ["3 seasons", "30 eps", "~22h total", "Ended"]. Empty for movies and for
// shows without season data yet.
func (t Title) SeriesSummary() []string {
	if !t.IsTV() || (t.Seasons == 0 && t.TVStatus == "") {
		return nil
	}
	var out []string
	if t.Seasons > 0 {
		out = append(out, plural(t.Seasons, "season"))
	}
	if t.Episodes > 0 {
		out = append(out, fmt.Sprintf("%d eps", t.Episodes))
	}
	if total := t.TotalRuntimeMins(); total > 0 {
		if total >= 60 {
			out = append(out, fmt.Sprintf("~%dh total", (total+30)/60))
		} else {
			out = append(out, fmt.Sprintf("~%dm total", total))
		}
	}
	switch {
	case t.TVType == "Miniseries":
		out = append(out, "Miniseries")
	case t.TVStatus == "Returning Series":
		out = append(out, "Returning")
	case t.TVStatus != "":
		out = append(out, t.TVStatus)
	}
	return out
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// TMDbKey is the pool key for a title added straight from TMDb rather than a
// Letterboxd export. It lives in LetterboxdURI so Upsert's dedupe, undo, and
// mark-watched keep working unchanged.
func TMDbKey(mediaType string, id int) string {
	return fmt.Sprintf("tmdb:%s:%d", mediaType, id)
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

// Upsert appends t only if it is not already present — same LetterboxdURI,
// or same TMDb identity (so adding a title by search never duplicates one
// imported from Letterboxd). It reports whether the title was newly added.
// This is what makes re-import idempotent: existing enrichment and watched
// flags are never clobbered.
func (d *Data) Upsert(t Title) bool {
	if d.Find(t) >= 0 {
		return false
	}
	if t.AddedAt.IsZero() {
		t.AddedAt = time.Now()
	}
	d.Titles = append(d.Titles, t)
	return true
}

// Find returns the index of the pool entry matching t by LetterboxdURI or
// TMDb identity, or -1.
func (d *Data) Find(t Title) int {
	for i := range d.Titles {
		e := &d.Titles[i]
		if e.LetterboxdURI != "" && e.LetterboxdURI == t.LetterboxdURI {
			return i
		}
		if t.TMDbID != 0 && e.TMDbID == t.TMDbID && e.MediaType == t.MediaType {
			return i
		}
	}
	return -1
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

// NeedsTVDetails returns enriched shows missing season/episode/status data
// (enriched before those fields existed).
func (d *Data) NeedsTVDetails() []*Title {
	var out []*Title
	for i := range d.Titles {
		t := &d.Titles[i]
		if t.Enriched && t.IsTV() && t.TVStatus == "" {
			out = append(out, t)
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
