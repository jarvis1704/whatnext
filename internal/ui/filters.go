package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/jarvis1704/whatnext/internal/mood"
	"github.com/jarvis1704/whatnext/internal/pick"
)

// filterState is the interactive filter builder backing the pick flow.
type filterState struct {
	moodNames   []string
	moodIdx     int // -1 = any mood
	mediaIdx    int // 0 any, 1 movie, 2 tv
	runtimeIx   int // index into runtimeCaps
	providerIdx int // -1 = any provider; else index into m.availableProviders()
	commitIdx   int // index into commitOpts

	directorInput textinput.Model
	actorInput    textinput.Model
	editing       bool // true while directorInput/actorInput (whichever filterIdx points at) has focus
}

var runtimeCaps = []int{0, 90, 120, 150, 999}
var mediaOpts = []string{"any", "movie", "tv"}

// commitOpts are the "how big a commitment" presets. Show-only constraints
// let movies through (they're finished by nature); miniseries is tv-only.
var commitOpts = []struct {
	label string
	apply func(*pick.Filter)
}{
	{"any", func(*pick.Filter) {}},
	{"finished shows only", func(f *pick.Filter) { f.FinishedOnly = true }},
	{"miniseries", func(f *pick.Filter) { f.Miniseries = true }},
	{"≤ 2 seasons", func(f *pick.Filter) { f.MaxSeasons = 2 }},
	{"≤ 10h total", func(f *pick.Filter) { f.MaxTotalMins = 600 }},
}

func newFilterState(cfg *mood.Config) filterState {
	di := textinput.New()
	di.Placeholder = "any director"
	di.Prompt = styleAccent.Render("› ")
	di.CharLimit = 64
	di.Width = 24

	ai := textinput.New()
	ai.Placeholder = "any actor"
	ai.Prompt = styleAccent.Render("› ")
	ai.CharLimit = 64
	ai.Width = 24

	return filterState{
		moodNames: cfg.Names(), moodIdx: -1, providerIdx: -1,
		directorInput: di, actorInput: ai,
	}
}

// filter rows the user can cycle through.
const (
	rowMood = iota
	rowMedia
	rowRuntime
	rowCommit
	rowProvider
	rowDirector
	rowActor
	rowPick
	rowCount
)

func (m model) updateFilters(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.filter.editing {
		return m.updateFilterEditing(msg)
	}
	switch msg.String() {
	case "esc", "backspace", "h", "left":
		m.goTo(scrHome)
		return m, nil
	case "up", "k":
		m.filterIdx = (m.filterIdx - 1 + rowCount) % rowCount
	case "down", "j", "tab":
		m.filterIdx = (m.filterIdx + 1) % rowCount
	case "enter", " ", "l", "right":
		switch m.filterIdx {
		case rowMood:
			m.filter.cycleMood(1)
		case rowMedia:
			m.filter.mediaIdx = (m.filter.mediaIdx + 1) % len(mediaOpts)
		case rowRuntime:
			m.filter.runtimeIx = (m.filter.runtimeIx + 1) % len(runtimeCaps)
		case rowCommit:
			m.filter.commitIdx = (m.filter.commitIdx + 1) % len(commitOpts)
		case rowProvider:
			m.filter.cycleProvider(1, len(m.availableProviders()))
		case rowDirector:
			m.filter.editing = true
			m.filter.directorInput.Focus()
			return m, textinput.Blink
		case rowActor:
			m.filter.editing = true
			m.filter.actorInput.Focus()
			return m, textinput.Blink
		case rowPick:
			return m.startReveal()
		}
	case "r":
		// quick reset
		m.filter.moodIdx, m.filter.mediaIdx, m.filter.runtimeIx, m.filter.providerIdx = -1, 0, 0, -1
		m.filter.commitIdx = 0
		m.filter.directorInput.SetValue("")
		m.filter.actorInput.SetValue("")
	}
	return m, nil
}

// updateFilterEditing routes every key into whichever text field (director
// or actor) filterIdx currently points at, until enter/esc hands focus back
// to row navigation.
func (m model) updateFilterEditing(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "enter":
		m.filter.editing = false
		m.filter.directorInput.Blur()
		m.filter.actorInput.Blur()
		return m, nil
	}
	var cmd tea.Cmd
	switch m.filterIdx {
	case rowDirector:
		m.filter.directorInput, cmd = m.filter.directorInput.Update(msg)
	case rowActor:
		m.filter.actorInput, cmd = m.filter.actorInput.Update(msg)
	}
	return m, cmd
}

func (f *filterState) cycleMood(dir int) {
	// range is [-1 .. len-1], where -1 means "any".
	n := len(f.moodNames)
	f.moodIdx += dir
	if f.moodIdx < -1 {
		f.moodIdx = n - 1
	}
	if f.moodIdx >= n {
		f.moodIdx = -1
	}
}

// cycleProvider is cycleMood's twin for the provider row, whose option count
// (n) is derived from the pool rather than fixed at filterState creation.
func (f *filterState) cycleProvider(dir, n int) {
	if n == 0 {
		f.providerIdx = -1
		return
	}
	f.providerIdx += dir
	if f.providerIdx < -1 {
		f.providerIdx = n - 1
	}
	if f.providerIdx >= n {
		f.providerIdx = -1
	}
}

// availableProviders returns every distinct streaming provider seen across
// the pool, sorted alphabetically.
func (m model) availableProviders() []string {
	seen := map[string]bool{}
	for _, t := range m.data.Titles {
		for _, p := range t.Providers {
			seen[p] = true
		}
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// build converts the UI selections into a pick.Filter.
func (m model) buildFilter() pick.Filter {
	f := pick.Filter{}
	if m.filter.moodIdx >= 0 {
		name := m.filter.moodNames[m.filter.moodIdx]
		if g, ok := m.moods.Genres(name); ok {
			f.MoodGenres = g
		}
	}
	if m.filter.mediaIdx > 0 {
		f.MediaType = mediaOpts[m.filter.mediaIdx]
	}
	if cap := runtimeCaps[m.filter.runtimeIx]; cap > 0 && cap < 999 {
		f.MaxRuntime = cap
	}
	commitOpts[m.filter.commitIdx].apply(&f)
	if providers := m.availableProviders(); m.filter.providerIdx >= 0 && m.filter.providerIdx < len(providers) {
		f.Provider = providers[m.filter.providerIdx]
	}
	if v := strings.TrimSpace(m.filter.directorInput.Value()); v != "" {
		f.Director = v
	}
	if v := strings.TrimSpace(m.filter.actorInput.Value()); v != "" {
		f.Actor = v
	}
	return f
}

func (m model) viewFilters() string {
	title := gradientText("Tune your pick", float64(m.frame)/40.0, true)

	moodVal := "any mood"
	if m.filter.moodIdx >= 0 {
		moodVal = m.filter.moodNames[m.filter.moodIdx]
	}
	runtimeVal := "any length"
	if c := runtimeCaps[m.filter.runtimeIx]; c > 0 && c < 999 {
		runtimeVal = fmt.Sprintf("≤ %d min", c)
	}

	providers := m.availableProviders()
	providerVal := "any provider"
	switch {
	case len(providers) == 0:
		providerVal = "no provider data yet — refresh"
	case m.filter.providerIdx >= 0 && m.filter.providerIdx < len(providers):
		providerVal = providers[m.filter.providerIdx]
	}

	rows := []string{
		m.filterRow(rowMood, "Mood", moodVal, m.moodChips()),
		m.filterRow(rowMedia, "Type", mediaOpts[m.filter.mediaIdx], ""),
		m.filterRow(rowRuntime, "Runtime", runtimeVal, styleSubtle.Render("   per sitting — one episode for a show")),
		m.filterRow(rowCommit, "Commit", commitOpts[m.filter.commitIdx].label, ""),
		m.filterRow(rowProvider, "Watch on", providerVal, m.providerChips(providers)),
		m.filterTextRow(rowDirector, "Director", m.filter.directorInput),
		m.filterTextRow(rowActor, "Actor", m.filter.actorInput),
		"",
		m.pickButton(),
	}

	// Live candidate count so the user sees the pool narrow as they tune.
	cands := pick.Candidates(m.data.Titles, m.buildFilter())
	count := styleSubtle.Render("matching now: ") + styleAccent.Render(fmt.Sprintf("%d", len(cands)))

	panel := stylePanel.Render(lipgloss.JoinVertical(lipgloss.Left, rows...))

	foot := m.footer(
		[2]string{"↑↓", "field"},
		[2]string{"↵/→", "change"},
		[2]string{"r", "reset"},
		[2]string{"esc", "back"},
	)
	if m.filter.editing {
		foot = m.footer([2]string{"↵/esc", "done editing"})
	}
	return lipgloss.JoinVertical(lipgloss.Left, title, "", panel, "", count, "", foot)
}

func (m model) filterRow(idx int, label, value, extra string) string {
	selected := m.filterIdx == idx
	marker := "  "
	labelStyle := lipgloss.NewStyle().Foreground(cSubtle)
	valStyle := lipgloss.NewStyle().Foreground(cFg)
	if selected {
		markCol := lipgloss.Color(gradientAt(float64(m.frame) / 30.0).Clamped().Hex())
		marker = lipgloss.NewStyle().Foreground(markCol).Bold(true).Render("▸ ")
		labelStyle = labelStyle.Foreground(cFg).Bold(true)
		valStyle = valStyle.Foreground(cGold).Bold(true)
	}
	line := marker + labelStyle.Width(10).Render(label) + valStyle.Render(value)
	if extra != "" && selected {
		line += "\n" + extra
	}
	return line
}

// filterTextRow renders a free-text filter row (director/actor): the live
// input box while it's focused for editing, the entered value or a dim
// placeholder otherwise.
func (m model) filterTextRow(idx int, label string, ti textinput.Model) string {
	selected := m.filterIdx == idx
	marker := "  "
	labelStyle := lipgloss.NewStyle().Foreground(cSubtle)
	if selected {
		markCol := lipgloss.Color(gradientAt(float64(m.frame) / 30.0).Clamped().Hex())
		marker = lipgloss.NewStyle().Foreground(markCol).Bold(true).Render("▸ ")
		labelStyle = labelStyle.Foreground(cFg).Bold(true)
	}
	var value string
	switch {
	case selected && m.filter.editing:
		value = ti.View()
	case ti.Value() != "":
		value = lipgloss.NewStyle().Foreground(cGold).Bold(true).Render(ti.Value())
	default:
		value = styleSubtle.Render(ti.Placeholder)
	}
	return marker + labelStyle.Width(10).Render(label) + value
}

// moodChips renders the mood options as a chip row, highlighting the active one.
func (m model) moodChips() string {
	var chips []string
	chips = append(chips, m.chip("any", m.filter.moodIdx == -1))
	for i, name := range m.filter.moodNames {
		chips = append(chips, m.chip(name, m.filter.moodIdx == i))
	}
	return "   " + lipgloss.JoinHorizontal(lipgloss.Top, spaceChips(chips)...)
}

// providerChips renders the streaming-provider options as a chip row, like
// moodChips. Empty when the pool has no provider data yet.
func (m model) providerChips(providers []string) string {
	if len(providers) == 0 {
		return ""
	}
	var chips []string
	chips = append(chips, m.chip("any", m.filter.providerIdx == -1))
	for i, name := range providers {
		chips = append(chips, m.chip(name, m.filter.providerIdx == i))
	}
	return "   " + lipgloss.JoinHorizontal(lipgloss.Top, spaceChips(chips)...)
}

func (m model) chip(label string, on bool) string {
	if on {
		return styleChipOn.Render(label)
	}
	return styleChipOff.Render(label)
}

func spaceChips(chips []string) []string {
	out := make([]string, 0, len(chips)*2)
	for i, c := range chips {
		if i > 0 {
			out = append(out, " ")
		}
		out = append(out, c)
	}
	return out
}

func (m model) pickButton() string {
	selected := m.filterIdx == rowPick
	label := "  🎬  Pick it  "
	if selected {
		phase := float64(m.frame) / 18.0
		return gradientText("▸ "+label, phase, true)
	}
	return styleSubtle.Render("   " + label)
}
