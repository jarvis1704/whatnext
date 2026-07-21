// Package pick holds the pure filtering + random-selection logic, decoupled
// from I/O so it stays trivially testable.
package pick

import (
	"math/rand"
	"strings"
	"time"

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
	MinRating  float64  // minimum TMDb vote average (0-10); 0 = no floor
}

// Active reports whether any constraint is set.
func (f Filter) Active() bool {
	return f.Genre != "" || len(f.MoodGenres) > 0 || f.MediaType != "" || f.MaxRuntime > 0 || f.Provider != "" || f.MinRating > 0
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
	if f.MinRating > 0 && t.VoteAverage < f.MinRating {
		return false
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

// Weighted returns a random title from candidates, weighted by days since
// AddedAt so titles that have sat unwatched the longest are proportionally
// more likely to be picked. Titles without a recorded AddedAt (pool entries
// from before this field existed) weigh as maximally old. ok is false when
// the slice is empty.
func Weighted(candidates []store.Title) (store.Title, bool) {
	if len(candidates) == 0 {
		return store.Title{}, false
	}
	now := time.Now()
	weights := make([]float64, len(candidates))
	total := 0.0
	for i, t := range candidates {
		age := 1.0 // +1 so a same-day add still has a nonzero chance
		if !t.AddedAt.IsZero() {
			age += now.Sub(t.AddedAt).Hours() / 24
		} else {
			age += 365 * 10 // untracked entries: treat as a decade old
		}
		weights[i] = age
		total += age
	}
	r := rand.Float64() * total
	for i, w := range weights {
		r -= w
		if r <= 0 {
			return candidates[i], true
		}
	}
	return candidates[len(candidates)-1], true
}

// DoubleFeature picks two distinct candidates whose combined runtime fits
// within budget minutes (budget <= 0 means no cap — just any random pair).
// Titles without a known runtime are excluded, since there's nothing to sum.
// ok is false when fewer than two runtime-known candidates exist, or no
// pair fits the budget.
func DoubleFeature(candidates []store.Title, budget int) (a, b store.Title, ok bool) {
	timed := make([]store.Title, 0, len(candidates))
	for _, t := range candidates {
		if t.RuntimeMins > 0 {
			timed = append(timed, t)
		}
	}
	if len(timed) < 2 {
		return store.Title{}, store.Title{}, false
	}
	rand.Shuffle(len(timed), func(i, j int) { timed[i], timed[j] = timed[j], timed[i] })
	for i := 0; i < len(timed); i++ {
		for j := i + 1; j < len(timed); j++ {
			if budget <= 0 || timed[i].RuntimeMins+timed[j].RuntimeMins <= budget {
				return timed[i], timed[j], true
			}
		}
	}
	return store.Title{}, store.Title{}, false
}
