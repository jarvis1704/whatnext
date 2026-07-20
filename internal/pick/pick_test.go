package pick

import (
	"testing"

	"whatnext/internal/store"
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
