// Package enrich orchestrates TMDb enrichment over a pool of titles. It is the
// shared engine behind both the `import`/`refresh` CLI commands and the TUI's
// live progress views.
package enrich

import (
	"time"

	"whatnext/internal/store"
	"whatnext/internal/tmdb"
)

// politeDelay throttles requests to be a good API citizen (plan §8).
const politeDelay = 250 * time.Millisecond

// Progress reports the outcome of enriching one title.
type Progress struct {
	Title store.Title
	Found bool
	Err   error
	Done  int // 1-based index of this title
	Total int
}

// Run enriches every unenriched title in d, mutating the pool in place. For
// each title it invokes onProgress (may be nil) so callers can animate. The
// caller is responsible for saving d afterward. A per-title error is reported
// through Progress.Err but does not abort the run.
func Run(d *store.Data, c *tmdb.Client, onProgress func(Progress)) {
	targets := d.Unenriched()
	total := len(targets)
	for i, t := range targets {
		if i > 0 {
			time.Sleep(politeDelay)
		}
		p := Progress{Done: i + 1, Total: total}

		res, ok, err := c.Lookup(t.Name, t.Year)
		if err != nil {
			p.Err = err
		} else if ok {
			t.TMDbID = res.TMDbID
			t.MediaType = res.MediaType
			t.Genres = res.Genres
			t.RuntimeMins = res.RuntimeMins
			t.Enriched = true
			p.Found = true
		}
		p.Title = *t
		if onProgress != nil {
			onProgress(p)
		}
	}
}

// CountUnenriched is a convenience for the UI's summary lines.
func CountUnenriched(d *store.Data) int {
	return len(d.Unenriched())
}
