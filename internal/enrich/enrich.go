// Package enrich orchestrates TMDb enrichment over a pool of titles. It is the
// shared engine behind both the `import`/`refresh` CLI commands and the TUI's
// live progress views.
//
// Run enriches concurrently: many titles' TMDb lookups are in flight at
// once, throttled by a shared rate limiter rather than a single sequential
// sleep, and identity lookups (genre/runtime/rating) are cached to disk so
// repeat runs over the same titles skip the network entirely.
package enrich

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/jarvis1704/whatnext/internal/store"
	"github.com/jarvis1704/whatnext/internal/tmdb"
)

// politeDelay is the minimum gap between successive TMDb request sequences
// across ALL workers combined — the same politeness budget a plain
// sequential loop would enforce with time.Sleep, just shared via a limiter
// so multiple titles' multi-request sequences can overlap in flight instead
// of waiting on each other's full round trip.
const politeDelay = 250 * time.Millisecond

// MaxWorkers caps how many titles are enriched concurrently.
const MaxWorkers = 8

// Job is one unit of enrichment work, keyed by an opaque ID the caller
// assigns (typically a pool index) so results can be routed back safely.
// Workers never touch *store.Title directly — only Result values cross the
// channel — so this is safe to drive from a UI goroutine that's
// concurrently rendering the same pool.
type Job struct {
	ID   int
	Name string
	Year string

	// ProvidersOnly marks a title that's already enriched and only needs a
	// watch-provider refresh for a new region.
	ProvidersOnly bool
	MediaType     string // valid when ProvidersOnly
	TMDbID        int    // valid when ProvidersOnly
}

// Result is the outcome of one Job.
type Result struct {
	ID          int
	Done, Total int // 1-based completion count / total, for progress bars

	Err           error       // a lookup transport error; title stays unenriched
	Found         bool        // false: TMDb had no match for a fresh lookup
	ProvidersOnly bool        // mirrors Job.ProvidersOnly
	Lookup        tmdb.Result // populated when a fresh lookup succeeded

	Providers    []string // nil when ProvidersErr != nil or nothing found
	ProvidersErr error    // non-nil: leave the title's cached providers untouched
}

// Jobs builds the work list from a pool: every unenriched title, plus any
// already-enriched title whose cached watch-providers are stale for region.
func Jobs(d *store.Data, region string) []Job {
	var jobs []Job
	for i := range d.Titles {
		t := &d.Titles[i]
		switch {
		case !t.Enriched:
			jobs = append(jobs, Job{ID: i, Name: t.Name, Year: t.Year})
		case t.ProvidersRegion != region:
			jobs = append(jobs, Job{ID: i, ProvidersOnly: true, MediaType: t.MediaType, TMDbID: t.TMDbID})
		}
	}
	return jobs
}

// Run enriches jobs concurrently against TMDb and streams a Result per job
// on the returned channel, in completion order (not job order). The channel
// closes once every job has reported. Run never mutates the pool — callers
// apply each Result themselves.
func Run(jobs []Job, c *tmdb.Client, region string) <-chan Result {
	out := make(chan Result)
	total := len(jobs)
	if total == 0 {
		close(out)
		return out
	}

	cache := loadCache()
	tokens, stop := newLimiter(politeDelay)

	jobCh := make(chan Job)
	go func() {
		for _, j := range jobs {
			jobCh <- j
		}
		close(jobCh)
	}()

	workers := MaxWorkers
	if workers > total {
		workers = total
	}
	var wg sync.WaitGroup
	var completed atomic.Int32
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for j := range jobCh {
				r := runJob(j, c, region, cache, tokens)
				r.Total = total
				r.Done = int(completed.Add(1))
				out <- r
			}
		}()
	}

	go func() {
		wg.Wait()
		stop()
		if cache.dirty {
			_ = cache.save()
		}
		close(out)
	}()

	return out
}

// runJob performs one job's TMDb work, consulting the identity cache before
// spending a rate-limit token on a fresh lookup.
func runJob(j Job, c *tmdb.Client, region string, cache *Cache, tokens <-chan struct{}) Result {
	if j.ProvidersOnly {
		<-tokens
		provs, err := c.WatchProviders(j.MediaType, j.TMDbID, region)
		return Result{ID: j.ID, ProvidersOnly: true, Found: true, Providers: provs, ProvidersErr: err}
	}

	var res tmdb.Result
	var found bool
	if e, hit := cache.get(j.Name, j.Year); hit {
		res, found = e.Result, e.Found
	} else {
		<-tokens
		var err error
		res, found, err = c.Lookup(j.Name, j.Year)
		if err != nil {
			return Result{ID: j.ID, Err: err}
		}
		cache.put(j.Name, j.Year, cacheEntry{Result: res, Found: found})
	}
	if !found {
		return Result{ID: j.ID, Found: false}
	}

	<-tokens
	provs, provErr := c.WatchProviders(res.MediaType, res.TMDbID, region)
	return Result{ID: j.ID, Found: true, Lookup: res, Providers: provs, ProvidersErr: provErr}
}

// CountUnenriched is a convenience for the UI's summary lines.
func CountUnenriched(d *store.Data) int {
	return len(d.Unenriched())
}

// CountPending is like CountUnenriched but also counts titles whose provider
// cache needs refreshing for region.
func CountPending(d *store.Data, region string) int {
	return len(d.Unenriched()) + len(d.NeedsProviders(region))
}
