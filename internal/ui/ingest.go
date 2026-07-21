package ui

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/jarvis1704/whatnext/internal/enrich"
	"github.com/jarvis1704/whatnext/internal/letterboxd"
	"github.com/jarvis1704/whatnext/internal/store"
)

// ---- messages ----

// parsedTitlesMsg carries titles parsed from a Letterboxd export.
type parsedTitlesMsg struct {
	titles []store.Title
	err    error
}

// enrichResultMsg carries one concurrent worker's result off m.enrichCh. ok
// is false once the channel closes, meaning every job has reported.
type enrichResultMsg struct {
	res enrich.Result
	ok  bool
}

// waitForEnrichResult reads the next result off ch, turning the channel
// into a stream of tea.Msg — the standard bubbletea pattern for draining a
// channel fed by background goroutines without blocking the Update loop.
func waitForEnrichResult(ch <-chan enrich.Result) tea.Cmd {
	return func() tea.Msg {
		res, ok := <-ch
		return enrichResultMsg{res: res, ok: ok}
	}
}

// ---- progress state ----

type progressState struct {
	mode     string // "import" | "refresh"
	cursor   int
	total    int
	found    int
	notFound int
	errs     int
	added    int // titles newly imported (import mode)
	log      []string
	done     bool
	noKey    bool
}

// ---- import path input ----

func (m model) updateImport(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.goTo(scrHome)
		return m, nil
	case "enter":
		path := expandHome(strings.TrimSpace(m.pathInput.Value()))
		if path == "" {
			m.importErr = "Enter a path to a .zip or watchlist.csv"
			return m, nil
		}
		m.importErr = ""
		return m, parseCmd(path)
	}
	var cmd tea.Cmd
	m.pathInput, cmd = m.pathInput.Update(msg)
	return m, cmd
}

func parseCmd(path string) tea.Cmd {
	return func() tea.Msg {
		titles, err := letterboxd.ParseWatchlist(path)
		return parsedTitlesMsg{titles: titles, err: err}
	}
}

// afterParse merges parsed titles into the pool and launches enrichment.
func (m model) afterParse(msg parsedTitlesMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.importErr = msg.err.Error()
		return m, nil
	}
	added := 0
	for _, t := range msg.titles {
		if m.data.Upsert(t) {
			added++
		}
	}
	m.dirty = true
	_ = m.data.Save()
	m.dirty = false

	m.prog = progressState{mode: "import", added: added}
	m.goTo(scrProgress)
	return m.beginEnrich()
}

func expandHome(p string) string {
	if p == "~" {
		if h, err := os.UserHomeDir(); err == nil {
			return h
		}
	}
	if strings.HasPrefix(p, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			return h + p[1:]
		}
	}
	return p
}

func (m model) viewImport() string {
	title := gradientText("Import a Letterboxd export", float64(m.frame)/40.0, true)
	help := styleSubtle.Render("Letterboxd → Settings → Import & Export → Export Data")
	prompt := stylePanel.Render(lipgloss.JoinVertical(lipgloss.Left,
		styleSubtle.Render("Path to .zip or watchlist.csv:"),
		"",
		m.pathInput.View(),
	))
	var errLine string
	if m.importErr != "" {
		errLine = styleBad.Render("✗ " + m.importErr)
	}
	foot := m.footer([2]string{"↵", "import"}, [2]string{"esc", "back"})
	blocks := []string{title, help, "", prompt, ""}
	if errLine != "" {
		blocks = append(blocks, errLine, "")
	}
	blocks = append(blocks, foot)
	return lipgloss.JoinVertical(lipgloss.Left, blocks...)
}

// ---- refresh + enrichment stepping ----

func (m model) startRefresh() (tea.Model, tea.Cmd) {
	m.prog = progressState{mode: "refresh"}
	m.goTo(scrProgress)
	return m.beginEnrich()
}

// beginEnrich builds the pending-work list — unenriched titles, plus any
// already-enriched title whose cached watch providers are stale for the
// current region — and kicks off a concurrent enrichment run.
func (m model) beginEnrich() (tea.Model, tea.Cmd) {
	jobs := enrich.Jobs(m.data, m.region)
	m.prog.total = len(jobs)
	m.prog.cursor = 0

	if m.tmdb == nil {
		m.prog.noKey = true
		m.prog.done = true
		return m, nil
	}
	if m.prog.total == 0 {
		m.prog.done = true
		return m, nil
	}
	m.enrichCh = enrich.Run(jobs, m.tmdb, m.region)
	return m, tea.Batch(waitForEnrichResult(m.enrichCh), m.bar.SetPercent(0))
}

// updateEnrichResult applies one concurrent worker's result to the pool and
// re-arms the channel read for the next one, until the channel closes.
func (m model) updateEnrichResult(msg enrichResultMsg) (tea.Model, tea.Cmd) {
	if !msg.ok {
		m.prog.done = true
		return m, nil
	}
	r := msg.res
	t := &m.data.Titles[r.ID]
	switch {
	case r.Err != nil:
		m.prog.errs++
		m.pushLog(styleBad.Render("✗ ") + t.Name + styleSubtle.Render("  "+shortErr(r.Err)))
	case r.Found && !r.ProvidersOnly:
		t.TMDbID = r.Lookup.TMDbID
		t.MediaType = r.Lookup.MediaType
		t.Genres = r.Lookup.Genres
		t.RuntimeMins = r.Lookup.RuntimeMins
		t.VoteAverage = r.Lookup.VoteAverage
		t.Popularity = r.Lookup.Popularity
		t.Enriched = true
		if r.ProvidersErr == nil {
			t.Providers = r.Providers
			t.ProvidersRegion = m.region
		}
		m.prog.found++
		m.pushLog(styleGood.Render("✓ ") + t.Name + styleSubtle.Render("  "+strings.Join(t.Genres, ", ")))
	case r.Found: // already enriched — this was purely a provider-region refresh
		if r.ProvidersErr == nil {
			t.Providers = r.Providers
			t.ProvidersRegion = m.region
		}
		m.prog.found++
		where := "no streaming providers found"
		if len(r.Providers) > 0 {
			where = strings.Join(r.Providers, ", ")
		}
		m.pushLog(styleGood.Render("▸ ") + t.Name + styleSubtle.Render("  "+where))
	default:
		m.prog.notFound++
		m.pushLog(styleWarn.Render("? ") + t.Name + styleSubtle.Render("  no match"))
	}

	m.prog.cursor = r.Done
	frac := float64(m.prog.cursor) / float64(max(m.prog.total, 1))
	barCmd := m.bar.SetPercent(frac)

	// Persist incrementally so a mid-run quit keeps what we've enriched.
	_ = m.data.Save()

	return m, tea.Batch(barCmd, waitForEnrichResult(m.enrichCh))
}

func (m model) updateProgress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if !m.prog.done {
		return m, nil
	}
	switch msg.String() {
	case "enter", "esc", "backspace":
		m.flash = m.progressSummary()
		m.goTo(scrHome)
	}
	return m, nil
}

func (m *model) pushLog(line string) {
	m.prog.log = append(m.prog.log, line)
	if len(m.prog.log) > 6 {
		m.prog.log = m.prog.log[len(m.prog.log)-6:]
	}
}

func shortErr(err error) string {
	s := err.Error()
	if len(s) > 40 {
		s = s[:39] + "…"
	}
	return s
}

func (m model) progressSummary() string {
	if m.prog.noKey {
		if m.prog.added > 0 {
			return fmt.Sprintf("Imported %d — set TMDB_API_KEY then Refresh to enrich.", m.prog.added)
		}
		return "Set TMDB_API_KEY, then Refresh to enrich."
	}
	base := ""
	if m.prog.mode == "import" {
		base = fmt.Sprintf("Imported %d · ", m.prog.added)
	}
	if m.prog.total == 0 {
		return base + "everything already enriched."
	}
	return base + fmt.Sprintf("enriched %d · %d not found · %d errors", m.prog.found, m.prog.notFound, m.prog.errs)
}

func (m model) viewProgress() string {
	title := gradientText(caser(m.prog.mode)+" · enriching", float64(m.frame)/40.0, true)

	if m.prog.noKey {
		lines := []string{}
		if m.prog.added > 0 {
			lines = append(lines, styleGood.Render(fmt.Sprintf("✓ Imported %d new titles.", m.prog.added)), "")
		}
		lines = append(lines,
			styleWarn.Render("⚠ TMDB_API_KEY is not set."),
			"",
			styleSubtle.Render("Enrichment needs a free TMDb v3 key:"),
			styleSubtle.Render("  export TMDB_API_KEY=\"your-key\""),
			styleSubtle.Render("Then run Refresh again."),
		)
		box := stylePanel.BorderForeground(cAmber).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
		foot := m.footer([2]string{"↵/esc", "back"})
		return lipgloss.JoinVertical(lipgloss.Left, title, "", box, "", foot)
	}

	if m.prog.total == 0 {
		msg := "✓ Everything is already enriched."
		if m.prog.mode == "import" {
			msg = fmt.Sprintf("✓ Imported %d new titles — all enriched.", m.prog.added)
		}
		box := stylePanel.Render(styleGood.Render(msg))
		foot := m.footer([2]string{"↵/esc", "back"})
		return lipgloss.JoinVertical(lipgloss.Left, title, "", box, "", foot)
	}

	var head string
	if m.prog.done {
		head = styleGood.Render("✓ done")
	} else {
		label := fmt.Sprintf("enriching up to %d titles concurrently…", min(enrich.MaxWorkers, m.prog.total))
		head = m.spinner.View() + " " + lipgloss.NewStyle().Foreground(cFg).Render(label)
	}

	countLine := fmt.Sprintf("%s/%s   %s  %s  %s",
		styleAccent.Render(fmt.Sprintf("%d", m.prog.cursor)),
		styleSubtle.Render(fmt.Sprintf("%d", m.prog.total)),
		styleGood.Render(fmt.Sprintf("✓%d", m.prog.found)),
		styleWarn.Render(fmt.Sprintf("?%d", m.prog.notFound)),
		styleBad.Render(fmt.Sprintf("✗%d", m.prog.errs)),
	)

	bar := m.bar.View()
	var logBox string
	if len(m.prog.log) > 0 {
		logBox = stylePanel.BorderForeground(cFaint).Render(
			lipgloss.JoinVertical(lipgloss.Left, m.prog.log...))
	}

	var foot string
	if m.prog.done {
		foot = m.footer([2]string{"↵/esc", "back to menu"})
	} else {
		foot = styleSubtle.Render("enriching… you can leave it running")
	}

	blocks := []string{title, "", head, "", bar, countLine, ""}
	if logBox != "" {
		blocks = append(blocks, logBox, "")
	}
	blocks = append(blocks, foot)
	return lipgloss.JoinVertical(lipgloss.Left, blocks...)
}

func caser(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
