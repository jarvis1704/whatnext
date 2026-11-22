package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/jarvis1704/whatnext/internal/enrich"
	"github.com/jarvis1704/whatnext/internal/store"
)

// ---- step 1: TMDb API key ----

func (m model) updateOnboardKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		k := strings.TrimSpace(m.keyInput.Value())
		m.saveKey(k) // persists config.json (marks onboarding underway)
		m.keyInput.Blur()
		m.regionInput.Focus()
		m.goTo(scrOnboardRegion)
		return m, textinput.Blink
	case "esc":
		// Skip the key for now; still persist config so we don't re-onboard.
		m.saveKey("")
		m.keyInput.Blur()
		m.regionInput.Focus()
		m.goTo(scrOnboardRegion)
		return m, textinput.Blink
	}
	var cmd tea.Cmd
	m.keyInput, cmd = m.keyInput.Update(msg)
	return m, cmd
}

// saveKey writes the key to config.json and rebuilds the TMDb client so the
// rest of the session can use it immediately.
func (m *model) saveKey(k string) {
	cfg, _ := store.LoadConfig()
	cfg.TMDbKey = k
	_ = cfg.Save()
	if client, err := enrich.NewClient(store.ResolveKey()); err == nil {
		m.tmdb, m.tmdbErr = client, nil
	} else {
		m.tmdb, m.tmdbErr = nil, err
	}
}

func (m model) viewOnboardKey() string {
	welcome := renderBanner(float64(m.frame) / 40.0)
	step := styleAccent.Render("Step 1 of 3") + styleSubtle.Render("  ·  TMDb API key")

	body := lipgloss.JoinVertical(lipgloss.Left,
		styleSubtle.Render("whatnext enriches your watchlist with genre, runtime, and"),
		styleSubtle.Render("movie/TV data from TMDb. Paste a free v3 API key to enable it."),
		"",
		styleSubtle.Render("Get one at ")+styleKeyCap.Render("developer.themoviedb.org"),
		"",
		m.keyInput.View(),
	)
	panel := stylePanel.Render(body)

	note := styleSubtle.Render("Stored locally at ~/.whatnext/config.json (chmod 600). Never sent anywhere but TMDb.")
	foot := m.footer(
		[2]string{"↵", "save & continue"},
		[2]string{"esc", "skip for now"},
		[2]string{"ctrl+c", "quit"},
	)
	return lipgloss.JoinVertical(lipgloss.Left, welcome, "", step, "", panel, "", note, "", foot)
}

// ---- step 2: watch-provider region ----

func (m model) updateOnboardRegion(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		r := strings.ToUpper(strings.TrimSpace(m.regionInput.Value()))
		if r == "" {
			r = store.DefaultRegion
		}
		m.saveRegion(r)
		m.regionInput.Blur()
		m.pathInput.Focus()
		m.goTo(scrOnboardPath)
		return m, textinput.Blink
	case "esc":
		// Skip; default region is still saved so we don't re-onboard.
		m.saveRegion(store.DefaultRegion)
		m.regionInput.Blur()
		m.pathInput.Focus()
		m.goTo(scrOnboardPath)
		return m, textinput.Blink
	}
	var cmd tea.Cmd
	m.regionInput, cmd = m.regionInput.Update(msg)
	return m, cmd
}

// saveRegion writes the region to config.json and updates the live model so
// enrichment this session uses it immediately.
func (m *model) saveRegion(r string) {
	cfg, _ := store.LoadConfig()
	cfg.Region = r
	_ = cfg.Save()
	m.region = r
}

func (m model) viewOnboardRegion() string {
	welcome := renderBanner(float64(m.frame) / 40.0)
	step := styleAccent.Render("Step 2 of 3") + styleSubtle.Render("  ·  Streaming region")

	body := lipgloss.JoinVertical(lipgloss.Left,
		styleSubtle.Render("Which country's streaming catalog should \"where to watch\""),
		styleSubtle.Render("filters use? Two-letter code, e.g. US, IN, GB, CA."),
		"",
		m.regionInput.View(),
	)
	panel := stylePanel.Render(body)

	note := styleSubtle.Render("Stored locally at ~/.whatnext/config.json — change it later from the home menu.")
	foot := m.footer(
		[2]string{"↵", "save & continue"},
		[2]string{"esc", "use " + store.DefaultRegion},
		[2]string{"ctrl+c", "quit"},
	)
	return lipgloss.JoinVertical(lipgloss.Left, welcome, "", step, "", panel, "", note, "", foot)
}

// ---- region (post-onboarding, from the home menu) ----

// updateRegion handles the standalone "Region" home-menu screen: same field
// as onboarding, but saving here returns to home and — if the region
// actually changed and a TMDb key is available — immediately kicks off a
// refresh so cached watch-provider data catches up to the new region.
func (m model) updateRegion(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.regionInput.Blur()
		m.goTo(scrHome)
		return m, nil
	case "enter":
		r := strings.ToUpper(strings.TrimSpace(m.regionInput.Value()))
		if r == "" {
			r = store.DefaultRegion
		}
		changed := r != m.region
		m.saveRegion(r)
		m.regionInput.Blur()

		if !changed {
			m.flash = "Region already set to " + r + "."
			m.goTo(scrHome)
			return m, nil
		}
		if m.tmdb == nil {
			m.flash = "Region set to " + r + ". Add a TMDb key, then Refresh to update streaming data."
			m.goTo(scrHome)
			return m, nil
		}
		return m.startRefresh()
	}
	var cmd tea.Cmd
	m.regionInput, cmd = m.regionInput.Update(msg)
	return m, cmd
}

func (m model) viewRegion() string {
	title := gradientText("Streaming region", float64(m.frame)/40.0, true)

	body := lipgloss.JoinVertical(lipgloss.Left,
		styleSubtle.Render("Two-letter country code for \"where to watch\" filters,"),
		styleSubtle.Render("e.g. US, IN, GB, CA. Changing it refreshes cached streaming data."),
		"",
		m.regionInput.View(),
	)
	panel := stylePanel.Render(body)

	current := styleSubtle.Render("currently: ") + styleAccent.Render(m.region)

	foot := m.footer(
		[2]string{"↵", "save"},
		[2]string{"esc", "cancel"},
	)
	return lipgloss.JoinVertical(lipgloss.Left, title, "", current, "", panel, "", foot)
}

// ---- step 3: Letterboxd watchlist ----

func (m model) updateOnboardPath(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		p := expandHome(strings.TrimSpace(m.pathInput.Value()))
		if p == "" {
			// Nothing to import; finish onboarding.
			m.completeOnboarding()
			m.flash = "Welcome! Import a Letterboxd export anytime from the menu."
			m.goTo(scrHome)
			return m, nil
		}
		m.importErr = ""
		m.completeOnboarding()
		return m, parseCmd(p) // afterParse → progress → home
	case "esc":
		m.completeOnboarding()
		m.flash = "Welcome! Import a Letterboxd export anytime from the menu."
		m.goTo(scrHome)
		return m, nil
	}
	var cmd tea.Cmd
	m.pathInput, cmd = m.pathInput.Update(msg)
	return m, cmd
}

// completeOnboarding ensures config.json exists (so first-run won't fire again)
// and drops the onboarding flag.
func (m *model) completeOnboarding() {
	if !store.ConfigExists() {
		cfg, _ := store.LoadConfig()
		_ = cfg.Save()
	}
	m.pathInput.Blur()
	m.onboarding = false
}

func (m model) viewOnboardPath() string {
	welcome := renderBanner(float64(m.frame) / 40.0)
	step := styleAccent.Render("Step 3 of 3") + styleSubtle.Render("  ·  Letterboxd watchlist")

	keyState := styleGood.Render("✓ API key saved")
	if m.tmdb == nil {
		keyState = styleWarn.Render("• no key set — you can add one later (enrichment off)")
	}

	body := lipgloss.JoinVertical(lipgloss.Left,
		styleSubtle.Render("Export your data: Letterboxd → Settings → Import & Export →"),
		styleSubtle.Render("Export Data. Point me at the .zip (or an unzipped watchlist.csv)."),
		"",
		m.pathInput.View(),
	)
	panel := stylePanel.Render(body)

	var errLine string
	if m.importErr != "" {
		errLine = styleBad.Render("✗ " + m.importErr)
	}

	foot := m.footer(
		[2]string{"↵", "import"},
		[2]string{"esc", "skip — do it later"},
		[2]string{"ctrl+c", "quit"},
	)
	blocks := []string{welcome, "", step, "", keyState, "", panel, ""}
	if errLine != "" {
		blocks = append(blocks, errLine, "")
	}
	blocks = append(blocks, foot)
	return lipgloss.JoinVertical(lipgloss.Left, blocks...)
}
