package stats

import (
	"testing"

	"github.com/jarvis1704/whatnext/internal/store"
)

func TestComputeEmpty(t *testing.T) {
	s := Compute(nil)
	if s.Total != 0 || s.AvgRuntimeMins != 0 || len(s.Genres) != 0 || len(s.Decades) != 0 {
		t.Fatalf("expected zero-value stats for empty pool, got %+v", s)
	}
}

func TestCompute(t *testing.T) {
	titles := []store.Title{
		{Name: "A", Year: "1994", MediaType: "movie", Genres: []string{"Drama", "Crime"}, RuntimeMins: 142, Enriched: true, Watched: true},
		{Name: "B", Year: "1999", MediaType: "movie", Genres: []string{"Drama"}, RuntimeMins: 118, Enriched: true},
		{Name: "C", Year: "2011", MediaType: "tv", Genres: []string{"Drama", "Fantasy"}, RuntimeMins: 60, Enriched: true},
		{Name: "D", Year: "", MediaType: "movie", Enriched: false}, // unenriched, unknown year/runtime
	}

	s := Compute(titles)

	if s.Total != 4 {
		t.Errorf("Total = %d, want 4", s.Total)
	}
	if s.Watched != 1 || s.Unwatched != 3 {
		t.Errorf("Watched/Unwatched = %d/%d, want 1/3", s.Watched, s.Unwatched)
	}
	if s.Unenriched != 1 {
		t.Errorf("Unenriched = %d, want 1", s.Unenriched)
	}
	if s.MovieCount != 3 || s.TVCount != 1 {
		t.Errorf("MovieCount/TVCount = %d/%d, want 3/1", s.MovieCount, s.TVCount)
	}

	wantAvg := (142 + 118 + 60) / 3
	if s.AvgRuntimeMins != wantAvg || s.RuntimeSamples != 3 {
		t.Errorf("AvgRuntimeMins/RuntimeSamples = %d/%d, want %d/3", s.AvgRuntimeMins, s.RuntimeSamples, wantAvg)
	}

	if len(s.Genres) != 3 {
		t.Fatalf("Genres = %+v, want 3 entries", s.Genres)
	}
	if s.Genres[0].Genre != "Drama" || s.Genres[0].Count != 3 {
		t.Errorf("top genre = %+v, want Drama:3", s.Genres[0])
	}

	if len(s.Decades) != 2 {
		t.Fatalf("Decades = %+v, want 2 entries", s.Decades)
	}
	wantDecades := []DecadeCount{{1990, 2}, {2010, 1}}
	if s.Decades[0] != wantDecades[0] {
		t.Errorf("Decades[0] = %+v, want %+v", s.Decades[0], wantDecades[0])
	}
}

func TestParseYear(t *testing.T) {
	cases := map[string]bool{
		"1994":  true,
		"":      false,
		"abcd":  false,
		"94":    false,
		"31999": false,
	}
	for in, want := range cases {
		_, ok := parseYear(in)
		if ok != want {
			t.Errorf("parseYear(%q) ok = %v, want %v", in, ok, want)
		}
	}
}
