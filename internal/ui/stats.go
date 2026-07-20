package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/jarvis1704/whatnext/internal/stats"
)

func (m model) updateStats(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "backspace", "h", "left", "enter":
		m.goTo(scrHome)
	}
	return m, nil
}

func (m model) viewStats() string {
	title := gradientText("Your pool, by the numbers", float64(m.frame)/40.0, true)
	foot := m.footer([2]string{"esc", "back"})

	s := stats.Compute(m.data.Titles)
	if s.Total == 0 {
		empty := stylePanel.Render(styleSubtle.Render("Nothing here yet. Import a Letterboxd export to fill your pool."))
		return lipgloss.JoinVertical(lipgloss.Left, title, "", empty, "", foot)
	}

	pct := s.Watched * 100 / s.Total
	summary := fmt.Sprintf("%s titles · %s watched (%d%%) · %s movies · %s tv",
		styleAccent.Render(fmt.Sprintf("%d", s.Total)),
		styleGood.Render(fmt.Sprintf("%d", s.Watched)),
		pct,
		styleAccent.Render(fmt.Sprintf("%d", s.MovieCount)),
		styleAccent.Render(fmt.Sprintf("%d", s.TVCount)),
	)
	if s.RuntimeSamples > 0 {
		summary += styleSubtle.Render(fmt.Sprintf("  ·  %d min avg runtime", s.AvgRuntimeMins))
	}
	if s.Unenriched > 0 {
		summary += "  " + styleWarn.Render(fmt.Sprintf("· %d need enrichment", s.Unenriched))
	}

	blocks := []string{title, "", summary, ""}

	if len(s.Genres) > 0 {
		genres := s.Genres
		if len(genres) > 8 {
			genres = genres[:8]
		}
		labels := make([]string, len(genres))
		counts := make([]int, len(genres))
		for i, g := range genres {
			labels[i], counts[i] = g.Genre, g.Count
		}
		panel := stylePanel.Render(lipgloss.JoinVertical(lipgloss.Left,
			styleTitle.Foreground(cViolet).Render("GENRES"),
			m.barChart(labels, counts, cCyan),
		))
		blocks = append(blocks, panel, "")
	}

	if len(s.Decades) > 0 {
		labels := make([]string, len(s.Decades))
		counts := make([]int, len(s.Decades))
		for i, d := range s.Decades {
			labels[i] = fmt.Sprintf("%ds", d.Decade)
			counts[i] = d.Count
		}
		panel := stylePanel.Render(lipgloss.JoinVertical(lipgloss.Left,
			styleTitle.Foreground(cViolet).Render("DECADES"),
			m.barChart(labels, counts, cGold),
		))
		blocks = append(blocks, panel, "")
	}

	blocks = append(blocks, foot)
	return lipgloss.JoinVertical(lipgloss.Left, blocks...)
}

// barChart renders a horizontal bar per label, scaled so the largest count
// fills barWidth cells.
func (m model) barChart(labels []string, counts []int, color lipgloss.Color) string {
	const barWidth = 24
	max, labelWidth := 0, 0
	for i, n := range counts {
		if n > max {
			max = n
		}
		if len(labels[i]) > labelWidth {
			labelWidth = len(labels[i])
		}
	}
	if max == 0 {
		return ""
	}

	labelStyle := lipgloss.NewStyle().Foreground(cFg)
	barStyle := lipgloss.NewStyle().Foreground(color)
	countStyle := lipgloss.NewStyle().Foreground(cSubtle)

	var rows []string
	for i, n := range counts {
		filled := n * barWidth / max
		if filled == 0 && n > 0 {
			filled = 1
		}
		bar := barStyle.Render(strings.Repeat("█", filled))
		row := fmt.Sprintf("%s %s %s",
			labelStyle.Width(labelWidth).Render(labels[i]),
			bar,
			countStyle.Render(fmt.Sprintf("%d", n)),
		)
		rows = append(rows, row)
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}
