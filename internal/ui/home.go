package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type homeItem struct {
	icon  string
	label string
	desc  string
	to    screen
}

func (m model) homeItems() []homeItem {
	return []homeItem{
		{"🎲", "Pick something", "Random pick, optionally by mood or filters", scrFilters},
		{"🗂", "Browse pool", "Scroll your watchlist, mark titles watched", scrBrowse},
		{"📥", "Import export", "Load a Letterboxd .zip or watchlist.csv", scrImport},
		{"✨", "Refresh enrichment", "Retry TMDb lookups for unenriched titles", scrProgress},
	}
}

func (m model) updateHome(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	items := m.homeItems()
	switch msg.String() {
	case "up", "k":
		m.homeIdx = (m.homeIdx - 1 + len(items)) % len(items)
	case "down", "j":
		m.homeIdx = (m.homeIdx + 1) % len(items)
	case "enter", "l", "right", " ":
		return m.chooseHome(items[m.homeIdx])
	}
	return m, nil
}

func (m model) chooseHome(it homeItem) (tea.Model, tea.Cmd) {
	switch it.to {
	case scrFilters:
		m.filterIdx = 0
		m.goTo(scrFilters)
	case scrBrowse:
		m.browse = newBrowseState(m.data, false)
		m.goTo(scrBrowse)
	case scrImport:
		m.pathInput.Reset()
		m.pathInput.Focus()
		m.importErr = ""
		m.goTo(scrImport)
	case scrProgress:
		return m.startRefresh()
	}
	return m, nil
}

func (m model) viewHome() string {
	phase := float64(m.frame) / 40.0
	banner := renderBanner(phase)

	// Pool summary line.
	total := len(m.data.Titles)
	unwatched, unenriched := 0, 0
	for _, t := range m.data.Titles {
		if !t.Watched {
			unwatched++
		}
		if !t.Enriched {
			unenriched++
		}
	}
	summary := fmt.Sprintf("%s in pool · %s unwatched",
		styleAccent.Render(fmt.Sprintf("%d", total)),
		styleAccent.Render(fmt.Sprintf("%d", unwatched)),
	)
	if unenriched > 0 {
		summary += styleSubtle.Render("  ·  ") + styleWarn.Render(fmt.Sprintf("%d need enrichment", unenriched))
	}

	// Menu items with an animated selection marker.
	items := m.homeItems()
	var rows []string
	for i, it := range items {
		selected := i == m.homeIdx
		rows = append(rows, m.homeRow(it, selected))
	}
	menu := lipgloss.JoinVertical(lipgloss.Left, rows...)

	// Key / status footer.
	var status string
	if m.tmdbErr != nil {
		status = styleWarn.Render("⚠ TMDB_API_KEY not set — import & pick work; enrichment is disabled.")
	} else if m.flash != "" {
		status = styleGood.Render(m.flash)
	}

	foot := m.footer(
		[2]string{"↑↓", "move"},
		[2]string{"↵", "select"},
		[2]string{"q", "quit"},
	)

	blocks := []string{banner, "", summary, "", menu, ""}
	if status != "" {
		blocks = append(blocks, status, "")
	}
	blocks = append(blocks, foot)
	return lipgloss.JoinVertical(lipgloss.Left, blocks...)
}

func (m model) homeRow(it homeItem, selected bool) string {
	if selected {
		// Breathing gradient marker + bright label.
		p := pulse(m.frame, 22)
		markCol := lipgloss.Color(gradientAt(float64(m.frame) / 30.0).Clamped().Hex())
		mark := lipgloss.NewStyle().Foreground(markCol).Bold(true).Render("▌")
		label := lipgloss.NewStyle().Foreground(cFg).Bold(true).Render(it.icon + "  " + it.label)
		descCol := dimColor(hex("#E5E7EB"), 0.35+0.25*(1-p))
		desc := lipgloss.NewStyle().Foreground(descCol).Render("   " + it.desc)
		return mark + " " + label + "\n" + desc
	}
	label := lipgloss.NewStyle().Foreground(cSubtle).Render("  " + it.icon + "  " + it.label)
	return label
}
