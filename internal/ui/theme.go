package ui

import (
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
	colorful "github.com/lucasb-eyer/go-colorful"
)

// Cinema-marquee palette. Dark-first, works on any truecolor terminal.
var (
	cGold   = lipgloss.Color("#F5C518")
	cAmber  = lipgloss.Color("#FF8C42")
	cPink   = lipgloss.Color("#F472B6")
	cViolet = lipgloss.Color("#A78BFA")
	cIndigo = lipgloss.Color("#7C3AED")
	cCyan   = lipgloss.Color("#22D3EE")
	cGreen  = lipgloss.Color("#4ADE80")
	cRed    = lipgloss.Color("#F87171")
	cFg     = lipgloss.Color("#E5E7EB")
	cSubtle = lipgloss.Color("#6B7280")
	cFaint  = lipgloss.Color("#3F3F52")
)

// Shared styles.
var (
	styleApp = lipgloss.NewStyle().Padding(1, 3)

	styleTitle = lipgloss.NewStyle().Bold(true)

	styleSubtle = lipgloss.NewStyle().Foreground(cSubtle)

	styleAccent = lipgloss.NewStyle().Foreground(cGold).Bold(true)

	stylePanel = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(cIndigo).
			Padding(1, 3)

	styleCard = lipgloss.NewStyle().
			Border(lipgloss.DoubleBorder()).
			BorderForeground(cGold).
			Padding(1, 4)

	styleKeyHint = lipgloss.NewStyle().Foreground(cSubtle)

	styleKeyCap = lipgloss.NewStyle().Foreground(cViolet).Bold(true)

	styleGood = lipgloss.NewStyle().Foreground(cGreen)
	styleBad  = lipgloss.NewStyle().Foreground(cRed)
	styleWarn = lipgloss.NewStyle().Foreground(cAmber)

	// Chips are single-line (height 1) so bordered/unbordered never mix and
	// rows stay aligned within their panel.
	styleChipOn = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#1A1626")).
			Background(cGold).
			Bold(true).
			Padding(0, 1)

	styleChipOff = lipgloss.NewStyle().
			Foreground(cSubtle).
			Padding(0, 1)
)

// gradientStops are the marquee gradient's color anchors, blended in a loop.
var gradientStops = []colorful.Color{
	hex("#F5C518"),
	hex("#FF8C42"),
	hex("#F472B6"),
	hex("#A78BFA"),
	hex("#22D3EE"),
}

func hex(s string) colorful.Color {
	c, _ := colorful.Hex(s)
	return c
}

// gradientAt samples the looping gradient at position p in [0,1).
func gradientAt(p float64) colorful.Color {
	p = p - math.Floor(p)
	n := len(gradientStops)
	scaled := p * float64(n)
	i := int(scaled) % n
	j := (i + 1) % n
	frac := scaled - math.Floor(scaled)
	return gradientStops[i].BlendLuv(gradientStops[j], frac)
}

// gradientText paints s with a horizontal gradient that scrolls with phase, so
// calling it each frame produces a smooth flowing shimmer.
func gradientText(s string, phase float64, bold bool) string {
	runes := []rune(s)
	n := len(runes)
	if n == 0 {
		return ""
	}
	var b strings.Builder
	for i, r := range runes {
		if r == ' ' || r == '\n' {
			b.WriteRune(r)
			continue
		}
		p := phase + float64(i)/float64(max(n, 1))*1.2
		col := lipgloss.Color(gradientAt(p).Clamped().Hex())
		st := lipgloss.NewStyle().Foreground(col)
		if bold {
			st = st.Bold(true)
		}
		b.WriteString(st.Render(string(r)))
	}
	return b.String()
}

// pulse returns a 0..1 eased brightness value for the given frame, used to make
// selection markers and prompts breathe.
func pulse(frame int, periodFrames float64) float64 {
	x := math.Sin(float64(frame)/periodFrames*2*math.Pi)*0.5 + 0.5
	return x
}

// dimColor linearly darkens a color toward black by amount t in [0,1].
func dimColor(c colorful.Color, t float64) lipgloss.Color {
	black := colorful.Color{R: 0, G: 0, B: 0}
	return lipgloss.Color(c.BlendLuv(black, t).Clamped().Hex())
}

// sprocket renders a film-strip row of the given inner width, with holes that
// drift with phase to suggest the reel turning.
func sprocket(width int, phase float64) string {
	if width < 2 {
		width = 2
	}
	off := int(phase*8) % 4
	var b strings.Builder
	for i := 0; i < width; i++ {
		if (i+off)%4 == 0 {
			b.WriteString("▢")
		} else {
			b.WriteString(" ")
		}
	}
	return lipgloss.NewStyle().Foreground(cFaint).Render(b.String())
}

// renderBanner draws the letter-spaced wordmark between two drifting film
// sprocket strips, with the flowing gradient sweeping across the letters.
func renderBanner(phase float64) string {
	word := gradientText("W H A T N E X T", phase, true)
	inner := lipgloss.Width(word)
	top := sprocket(inner, phase)
	bot := sprocket(inner, phase+0.5)
	tag := styleSubtle.Render("· pick your next watch ·")
	return strings.Join([]string{top, word, bot, "", tag}, "\n")
}
