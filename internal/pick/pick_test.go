package pick

import (
	"testing"
	"time"

	"github.com/jarvis1704/whatnext/internal/store"
)

func TestCandidatesProviderFilter(t *testing.T) {
	titles := []store.Title{
		{Name: "A", Enriched: true, Providers: []string{"Netflix", "Max"}},
		{Name: "B", Enriched: true, Providers: []string{"Prime Video"}},
		{Name: "C", Enriched: true}, // no providers
		{Name: "D", Watched: true, Enriched: true, Providers: []string{"Netflix"}},
	}

	got := Candidates(titles, Filter{Provider: "netflix"})
	if len(got) != 1 || got[0].Name != "A" {
		t.Fatalf("Candidates(Provider=netflix) = %+v, want just A (case-insensitive, unwatched only)", got)
	}
}

func TestWeightedEmpty(t *testing.T) {
	if _, ok := Weighted(nil); ok {
		t.Fatal("Weighted(nil) should report ok=false")
	}
}

func TestWeightedFavorsOlder(t *testing.T) {
	now := time.Now()
	titles := []store.Title{
		{Name: "Old", AddedAt: now.AddDate(0, 0, -365)},
		{Name: "New", AddedAt: now},
	}

	oldWins := 0
	const trials = 2000
	for i := 0; i < trials; i++ {
		got, ok := Weighted(titles)
		if !ok {
			t.Fatal("Weighted(titles) ok=false, want true")
		}
		if got.Name == "Old" {
			oldWins++
		}
	}
	// Old is ~366x heavier than New, so it should win the overwhelming
	// majority of trials; a loose >80% bound keeps this test non-flaky.
	if oldWins < trials*8/10 {
		t.Fatalf("Weighted favored the older title in %d/%d trials, want a strong majority", oldWins, trials)
	}
}
