package ui

import (
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
	"strings"

	"github.com/jarvis1704/whatnext/internal/store"
)

// openURL launches u in the user's default browser: xdg-open on Linux,
// open on macOS, the shell's start on Windows. Start (not Run) so the
// TUI never blocks waiting on the browser process.
func openURL(u string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", u)
	default:
		cmd = exec.Command("xdg-open", u)
	}
	return cmd.Start()
}

// letterboxdURL returns t's Letterboxd page, or "" if the pool entry has no
// real URI — titles imported from a URI-less CSV row get a synthetic
// "synthetic:name:year" key (see internal/letterboxd) that must never be
// handed to a browser.
func letterboxdURL(t store.Title) string {
	if strings.HasPrefix(t.LetterboxdURI, "http://") || strings.HasPrefix(t.LetterboxdURI, "https://") {
		return t.LetterboxdURI
	}
	return ""
}

// tmdbURL returns t's public TMDb page, or "" if it hasn't been enriched
// with a TMDb ID yet.
func tmdbURL(t store.Title) string {
	if !t.Enriched || t.TMDbID == 0 {
		return ""
	}
	kind := "movie"
	if t.MediaType == "tv" {
		kind = "tv"
	}
	return fmt.Sprintf("https://www.themoviedb.org/%s/%d", kind, t.TMDbID)
}

// youtubeTrailerURL builds a YouTube search for the title's trailer. A
// search link (rather than the actual trailer video) is deliberate: finding
// the real trailer ID needs another TMDb API call we don't otherwise make.
func youtubeTrailerURL(t store.Title) string {
	q := t.Name
	if t.Year != "" {
		q += " " + t.Year
	}
	q += " trailer"
	return "https://www.youtube.com/results?search_query=" + url.QueryEscape(q)
}

// openTarget opens u (if non-empty) and returns a one-line status describing
// what happened, for a flash/status line.
func openTarget(label, u string) string {
	if u == "" {
		return "No " + label + " link for this title yet."
	}
	if err := openURL(u); err != nil {
		return "Couldn't open " + label + ": " + err.Error()
	}
	return "Opened " + label + " in your browser."
}

// resolveOpenTarget maps an "o"-menu key (l/t/y) to a title's link. ok is
// false for any other key, so callers can treat it as "cancel".
func resolveOpenTarget(t store.Title, key string) (label, u string, ok bool) {
	switch key {
	case "l":
		return "Letterboxd", letterboxdURL(t), true
	case "t":
		return "TMDb", tmdbURL(t), true
	case "y":
		return "YouTube trailer", youtubeTrailerURL(t), true
	default:
		return "", "", false
	}
}

// openMenuBox renders the inline "open: l letterboxd  t tmdb  y trailer"
// picker shared by the reveal and browse screens.
func openMenuBox() string {
	line := styleSubtle.Render("open  ") +
		styleAccent.Render("l") + styleSubtle.Render(" letterboxd    ") +
		styleAccent.Render("t") + styleSubtle.Render(" tmdb    ") +
		styleAccent.Render("y") + styleSubtle.Render(" trailer    ") +
		styleAccent.Render("esc") + styleSubtle.Render(" cancel")
	return stylePanel.Render(line)
}
