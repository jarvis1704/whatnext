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

func TestCandidatesMinRating(t *testing.T) {
	titles := []store.Title{
		{Name: "A", Enriched: true, VoteAverage: 8.1},
		{Name: "B", Enriched: true, VoteAverage: 5.0},
		{Name: "C", Enriched: true}, // unrated
	}

	got := Candidates(titles, Filter{MinRating: 7})
	if len(got) != 1 || got[0].Name != "A" {
		t.Fatalf("Candidates(MinRating=7) = %+v, want just A", got)
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

func TestDoubleFeatureRespectsBudget(t *testing.T) {
	titles := []store.Title{
		{Name: "Short", RuntimeMins: 40},
		{Name: "Medium", RuntimeMins: 90},
		{Name: "Long", RuntimeMins: 150},
	}

	// Only Short+Medium (130) fits under 140; Short+Long and Medium+Long don't.
	for i := 0; i < 50; i++ {
		a, b, ok := DoubleFeature(titles, 140)
		if !ok {
			t.Fatal("DoubleFeature ok=false, want a fitting pair")
		}
		names := map[string]bool{a.Name: true, b.Name: true}
		if !names["Short"] || !names["Medium"] || names["Long"] {
			t.Fatalf("DoubleFeature(budget=140) = %s+%s, want Short+Medium", a.Name, b.Name)
		}
	}
}

func TestDoubleFeatureNoBudget(t *testing.T) {
	titles := []store.Title{
		{Name: "Short", RuntimeMins: 40},
		{Name: "Long", RuntimeMins: 500},
	}
	a, b, ok := DoubleFeature(titles, 0)
	if !ok || a.Name == b.Name {
		t.Fatalf("DoubleFeature(budget=0) = %+v, %+v, ok=%v, want any distinct pair", a, b, ok)
	}
}

func TestDoubleFeatureNeedsTwoTimedCandidates(t *testing.T) {
	titles := []store.Title{{Name: "OnlyOne", RuntimeMins: 90}}
	if _, _, ok := DoubleFeature(titles, 0); ok {
		t.Fatal("DoubleFeature with one timed candidate should report ok=false")
	}
}

func TestDoubleFeatureNoFit(t *testing.T) {
	titles := []store.Title{
		{Name: "A", RuntimeMins: 100},
		{Name: "B", RuntimeMins: 100},
	}
	if _, _, ok := DoubleFeature(titles, 50); ok {
		t.Fatal("DoubleFeature should report ok=false when no pair fits the budget")
	}
}
