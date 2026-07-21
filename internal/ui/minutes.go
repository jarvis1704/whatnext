package ui

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/jarvis1704/whatnext/internal/pick"
)

// updateMinutes drives the "I have X minutes" quick-pick screen: a single
// numeric input that jumps straight to the reveal, skipping the tuned
// filters screen entirely.
func (m model) updateMinutes(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.goTo(scrHome)
		return m, nil
	case "enter":
		n, err := strconv.Atoi(strings.TrimSpace(m.minutesInput.Value()))
		if err != nil || n <= 0 {
			m.minutesErr = "Enter a whole number of minutes, e.g. 45"
			return m, nil
		}
		m.minutesErr = ""
		return m.startRevealWithFilter(pick.Filter{MaxRuntime: n})
	}
	var cmd tea.Cmd
	m.minutesInput, cmd = m.minutesInput.Update(msg)
	return m, cmd
}

func (m model) viewMinutes() string {
	title := gradientText("How many minutes do you have?", float64(m.frame)/40.0, true)
	prompt := stylePanel.Render(lipgloss.JoinVertical(lipgloss.Left,
		styleSubtle.Render("Runtime budget, in minutes:"),
		"",
		m.minutesInput.View(),
	))
	var errLine string
	if m.minutesErr != "" {
		errLine = styleBad.Render("✗ " + m.minutesErr)
	}
	foot := m.footer([2]string{"↵", "pick instantly"}, [2]string{"esc", "back"})
	blocks := []string{title, "", prompt, ""}
	if errLine != "" {
		blocks = append(blocks, errLine, "")
	}
	blocks = append(blocks, foot)
	return lipgloss.JoinVertical(lipgloss.Left, blocks...)
}
