package pick

import (
	"strings"

	"whatnext/internal/store"
)

// Match couples a pool index with its title for disambiguation UIs.
type Match struct {
	Index int
	Title store.Title
}

// MatchByName finds pool entries whose name relates to query. It prefers an
// exact (case-insensitive) name match; if none, it falls back to substring
// containment either direction. Returns every candidate so the caller can ask
// the user to disambiguate when more than one comes back.
func MatchByName(titles []store.Title, query string) []Match {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil
	}

	var exact, fuzzy []Match
	for i, t := range titles {
		name := strings.ToLower(t.Name)
		switch {
		case name == q:
			exact = append(exact, Match{Index: i, Title: t})
		case strings.Contains(name, q) || strings.Contains(q, name):
			fuzzy = append(fuzzy, Match{Index: i, Title: t})
		}
	}
	if len(exact) > 0 {
		return exact
	}
	return fuzzy
}
