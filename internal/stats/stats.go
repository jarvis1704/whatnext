// Package stats computes read-only aggregates over the title pool: genre
// breakdown, decade histogram, average runtime, watched ratio. Pure and
// I/O-free like pick, so it stays trivially testable.
package stats

import (
	"sort"
	"strconv"

	"whatnext/internal/store"
)

// GenreCount is a single genre's occurrence count across the pool.
type GenreCount struct {
	Genre string
	Count int
}

// DecadeCount is the number of titles released in a given decade (e.g. 1990
// means the 1990s).
type DecadeCount struct {
	Decade int
	Count  int
}

// Stats is the full set of pool aggregates.
type Stats struct {
	Total      int
	Watched    int
	Unwatched  int
	Unenriched int

	MovieCount int
	TVCount    int

	// AvgRuntimeMins is the mean runtime over enriched titles with a known
	// runtime. Zero when no title has one.
	AvgRuntimeMins int
	RuntimeSamples int

	// Genres is sorted by count descending, then name ascending.
	Genres []GenreCount
	// Decades is sorted by decade ascending. Titles with an unparseable or
	// missing year are excluded.
	Decades []DecadeCount
}

// Compute aggregates stats over the given pool.
func Compute(titles []store.Title) Stats {
	var s Stats
	genreCounts := map[string]int{}
	decadeCounts := map[int]int{}
	runtimeSum := 0

	for _, t := range titles {
		s.Total++
		if t.Watched {
			s.Watched++
		} else {
			s.Unwatched++
		}
		if !t.Enriched {
			s.Unenriched++
		}

		switch t.MediaType {
		case "movie":
			s.MovieCount++
		case "tv":
			s.TVCount++
		}

		for _, g := range t.Genres {
			if g == "" {
				continue
			}
			genreCounts[g]++
		}

		if t.RuntimeMins > 0 {
			runtimeSum += t.RuntimeMins
			s.RuntimeSamples++
		}

		if year, ok := parseYear(t.Year); ok {
			decadeCounts[(year/10)*10]++
		}
	}

	if s.RuntimeSamples > 0 {
		s.AvgRuntimeMins = runtimeSum / s.RuntimeSamples
	}

	s.Genres = make([]GenreCount, 0, len(genreCounts))
	for g, c := range genreCounts {
		s.Genres = append(s.Genres, GenreCount{Genre: g, Count: c})
	}
	sort.Slice(s.Genres, func(i, j int) bool {
		if s.Genres[i].Count != s.Genres[j].Count {
			return s.Genres[i].Count > s.Genres[j].Count
		}
		return s.Genres[i].Genre < s.Genres[j].Genre
	})

	s.Decades = make([]DecadeCount, 0, len(decadeCounts))
	for d, c := range decadeCounts {
		s.Decades = append(s.Decades, DecadeCount{Decade: d, Count: c})
	}
	sort.Slice(s.Decades, func(i, j int) bool {
		return s.Decades[i].Decade < s.Decades[j].Decade
	})

	return s
}

func parseYear(y string) (int, bool) {
	if len(y) != 4 {
		return 0, false
	}
	n, err := strconv.Atoi(y)
	if err != nil || n < 1800 || n > 2200 {
		return 0, false
	}
	return n, true
}
