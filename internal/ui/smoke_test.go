package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"whatnext/internal/pick"
	"whatnext/internal/store"
)

// newTestModel builds a model with a small in-memory pool, bypassing disk.
// HOME is redirected to a temp dir so tests never touch the real ~/.whatnext.
func newTestModel(t *testing.T) model {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	m, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	m.data = &store.Data{Titles: []store.Title{
		{Name: "Oldboy", Year: "2003", LetterboxdURI: "a", Enriched: true, MediaType: "movie", Genres: []string{"Thriller"}, RuntimeMins: 120},
		{Name: "Paddington 2", Year: "2017", LetterboxdURI: "b", Enriched: true, MediaType: "movie", Genres: []string{"Comedy", "Family"}, RuntimeMins: 104},
		{Name: "Parasite", Year: "2019", LetterboxdURI: "c"},
	}}
	m.width, m.height = 90, 30
	return m
}

// TestScreensRender walks every screen and asserts View produces output and
// never panics.
func TestScreensRender(t *testing.T) {
	m := newTestModel(t)
	m.filter = newFilterState(m.moods)

	screens := []screen{scrHome, scrFilters, scrReveal, scrBrowse, scrImport, scrProgress, scrStats}
	for _, s := range screens {
		m.screen = s
		if s == scrBrowse {
			m.browse = newBrowseState(m.data, false)
		}
		if s == scrReveal {
			m.reveal = revealState{phase: revEmpty}
		}
		out := m.View()
		if strings.TrimSpace(out) == "" {
			t.Errorf("screen %d rendered empty", s)
		}
	}
}

// TestRevealLands drives the slot machine to completion and checks a result
// locks in.
func TestRevealLands(t *testing.T) {
	m := newTestModel(t)
	tm, _ := m.startReveal()
	m = tm.(model)
	if m.reveal.phase != revShuffling {
		t.Fatalf("expected shuffling, got %d", m.reveal.phase)
	}
	// Run enough ticks to exhaust the spins.
	for i := 0; i < totalSpins+2 && m.reveal.phase == revShuffling; i++ {
		tm, _ = m.updateRevealTick()
		m = tm.(model)
	}
	if m.reveal.phase != revLanded {
		t.Fatalf("reveal never landed, phase=%d", m.reveal.phase)
	}
	if m.reveal.result.Name == "" {
		t.Fatal("landed with empty result")
	}
}

// TestBrowseToggleWatched marks a title watched via the browse handler.
func TestBrowseToggleWatched(t *testing.T) {
	m := newTestModel(t)
	m.screen = scrBrowse
	m.browse = newBrowseState(m.data, false)
	tm, _ := m.updateBrowse(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("w")})
	m = tm.(model)
	if !m.data.Titles[0].Watched {
		t.Error("expected first title marked watched")
	}
}

// TestFilterNarrows checks the mood filter shrinks the candidate set.
func TestFilterNarrows(t *testing.T) {
	m := newTestModel(t)
	all := len(m.data.Titles) - 0 // 3 titles, none watched
	m.filter.moodIdx = indexOf(m.filter.moodNames, "cozy")
	f := m.buildFilter()
	if len(f.MoodGenres) == 0 {
		t.Skip("cozy mood not present in defaults")
	}
	cands := len(pick.Candidates(m.data.Titles, f))
	if cands >= all {
		t.Errorf("mood filter did not narrow: %d of %d", cands, all)
	}
}

func indexOf(ss []string, want string) int {
	for i, s := range ss {
		if s == want {
			return i
		}
	}
	return -1
}
