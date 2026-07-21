package enrich

import (
	"testing"

	"github.com/jarvis1704/whatnext/internal/store"
	"github.com/jarvis1704/whatnext/internal/tmdb"
)

func TestJobsSplitsLookupAndProvidersOnly(t *testing.T) {
	d := &store.Data{Titles: []store.Title{
		{Name: "Unenriched"},
		{Name: "Stale region", Enriched: true, MediaType: "movie", TMDbID: 42, ProvidersRegion: "GB"},
		{Name: "Up to date", Enriched: true, MediaType: "movie", TMDbID: 7, ProvidersRegion: "US"},
	}}

	jobs := Jobs(d, "US")
	if len(jobs) != 2 {
		t.Fatalf("Jobs() = %d jobs, want 2 (unenriched + stale region), got %+v", len(jobs), jobs)
	}
	if jobs[0].ID != 0 || jobs[0].ProvidersOnly {
		t.Errorf("jobs[0] = %+v, want a full lookup job for pool index 0", jobs[0])
	}
	if jobs[1].ID != 1 || !jobs[1].ProvidersOnly || jobs[1].TMDbID != 42 {
		t.Errorf("jobs[1] = %+v, want a providers-only job for pool index 1", jobs[1])
	}
}

func TestRunEmptyJobsClosesImmediately(t *testing.T) {
	ch := Run(nil, nil, "US")
	if _, ok := <-ch; ok {
		t.Fatal("Run(nil jobs) should yield a closed, empty channel")
	}
}

func TestCacheRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	c := loadCache()
	if _, hit := c.get("Se7en", "1995"); hit {
		t.Fatal("fresh cache should have no entries")
	}

	entry := cacheEntry{Result: tmdb.Result{TMDbID: 807, MediaType: "movie", VoteAverage: 8.4}, Found: true}
	c.put("Se7en", "1995", entry)
	if !c.dirty {
		t.Fatal("put should mark the cache dirty")
	}
	if err := c.save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	c2 := loadCache()
	got, hit := c2.get("se7en", "1995") // key lookup is case-insensitive
	if !hit {
		t.Fatal("expected a cache hit after reload")
	}
	if got.Result.TMDbID != 807 || got.Result.VoteAverage != 8.4 {
		t.Errorf("got %+v, want the saved entry", got)
	}
}
