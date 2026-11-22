package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/jarvis1704/whatnext/internal/enrich"
	"github.com/jarvis1704/whatnext/internal/store"
	"github.com/jarvis1704/whatnext/internal/tmdb"
)

// The add screen: search TMDb by name, pick a result, and add it to the pool
// fully enriched. It's the way tv shows get in, since Letterboxd exports
// rarely carry them.

type addPhase int

const (
	addEntry     addPhase = iota // typing a query
	addSearching                 // search request in flight
	addResults                   // choosing from hits
	addAdding                    // details/providers request in flight
)

var addKinds = []string{"", "tv", "movie"} // "" = both

type addState struct {
	input   textinput.Model
	phase   addPhase
	kindIdx int // index into addKinds
	hits    []tmdb.SearchHit
	idx     int
	err     string
	msg     string // last success line, e.g. "✓ added Severance"
}

type addSearchMsg struct {
	hits []tmdb.SearchHit
	err  error
}

type addDoneMsg struct {
	title store.Title
	err   error
}

func newAddInput() textinput.Model {
	ti := textinput.New()
	ti.Placeholder = "e.g. Severance, Shōgun, Arrival…"
	ti.Prompt = styleAccent.Render("› ")
	ti.CharLimit = 128
	ti.Width = 40
	return ti
}

func (m model) openAdd() (tea.Model, tea.Cmd) {
	m.add = addState{input: newAddInput()}
	m.add.input.Focus()
	m.goTo(scrAdd)
	return m, textinput.Blink
}

func (m model) updateAdd(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.add.phase {
	case addSearching, addAdding:
		if msg.String() == "esc" {
			m.goTo(scrHome)
		}
		return m, nil
	case addResults:
		return m.updateAddResults(msg)
	}

	switch msg.String() {
	case "esc":
		m.goTo(scrHome)
		return m, nil
	case "tab":
		m.add.kindIdx = (m.add.kindIdx + 1) % len(addKinds)
		return m, nil
	case "enter":
		q := strings.TrimSpace(m.add.input.Value())
		if q == "" {
			return m, nil
		}
		if m.tmdb == nil {
			m.add.err = "Adding needs a TMDb API key."
			return m, nil
		}
		m.add.err, m.add.msg = "", ""
		m.add.phase = addSearching
		return m, searchCmd(m.tmdb, q, addKinds[m.add.kindIdx])
	}
	var cmd tea.Cmd
	m.add.input, cmd = m.add.input.Update(msg)
	return m, cmd
}

func (m model) updateAddResults(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	n := len(m.add.hits)
	switch msg.String() {
	case "esc", "/":
		m.add.phase = addEntry
		m.add.input.Focus()
		return m, textinput.Blink
	case "up", "k":
		m.add.idx = (m.add.idx - 1 + n) % n
	case "down", "j":
		m.add.idx = (m.add.idx + 1) % n
	case "enter":
		hit := m.add.hits[m.add.idx]
		if i := m.data.Find(probeTitle(hit)); i >= 0 {
			m.add.err = "Already in your pool: " + m.data.Titles[i].Name
			return m, nil
		}
		m.add.err = ""
		m.add.phase = addAdding
		return m, addTitleCmd(m.tmdb, hit, m.region)
	}
	return m, nil
}

// probeTitle is the identity of a search hit, for the already-in-pool check.
func probeTitle(h tmdb.SearchHit) store.Title {
	return store.Title{TMDbID: h.TMDbID, MediaType: h.MediaType, LetterboxdURI: store.TMDbKey(h.MediaType, h.TMDbID)}
}

func searchCmd(c *tmdb.Client, query, kind string) tea.Cmd {
	return func() tea.Msg {
		hits, err := c.Search(query, kind)
		return addSearchMsg{hits: hits, err: err}
	}
}

func addTitleCmd(c *tmdb.Client, hit tmdb.SearchHit, region string) tea.Cmd {
	return func() tea.Msg {
		t, err := enrich.FromTMDb(c, hit, region)
		return addDoneMsg{title: t, err: err}
	}
}

func (m model) afterAddSearch(msg addSearchMsg) (tea.Model, tea.Cmd) {
	if m.screen != scrAdd || m.add.phase != addSearching {
		return m, nil // user backed out while the request was in flight
	}
	switch {
	case msg.err != nil:
		m.add.phase = addEntry
		m.add.err = shortErr(msg.err)
	case len(msg.hits) == 0:
		m.add.phase = addEntry
		m.add.err = "No TMDb results — try another spelling."
	default:
		m.add.phase = addResults
		m.add.hits = msg.hits
		m.add.idx = 0
		m.add.input.Blur()
	}
	return m, nil
}

func (m model) afterAddDone(msg addDoneMsg) (tea.Model, tea.Cmd) {
	if m.screen != scrAdd || m.add.phase != addAdding {
		return m, nil
	}
	m.add.phase = addResults
	if msg.err != nil {
		m.add.err = shortErr(msg.err)
		return m, nil
	}
	if m.data.Upsert(msg.title) {
		if err := m.data.Save(); err != nil {
			m.add.err = "Couldn't save: " + err.Error()
			return m, nil
		}
	}
	m.add.msg = "✓ added " + msg.title.Name
	return m, nil
}

func (m model) viewAdd() string {
	title := gradientText("Add a movie or show", float64(m.frame)/40.0, true)

	var kindChips []string
	for i, k := range addKinds {
		label := k
		if label == "" {
			label = "both"
		}
		kindChips = append(kindChips, m.chip(label, i == m.add.kindIdx))
	}
	kinds := styleSubtle.Render("search  ") + lipgloss.JoinHorizontal(lipgloss.Top, spaceChips(kindChips)...)

	var body string
	switch m.add.phase {
	case addEntry:
		body = stylePanel.Render(lipgloss.JoinVertical(lipgloss.Left,
			styleSubtle.Render("Title to search on TMDb:"), "", m.add.input.View(), "", kinds))
	case addSearching:
		body = stylePanel.Render(m.spinner.View() + " searching TMDb for " +
			styleAccent.Render(strings.TrimSpace(m.add.input.Value())) + "…")
	default:
		body = stylePanel.Render(m.addResultsList())
	}

	var status string
	switch {
	case m.add.phase == addAdding:
		status = m.spinner.View() + styleSubtle.Render(" fetching details…")
	case m.add.err != "":
		status = styleWarn.Render("⚠ " + m.add.err)
	case m.add.msg != "":
		status = styleGood.Render(m.add.msg)
	}

	var foot string
	switch m.add.phase {
	case addEntry:
		foot = m.footer([2]string{"↵", "search"}, [2]string{"tab", "movie/tv"}, [2]string{"esc", "back"})
	case addResults, addAdding:
		foot = m.footer([2]string{"↑↓", "move"}, [2]string{"↵", "add"}, [2]string{"/", "new search"}, [2]string{"esc", "back"})
	default:
		foot = m.footer([2]string{"esc", "back"})
	}

	blocks := []string{title, "", body, ""}
	if status != "" {
		blocks = append(blocks, status, "")
	}
	blocks = append(blocks, foot)
	return lipgloss.JoinVertical(lipgloss.Left, blocks...)
}

func (m model) addResultsList() string {
	var rows []string
	for i, h := range m.add.hits {
		badge := lipgloss.NewStyle().Foreground(cCyan).Render("movie")
		if h.MediaType == "tv" {
			badge = lipgloss.NewStyle().Foreground(cViolet).Render("tv   ")
		}
		name := truncate(h.Name, 44)
		if h.Year != "" {
			name += " (" + h.Year + ")"
		}
		inPool := m.data.Find(probeTitle(h)) >= 0
		if i == m.add.idx {
			markCol := lipgloss.Color(gradientAt(float64(m.frame) / 30.0).Clamped().Hex())
			mark := lipgloss.NewStyle().Foreground(markCol).Bold(true).Render("▸ ")
			row := mark + badge + "  " + lipgloss.NewStyle().Foreground(cGold).Bold(true).Render(name)
			if inPool {
				row += styleGood.Render("  ✓ in pool")
			}
			if h.Overview != "" {
				row += "\n" + styleSubtle.Render("         "+truncate(h.Overview, 64))
			}
			rows = append(rows, row)
			continue
		}
		row := "  " + badge + "  " + lipgloss.NewStyle().Foreground(cFg).Render(name)
		if inPool {
			row += styleGood.Render("  ✓")
		}
		rows = append(rows, row)
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...) +
		"\n\n" + styleSubtle.Render(fmt.Sprintf("%d results", len(m.add.hits)))
}
