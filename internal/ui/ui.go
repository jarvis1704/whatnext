// Package ui is the Bubble Tea front-end for whatnext: a full-screen,
// animated, keyboard-driven menu over the watchlist pool.
package ui

import (
	"os"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/jarvis1704/whatnext/internal/mood"
	"github.com/jarvis1704/whatnext/internal/store"
	"github.com/jarvis1704/whatnext/internal/tmdb"
)

// screen identifies which view is active.
type screen int

const (
	scrHome screen = iota
	scrFilters
	scrReveal
	scrBrowse
	scrImport
	scrProgress
	scrStats
	scrRegion
	scrOnboardKey
	scrOnboardRegion
	scrOnboardPath
)

// isTextEntry reports whether the active screen has a focused text input,
// where the "q" global-quit shortcut must be treated as literal input
// instead.
func (m model) isTextEntry() bool {
	switch m.screen {
	case scrImport, scrRegion, scrOnboardKey, scrOnboardRegion, scrOnboardPath:
		return true
	case scrBrowse:
		return m.browse.filtering
	}
	return false
}

// frameInterval drives every ambient animation (gradient, pulses, sprockets).
const frameInterval = 70 * time.Millisecond

// tickMsg advances the global animation frame counter.
type tickMsg time.Time

func tickCmd() tea.Cmd {
	return tea.Tick(frameInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// model is the whole application state.
type model struct {
	width, height int
	frame         int
	screen        screen
	prev          screen // where "back" returns to

	data     *store.Data
	moods    *mood.Config
	tmdb     *tmdb.Client
	tmdbErr  error  // non-nil when TMDB_API_KEY is missing
	region   string // ISO country code for watch-provider lookups
	dirty    bool   // pool changed since last save
	quitting bool

	// home
	homeIdx int

	// filters
	filter    filterState
	filterIdx int

	// reveal (slot machine)
	reveal revealState

	// browse
	browse browseState

	// onboarding + import
	onboarding  bool
	keyInput    textinput.Model
	regionInput textinput.Model
	pathInput   textinput.Model
	importErr   string

	// progress (import/refresh enrichment)
	prog progressState

	spinner spinner.Model
	bar     progress.Model

	flash string // transient status line
}

// New builds the initial model, loading persisted state up front.
func New() (model, error) {
	data, err := store.Load()
	if err != nil {
		return model{}, err
	}
	moods, err := mood.Load()
	if err != nil {
		return model{}, err
	}

	var client *tmdb.Client
	var keyErr error
	client, keyErr = tmdb.New(store.ResolveKey())

	ti := textinput.New()
	ti.Placeholder = "~/Downloads/letterboxd-export.zip"
	ti.Prompt = styleAccent.Render("› ")
	ti.CharLimit = 512
	ti.Width = 48

	ki := textinput.New()
	ki.Placeholder = "paste your TMDb v3 API key…"
	ki.Prompt = styleAccent.Render("› ")
	ki.CharLimit = 128
	ki.Width = 48

	ri := textinput.New()
	ri.Placeholder = "US"
	ri.Prompt = styleAccent.Render("› ")
	ri.CharLimit = 2
	ri.Width = 8

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(cGold)

	bar := progress.New(
		progress.WithScaledGradient("#7C3AED", "#F5C518"),
		progress.WithoutPercentage(),
	)

	m := model{
		screen:      scrHome,
		data:        data,
		moods:       moods,
		tmdb:        client,
		tmdbErr:     keyErr,
		region:      store.ResolveRegion(),
		pathInput:   ti,
		keyInput:    ki,
		regionInput: ri,
		spinner:     sp,
		bar:         bar,
		filter:      newFilterState(moods),
	}

	// First run (no config.json yet) → walk the onboarding flow. If a key is
	// already supplied via the environment, skip straight to the region step.
	if !store.ConfigExists() {
		m.onboarding = true
		if os.Getenv("TMDB_API_KEY") != "" {
			m.screen = scrOnboardRegion
			m.regionInput.Focus()
		} else {
			m.screen = scrOnboardKey
			m.keyInput.Focus()
		}
	}
	return m, nil
}

func (m model) Init() tea.Cmd {
	return tea.Batch(tickCmd(), m.spinner.Tick, textinput.Blink)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.bar.Width = min(msg.Width-16, 48)
		return m, nil

	case tickMsg:
		m.frame++
		return m, tickCmd()

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case progress.FrameMsg:
		pm, cmd := m.bar.Update(msg)
		m.bar = pm.(progress.Model)
		return m, cmd

	case tea.KeyMsg:
		// Global quit works everywhere except in text-entry screens, where "q"
		// is literal input (ctrl+c still quits).
		if m.isTextEntry() {
			if msg.String() == "ctrl+c" {
				return m.quit()
			}
		} else {
			switch msg.String() {
			case "ctrl+c", "q":
				return m.quit()
			}
		}
		return m.updateScreen(msg)

	// Screen-specific async messages.
	case revealTickMsg:
		return m.updateRevealTick()
	case parsedTitlesMsg:
		return m.afterParse(msg)
	case enrichStepMsg:
		return m.updateEnrichStep(msg)
	case runStepMsg:
		return m.updateRunStep(msg)
	}
	return m, nil
}

// updateScreen dispatches key input to the active view.
func (m model) updateScreen(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.screen {
	case scrHome:
		return m.updateHome(msg)
	case scrFilters:
		return m.updateFilters(msg)
	case scrReveal:
		return m.updateReveal(msg)
	case scrBrowse:
		return m.updateBrowse(msg)
	case scrImport:
		return m.updateImport(msg)
	case scrProgress:
		return m.updateProgress(msg)
	case scrStats:
		return m.updateStats(msg)
	case scrRegion:
		return m.updateRegion(msg)
	case scrOnboardKey:
		return m.updateOnboardKey(msg)
	case scrOnboardRegion:
		return m.updateOnboardRegion(msg)
	case scrOnboardPath:
		return m.updateOnboardPath(msg)
	}
	return m, nil
}

func (m model) quit() (tea.Model, tea.Cmd) {
	if m.dirty {
		_ = m.data.Save()
	}
	m.quitting = true
	return m, tea.Quit
}

// goto switches screens, remembering the previous one for back-navigation.
func (m *model) goTo(s screen) {
	m.prev = m.screen
	m.screen = s
}

func (m model) View() string {
	if m.quitting {
		return styleSubtle.Render("  see you at the movies 🎬\n")
	}
	if m.width == 0 {
		return "\n  loading…\n"
	}

	var body string
	switch m.screen {
	case scrHome:
		body = m.viewHome()
	case scrFilters:
		body = m.viewFilters()
	case scrReveal:
		body = m.viewReveal()
	case scrBrowse:
		body = m.viewBrowse()
	case scrImport:
		body = m.viewImport()
	case scrProgress:
		body = m.viewProgress()
	case scrStats:
		body = m.viewStats()
	case scrRegion:
		body = m.viewRegion()
	case scrOnboardKey:
		body = m.viewOnboardKey()
	case scrOnboardRegion:
		body = m.viewOnboardRegion()
	case scrOnboardPath:
		body = m.viewOnboardPath()
	}
	return styleApp.Render(body)
}

// footer renders a dim key-hint line from label/key pairs.
func (m model) footer(pairs ...[2]string) string {
	var parts []string
	for _, p := range pairs {
		parts = append(parts, styleKeyCap.Render(p[0])+" "+styleKeyHint.Render(p[1]))
	}
	sep := styleKeyHint.Render("   ")
	return lipgloss.JoinHorizontal(lipgloss.Left, joinWith(parts, sep))
}

func joinWith(parts []string, sep string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += sep
		}
		out += p
	}
	return out
}

// Run launches the alt-screen TUI program.
func Run() error {
	m, err := New()
	if err != nil {
		return err
	}
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err = p.Run()
	return err
}
