// Package enrich orchestrates TMDb enrichment over a pool of titles. It is the
// shared engine behind both the `import`/`refresh` CLI commands and the TUI's
// live progress views.
package enrich

import (
	"time"

	"github.com/jarvis1704/whatnext/internal/store"
	"github.com/jarvis1704/whatnext/internal/tmdb"
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

// Run enriches every unenriched title in d, and refreshes watch-provider data
// for any title whose cached providers came from a different region, mutating
// the pool in place. For each title it invokes onProgress (may be nil) so
// callers can animate. The caller is responsible for saving d afterward. A
// per-title error is reported through Progress.Err but does not abort the run.
func Run(d *store.Data, c *tmdb.Client, region string, onProgress func(Progress)) {
	targets := pending(d, region)
	total := len(targets)
	for i, t := range targets {
		if i > 0 {
			time.Sleep(politeDelay)
		}
		p := Progress{Done: i + 1, Total: total}

		if !t.Enriched {
			res, ok, err := c.Lookup(t.Name, t.Year)
			switch {
			case err != nil:
				p.Err = err
			case ok:
				t.TMDbID = res.TMDbID
				t.MediaType = res.MediaType
				t.Genres = res.Genres
				t.RuntimeMins = res.RuntimeMins
				t.Enriched = true
				p.Found = true
				fetchProviders(c, t, region)
			}
		} else {
			// Already enriched — just needs provider data for this region.
			fetchProviders(c, t, region)
			p.Found = true
		}
		p.Title = *t
		if onProgress != nil {
			onProgress(p)
		}
	}
}

// fetchProviders looks up watch providers for an already-identified title and
// caches the result. Errors are swallowed: providers are a nice-to-have, and
// a failed lookup here shouldn't be reported as a failed enrichment.
func fetchProviders(c *tmdb.Client, t *store.Title, region string) {
	provs, err := c.WatchProviders(t.MediaType, t.TMDbID, region)
	if err != nil {
		return
	}
	t.Providers = provs
	t.ProvidersRegion = region
}

// pending returns every title still needing work: unenriched titles first,
// then enriched titles whose provider cache is stale for region.
func pending(d *store.Data, region string) []*store.Title {
	targets := d.Unenriched()
	targets = append(targets, d.NeedsProviders(region)...)
	return targets
}

// CountUnenriched is a convenience for the UI's summary lines.
func CountUnenriched(d *store.Data) int {
	return len(d.Unenriched())
}

// CountPending is like CountUnenriched but also counts titles whose provider
// cache needs refreshing for region.
func CountPending(d *store.Data, region string) int {
	return len(pending(d, region))
}
