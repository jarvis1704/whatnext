package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"whatnext/internal/pick"
	"whatnext/internal/store"
)

type revealPhase int

const (
	revShuffling revealPhase = iota
	revLanded
	revEmpty
)

type revealState struct {
	candidates []store.Title
	phase      revealPhase
	spins      int         // remaining shuffle steps
	displayIdx int         // which candidate name is flashing now
	result     store.Title // final pick
	landedAt   int         // frame the result locked in (for fade-in)
}

type revealTickMsg struct{}

const totalSpins = 16

func (m model) startReveal() (tea.Model, tea.Cmd) {
	cands := pick.Candidates(m.data.Titles, m.buildFilter())
	m.reveal = revealState{candidates: cands}
	m.goTo(scrReveal)

	if len(cands) == 0 {
		m.reveal.phase = revEmpty
		return m, nil
	}
	m.reveal.phase = revShuffling
	m.reveal.spins = totalSpins
	return m, scheduleSpin(0)
}

// scheduleSpin schedules the next slot step; delay grows linearly with step to
// create a decelerating "settling" feel. Total run ≈ 1.7s.
func scheduleSpin(step int) tea.Cmd {
	delay := time.Duration(25+step*11) * time.Millisecond
	return tea.Tick(delay, func(time.Time) tea.Msg { return revealTickMsg{} })
}

func (m model) updateRevealTick() (tea.Model, tea.Cmd) {
	if m.reveal.phase != revShuffling {
		return m, nil
	}
	n := len(m.reveal.candidates)
	// advance the flashing name; +7 keeps it from looking like a simple scroll
	m.reveal.displayIdx = (m.reveal.displayIdx + 7) % n
	m.reveal.spins--
	if m.reveal.spins <= 0 {
		res, _ := pick.Random(m.reveal.candidates)
		m.reveal.result = res
		m.reveal.phase = revLanded
		m.reveal.landedAt = m.frame
		return m, tea.Batch(bell())
	}
	step := totalSpins - m.reveal.spins
	return m, scheduleSpin(step)
}

func (m model) updateReveal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "backspace", "h", "left":
		m.goTo(scrFilters)
		return m, nil
	}
	if m.reveal.phase != revLanded {
		return m, nil
	}
	switch msg.String() {
	case "enter", "r": // reshuffle
		return m.startReveal()
	case "w": // mark this pick watched
		return m.markWatchedURI(m.reveal.result.LetterboxdURI)
	}
	return m, nil
}

func (m model) markWatchedURI(uri string) (tea.Model, tea.Cmd) {
	for i := range m.data.Titles {
		if m.data.Titles[i].LetterboxdURI == uri {
			m.data.Titles[i].Watched = true
			m.reveal.result.Watched = true
			m.dirty = true
			_ = m.data.Save()
			m.dirty = false
			m.flash = "✓ marked watched: " + m.data.Titles[i].Name
			break
		}
	}
	return m, nil
}

func (m model) viewReveal() string {
	switch m.reveal.phase {
	case revEmpty:
		msg := styleWarn.Render("No unwatched titles match those filters.")
		hint := styleSubtle.Render("Loosen a filter, or import more titles.")
		foot := m.footer([2]string{"esc", "back to filters"})
		box := stylePanel.BorderForeground(cAmber).Render(
			lipgloss.JoinVertical(lipgloss.Center, "🍿", "", msg, hint))
		return lipgloss.JoinVertical(lipgloss.Left, box, "", foot)

	case revShuffling:
		return m.viewShuffling()

	default:
		return m.viewLanded()
	}
}

func (m model) viewShuffling() string {
	name := m.reveal.candidates[m.reveal.displayIdx].Name
	// The flashing name shimmers; a reel of neighbors flickers above/below.
	phase := float64(m.frame) / 8.0
	center := gradientText("▶  "+name, phase, true)

	above := m.reelLine(m.reveal.displayIdx-1, 0.5)
	below := m.reelLine(m.reveal.displayIdx+1, 0.5)

	reel := lipgloss.JoinVertical(lipgloss.Center, above, center, below)
	box := styleCard.BorderForeground(
		lipgloss.Color(gradientAt(phase).Clamped().Hex()),
	).Render(reel)

	label := styleSubtle.Render("shuffling " + strings.Repeat("·", 1+m.frame%4))
	return lipgloss.JoinVertical(lipgloss.Center, label, "", box)
}

func (m model) reelLine(idx int, dim float64) string {
	n := len(m.reveal.candidates)
	idx = ((idx % n) + n) % n
	name := m.reveal.candidates[idx].Name
	if len(name) > 34 {
		name = name[:33] + "…"
	}
	col := dimColor(hex("#A78BFA"), dim)
	return lipgloss.NewStyle().Foreground(col).Render("   " + name + "   ")
}

func (m model) viewLanded() string {
	t := m.reveal.result

	// Fade-in: brighten the card contents over the first ~9 frames.
	age := m.frame - m.reveal.landedAt
	fade := clampF(float64(age)/9.0, 0, 1)

	titleCol := dimColor(hex("#F5C518"), 1-fade)
	titleLine := lipgloss.NewStyle().Foreground(titleCol).Bold(true).
		Render(fmt.Sprintf("%s", t.Name))
	if t.Year != "" {
		titleLine += lipgloss.NewStyle().Foreground(dimColor(hex("#6B7280"), 1-fade)).
			Render("  (" + t.Year + ")")
	}

	// Metadata line, only meaningful once enriched.
	var meta string
	if t.Enriched {
		bits := []string{}
		if t.MediaType != "" {
			bits = append(bits, strings.ToUpper(t.MediaType[:1])+t.MediaType[1:])
		}
		if len(t.Genres) > 0 {
			bits = append(bits, strings.Join(t.Genres, ", "))
		}
		if t.RuntimeMins > 0 {
			bits = append(bits, fmt.Sprintf("%d min", t.RuntimeMins))
		}
		meta = lipgloss.NewStyle().Foreground(dimColor(hex("#E5E7EB"), 0.2+(1-fade)*0.6)).
			Render(strings.Join(bits, "  ·  "))
	} else {
		meta = styleSubtle.Render("not yet enriched — no genre/runtime data")
	}

	marquee := gradientText("🎬  Tonight's pick", float64(m.frame)/24.0, true)

	inner := lipgloss.JoinVertical(lipgloss.Center, marquee, "", titleLine, meta)
	borderCol := lipgloss.Color(gradientAt(float64(m.frame) / 50.0).Clamped().Hex())
	card := styleCard.BorderForeground(borderCol).Render(inner)

	var status string
	if t.Watched {
		status = styleGood.Render("✓ marked watched")
	}

	foot := m.footer(
		[2]string{"↵/r", "reshuffle"},
		[2]string{"w", "mark watched"},
		[2]string{"esc", "filters"},
	)
	blocks := []string{card, ""}
	if status != "" {
		blocks = append(blocks, status, "")
	}
	blocks = append(blocks, foot)
	return lipgloss.JoinVertical(lipgloss.Center, blocks...)
}

// bell emits a terminal bell as the reel locks in.
func bell() tea.Cmd {
	return func() tea.Msg {
		fmt.Print("\a")
		return nil
	}
}

func clampF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
