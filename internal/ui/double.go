package ui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/jarvis1704/whatnext/internal/pick"
	"github.com/jarvis1704/whatnext/internal/store"
)

type doublePhase int

const (
	dblEntry doublePhase = iota
	dblFound
	dblEmpty
)

// doubleState backs the "double feature" flow: pick two unwatched titles
// whose combined runtime fits a budget.
type doubleState struct {
	phase   doublePhase
	budget  int // minutes; 0 = no cap
	a, b    store.Title
	openIdx int // -1 = no open picker focused; 0/1 = which title it's for
	openMsg string
}

// ---- update ----

func (m model) updateDouble(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.double.phase == dblEntry {
		return m.updateDoubleEntry(msg)
	}
	if m.double.openIdx >= 0 {
		return m.chooseDoubleOpenTarget(msg.String())
	}
	switch msg.String() {
	case "esc", "backspace", "h", "left":
		m.goTo(scrHome)
		return m, nil
	}
	if m.double.phase != dblFound {
		return m, nil
	}
	switch msg.String() {
	case "enter", "r": // reshuffle with the same budget
		return m.rollDouble(m.double.budget)
	case "w": // mark both watched
		return m.markDoubleWatched()
	case "1":
		m.double.openIdx = 0
		m.double.openMsg = ""
	case "2":
		m.double.openIdx = 1
		m.double.openMsg = ""
	}
	return m, nil
}

func (m model) updateDoubleEntry(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.goTo(scrHome)
		return m, nil
	case "enter":
		val := strings.TrimSpace(m.doubleInput.Value())
		budget := 0
		if val != "" {
			n, err := strconv.Atoi(val)
			if err != nil || n <= 0 {
				m.doubleErr = "Enter a whole number of minutes, or leave blank for no limit"
				return m, nil
			}
			budget = n
		}
		m.doubleErr = ""
		return m.rollDouble(budget)
	}
	var cmd tea.Cmd
	m.doubleInput, cmd = m.doubleInput.Update(msg)
	return m, cmd
}

// rollDouble picks a fresh pair against budget, staying on scrDouble.
func (m model) rollDouble(budget int) (tea.Model, tea.Cmd) {
	cands := pick.Candidates(m.data.Titles, pick.Filter{})
	a, b, ok := pick.DoubleFeature(cands, budget)
	if ok {
		m.double = doubleState{phase: dblFound, budget: budget, a: a, b: b, openIdx: -1}
	} else {
		m.double = doubleState{phase: dblEmpty, budget: budget, openIdx: -1}
	}
	return m, nil
}

// chooseDoubleOpenTarget handles a keypress while an "open" picker is
// focused for title A or B: l/t/y opens the link, anything else (esc
// included) just dismisses the picker.
func (m model) chooseDoubleOpenTarget(key string) (tea.Model, tea.Cmd) {
	t := m.double.a
	if m.double.openIdx == 1 {
		t = m.double.b
	}
	m.double.openIdx = -1
	if label, u, ok := resolveOpenTarget(t, key); ok {
		m.double.openMsg = openTarget(label, u)
	}
	return m, nil
}

// markDoubleWatched marks both titles watched in one action. The undo log
// only tracks a single most-recent change, so "u" elsewhere in the app will
// only revert the second title — the same single-slot limitation undo
// already has everywhere else.
func (m model) markDoubleWatched() (tea.Model, tea.Cmd) {
	for _, uri := range []string{m.double.a.LetterboxdURI, m.double.b.LetterboxdURI} {
		for i := range m.data.Titles {
			if m.data.Titles[i].LetterboxdURI == uri {
				_ = store.RecordUndo(m.data.Titles[i])
				m.data.Titles[i].Watched = true
				break
			}
		}
	}
	m.double.a.Watched = true
	m.double.b.Watched = true
	m.dirty = true
	_ = m.data.Save()
	m.dirty = false
	m.flash = "✓ marked watched: " + m.double.a.Name + " + " + m.double.b.Name
	return m, nil
}

// ---- view ----

func (m model) viewDouble() string {
	switch m.double.phase {
	case dblEntry:
		return m.viewDoubleEntry()
	case dblEmpty:
		return m.viewDoubleEmpty()
	default:
		return m.viewDoubleFound()
	}
}

func (m model) viewDoubleEntry() string {
	title := gradientText("🍿  Double feature", float64(m.frame)/40.0, true)
	prompt := stylePanel.Render(lipgloss.JoinVertical(lipgloss.Left,
		styleSubtle.Render("Combined runtime budget, in minutes:"),
		"",
		m.doubleInput.View(),
	))
	var errLine string
	if m.doubleErr != "" {
		errLine = styleBad.Render("✗ " + m.doubleErr)
	}
	foot := m.footer([2]string{"↵", "pick"}, [2]string{"esc", "back"})
	blocks := []string{title, "", prompt, ""}
	if errLine != "" {
		blocks = append(blocks, errLine, "")
	}
	blocks = append(blocks, foot)
	return lipgloss.JoinVertical(lipgloss.Left, blocks...)
}

func (m model) viewDoubleEmpty() string {
	msg := styleWarn.Render("No pair of unwatched titles fits that budget.")
	hint := styleSubtle.Render("Try a bigger budget, or import more titles.")
	if m.double.budget <= 0 {
		msg = styleWarn.Render("Need at least two unwatched titles with a known runtime.")
		hint = styleSubtle.Render("Run Refresh enrichment first.")
	}
	foot := m.footer([2]string{"esc", "back"})
	box := stylePanel.BorderForeground(cAmber).Render(
		lipgloss.JoinVertical(lipgloss.Center, "🍿", "", msg, hint))
	return lipgloss.JoinVertical(lipgloss.Left, box, "", foot)
}

func (m model) viewDoubleFound() string {
	title := gradientText("🍿  Double feature", float64(m.frame)/24.0, true)

	cardA := m.doubleCard(m.double.a, "First")
	cardB := m.doubleCard(m.double.b, "Second")

	total := m.double.a.RuntimeMins + m.double.b.RuntimeMins
	budgetLine := styleSubtle.Render(fmt.Sprintf("combined runtime: %d min", total))
	if m.double.budget > 0 {
		budgetLine += styleSubtle.Render(fmt.Sprintf("  (budget %d min)", m.double.budget))
	}

	var status string
	switch {
	case m.double.openMsg != "":
		status = styleSubtle.Render(m.double.openMsg)
	case m.double.a.Watched && m.double.b.Watched:
		status = styleGood.Render("✓ marked watched")
	}

	foot := m.footer(
		[2]string{"↵/r", "reshuffle"},
		[2]string{"w", "mark both watched"},
		[2]string{"1/2", "open first/second"},
		[2]string{"esc", "back"},
	)

	blocks := []string{title, "", cardA, cardB, budgetLine}
	switch {
	case m.double.openIdx >= 0:
		blocks = append(blocks, "", openMenuBox())
	case status != "":
		blocks = append(blocks, "", status)
	}
	blocks = append(blocks, "", foot)
	return lipgloss.JoinVertical(lipgloss.Center, blocks...)
}

func (m model) doubleCard(t store.Title, label string) string {
	titleLine := lipgloss.NewStyle().Foreground(cGold).Bold(true).Render(t.Name)
	if t.Year != "" {
		titleLine += lipgloss.NewStyle().Foreground(cSubtle).Render("  (" + t.Year + ")")
	}
	var meta string
	if t.Enriched {
		var bits []string
		if len(t.Genres) > 0 {
			bits = append(bits, strings.Join(t.Genres, ", "))
		}
		if t.RuntimeMins > 0 {
			bits = append(bits, fmt.Sprintf("%d min", t.RuntimeMins))
		}
		if t.VoteAverage > 0 {
			bits = append(bits, fmt.Sprintf("★ %.1f", t.VoteAverage))
		}
		meta = styleSubtle.Render(strings.Join(bits, "  ·  "))
	}
	inner := lipgloss.JoinVertical(lipgloss.Center, styleSubtle.Render(label), titleLine, meta)
	return stylePanel.Render(inner)
}
