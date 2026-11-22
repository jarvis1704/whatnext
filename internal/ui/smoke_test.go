package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jarvis1704/whatnext/internal/pick"
	"github.com/jarvis1704/whatnext/internal/store"
	"github.com/jarvis1704/whatnext/internal/tmdb"
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

	screens := []screen{scrHome, scrFilters, scrReveal, scrBrowse, scrAdd, scrImport, scrProgress, scrStats, scrRegion}
	for _, s := range screens {
		m.screen = s
		if s == scrBrowse {
			m.browse = newBrowseState(m.data, false)
		}
		if s == scrAdd {
			m.add = addState{input: newAddInput()}
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

// TestAddFlow drives the add screen through search results and a completed
// add without touching the network: the async messages are fed in directly.
func TestAddFlow(t *testing.T) {
	m := newTestModel(t)
	tm, _ := m.openAdd()
	m = tm.(model)
	if !m.isTextEntry() {
		t.Fatal("add entry phase should capture q as text")
	}
	m.add.phase = addSearching
	tm, _ = m.afterAddSearch(addSearchMsg{hits: []tmdb.SearchHit{
		{TMDbID: 95396, MediaType: "tv", Name: "Severance", Year: "2022"},
	}})
	m = tm.(model)
	if m.add.phase != addResults || len(m.add.hits) != 1 {
		t.Fatalf("phase=%d hits=%d", m.add.phase, len(m.add.hits))
	}
	if strings.TrimSpace(m.View()) == "" {
		t.Fatal("results view empty")
	}

	m.add.phase = addAdding
	show := store.Title{Name: "Severance", LetterboxdURI: store.TMDbKey("tv", 95396), TMDbID: 95396,
		MediaType: "tv", Enriched: true, Seasons: 2, Episodes: 19, TVStatus: "Returning Series"}
	tm, _ = m.afterAddDone(addDoneMsg{title: show})
	m = tm.(model)
	if len(m.data.Titles) != 4 {
		t.Fatalf("pool size = %d, want 4", len(m.data.Titles))
	}
	// Choosing it again reports it's already there instead of re-fetching.
	tm, cmd := m.updateAddResults(tea.KeyMsg{Type: tea.KeyEnter})
	m = tm.(model)
	if cmd != nil || !strings.Contains(m.add.err, "Already in your pool") {
		t.Errorf("re-add: cmd=%v err=%q", cmd != nil, m.add.err)
	}
}

// TestRevealShowsSeriesLine checks the reveal card surfaces tv commitment.
func TestRevealShowsSeriesLine(t *testing.T) {
	m := newTestModel(t)
	m.reveal = revealState{phase: revLanded, result: store.Title{
		Name: "Shōgun", MediaType: "tv", Enriched: true, RuntimeMins: 58, Seasons: 1, Episodes: 10,
		TVStatus: "Ended", TVType: "Miniseries",
	}}
	m.screen = scrReveal
	out := m.View()
	for _, want := range []string{"58 min/ep", "10 eps", "Miniseries"} {
		if !strings.Contains(out, want) {
			t.Errorf("reveal card missing %q", want)
		}
	}
}
