// Package pick holds the pure filtering + random-selection logic, decoupled
// from I/O so it stays trivially testable.
package pick

import (
	"math/rand"
	"strings"

	"github.com/jarvis1704/whatnext/internal/store"
)

// Filter describes the constraints a picked title must satisfy. Zero values
// mean "no constraint".
type Filter struct {
	Genre      string   // single genre name (case-insensitive)
	MoodGenres []string // any-of genre set expanded from a mood
	MediaType  string   // "movie" | "tv"
	MaxRuntime int      // minutes; 0 = no cap
	Provider   string   // streaming provider name (case-insensitive), e.g. "Netflix"
}

// Active reports whether any constraint is set.
func (f Filter) Active() bool {
	return f.Genre != "" || len(f.MoodGenres) > 0 || f.MediaType != "" || f.MaxRuntime > 0 || f.Provider != ""
}

// Candidates returns every unwatched title matching f. When a genre or mood
// filter is active, unenriched titles (which have no genres) are excluded.
func Candidates(titles []store.Title, f Filter) []store.Title {
	var out []store.Title
	for _, t := range titles {
		if t.Watched {
			continue
		}
		if !matches(t, f) {
			continue
		}
		out = append(out, t)
	}
	return out
}

func matches(t store.Title, f Filter) bool {
	if f.MediaType != "" && !strings.EqualFold(t.MediaType, f.MediaType) {
		return false
	}
	if f.MaxRuntime > 0 {
		// Titles with unknown runtime (0) fail an explicit runtime cap.
		if t.RuntimeMins == 0 || t.RuntimeMins > f.MaxRuntime {
			return false
		}
	}
	if f.Genre != "" {
		if !t.Enriched || !hasGenre(t.Genres, f.Genre) {
			return false
		}
	}
	if len(f.MoodGenres) > 0 {
		if !t.Enriched || !hasAnyGenre(t.Genres, f.MoodGenres) {
			return false
		}
	}
	if f.Provider != "" {
		if !hasGenre(t.Providers, f.Provider) { // same case-insensitive membership check
			return false
		}
	}
	return true
}

func hasGenre(genres []string, want string) bool {
	for _, g := range genres {
		if strings.EqualFold(g, want) {
			return true
		}
	}
	return false
}

func hasAnyGenre(genres, wants []string) bool {
	for _, w := range wants {
		if hasGenre(genres, w) {
			return true
		}
	}
	return false
}

// Random returns a uniformly random title from candidates. ok is false when
// the slice is empty.
func Random(candidates []store.Title) (store.Title, bool) {
	if len(candidates) == 0 {
		return store.Title{}, false
	}
	return candidates[rand.Intn(len(candidates))], true
}
