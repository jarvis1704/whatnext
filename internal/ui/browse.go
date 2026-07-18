package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"whatnext/internal/store"
)

type browseState struct {
	rows        []int // indices into data.Titles
	cursor      int
	offset      int
	watchedOnly bool
}

func newBrowseState(d *store.Data, watchedOnly bool) browseState {
	b := browseState{watchedOnly: watchedOnly}
	b.rebuild(d)
	return b
}

func (b *browseState) rebuild(d *store.Data) {
	b.rows = b.rows[:0]
	for i, t := range d.Titles {
		if b.watchedOnly && !t.Watched {
			continue
		}
		b.rows = append(b.rows, i)
	}
	if b.cursor >= len(b.rows) {
		b.cursor = max(0, len(b.rows)-1)
	}
}

func (m model) updateBrowse(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	b := &m.browse
	switch msg.String() {
	case "esc", "backspace", "h", "left":
		m.goTo(scrHome)
		return m, nil
	case "up", "k":
		if b.cursor > 0 {
			b.cursor--
		}
	case "down", "j":
		if b.cursor < len(b.rows)-1 {
			b.cursor++
		}
	case "g", "home":
		b.cursor = 0
	case "G", "end":
		b.cursor = max(0, len(b.rows)-1)
	case "tab":
		b.watchedOnly = !b.watchedOnly
		b.cursor = 0
		b.offset = 0
		b.rebuild(m.data)
	case "w", "enter", " ":
		if len(b.rows) > 0 {
			idx := b.rows[b.cursor]
			m.data.Titles[idx].Watched = !m.data.Titles[idx].Watched
			m.dirty = true
			_ = m.data.Save()
			m.dirty = false
			b.rebuild(m.data)
		}
	}
	return m, nil
}

func (m model) viewBrowse() string {
	b := m.browse
	title := gradientText("Your watchlist", float64(m.frame)/40.0, true)

	scope := "all titles"
	if b.watchedOnly {
		scope = "watched only"
	}
	sub := styleSubtle.Render(fmt.Sprintf("%d shown · %s", len(b.rows), scope))

	if len(b.rows) == 0 {
		empty := stylePanel.Render(styleSubtle.Render("Nothing here yet. Import a Letterboxd export to fill your pool."))
		foot := m.footer([2]string{"tab", "toggle watched"}, [2]string{"esc", "back"})
		return lipgloss.JoinVertical(lipgloss.Left, title, sub, "", empty, "", foot)
	}

	// Visible window sized to the terminal height. Offset derives purely from
	// the cursor so scrolling needs no persisted state.
	viewport := m.browseViewport()
	offset := 0
	if b.cursor >= viewport {
		offset = b.cursor - viewport + 1
	}

	var lines []string
	end := min(offset+viewport, len(b.rows))
	for i := offset; i < end; i++ {
		lines = append(lines, m.browseRow(b.rows[i], i == b.cursor))
	}
	list := lipgloss.JoinVertical(lipgloss.Left, lines...)

	// Scroll indicator.
	pos := fmt.Sprintf("%d/%d", b.cursor+1, len(b.rows))
	scroll := styleSubtle.Render(pos)

	foot := m.footer(
		[2]string{"↑↓", "move"},
		[2]string{"w/↵", "toggle watched"},
		[2]string{"tab", scope2(b.watchedOnly)},
		[2]string{"esc", "back"},
	)
	return lipgloss.JoinVertical(lipgloss.Left, title, sub, "", list, "", scroll, foot)
}

func scope2(watchedOnly bool) string {
	if watchedOnly {
		return "show all"
	}
	return "watched only"
}

func (m model) browseViewport() int {
	// leave room for title, sub, spacing, scroll, footer (~8 lines) + app pad.
	v := m.height - 10
	if v < 3 {
		v = 3
	}
	if v > 20 {
		v = 20
	}
	return v
}

func (m model) browseRow(idx int, selected bool) string {
	t := m.data.Titles[idx]

	// status glyph
	status := styleSubtle.Render("○")
	if t.Watched {
		status = styleGood.Render("✓")
	} else if !t.Enriched {
		status = styleWarn.Render("…")
	}

	name := t.Name
	if t.Year != "" {
		name += " (" + t.Year + ")"
	}
	name = truncate(name, 40)

	var meta string
	if t.Enriched && len(t.Genres) > 0 {
		meta = truncate(strings.Join(t.Genres, ", "), 30)
		if t.RuntimeMins > 0 {
			meta += fmt.Sprintf(" · %dm", t.RuntimeMins)
		}
	} else if !t.Enriched {
		meta = "unenriched"
	}

	nameStyle := lipgloss.NewStyle().Foreground(cFg)
	metaStyle := lipgloss.NewStyle().Foreground(cSubtle)
	marker := "  "
	if selected {
		markCol := lipgloss.Color(gradientAt(float64(m.frame) / 30.0).Clamped().Hex())
		marker = lipgloss.NewStyle().Foreground(markCol).Bold(true).Render("▌ ")
		nameStyle = nameStyle.Bold(true).Foreground(cGold)
	}
	line := fmt.Sprintf("%s%s %s   %s",
		marker,
		status,
		nameStyle.Width(42).Render(name),
		metaStyle.Render(meta),
	)
	return line
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
