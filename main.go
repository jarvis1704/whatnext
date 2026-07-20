// Command whatnext picks what to watch next from your Letterboxd watchlist.
//
// Run it with no arguments for the full-screen interactive TUI, or use the
// classic subcommands (import / pick / list / watched / refresh) for scripting.
package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"strings"

	"whatnext/internal/enrich"
	"whatnext/internal/letterboxd"
	"whatnext/internal/mood"
	"whatnext/internal/pick"
	"whatnext/internal/stats"
	"whatnext/internal/store"
	"whatnext/internal/tmdb"
	"whatnext/internal/ui"
)

// tiny ANSI helpers so CLI output has a bit of life without a TUI dependency.
const (
	aReset  = "\033[0m"
	aBold   = "\033[1m"
	aDim    = "\033[2m"
	aGold   = "\033[38;5;220m"
	aGreen  = "\033[38;5;114m"
	aRed    = "\033[38;5;210m"
	aViolet = "\033[38;5;141m"
	aAmber  = "\033[38;5;215m"
)

func c(color, s string) string { return color + s + aReset }

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		// No subcommand → launch the interactive TUI.
		if err := ui.Run(); err != nil {
			fail(err)
		}
		return
	}

	switch args[0] {
	case "import":
		cmdImport(args[1:])
	case "pick":
		cmdPick(args[1:])
	case "list":
		cmdList(args[1:])
	case "watched":
		cmdWatched(args[1:])
	case "stats":
		cmdStats(args[1:])
	case "refresh":
		cmdRefresh(args[1:])
	case "doctor":
		cmdDoctor(args[1:])
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", args[0])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Printf(`%s — pick your next watch

%s
  whatnext                          launch the interactive TUI
  whatnext import <path>            import a Letterboxd .zip or watchlist.csv
  whatnext pick [flags]             random pick from your unwatched pool
  whatnext list [--watched] [--on <provider>]   print the pool as a table
  whatnext watched "<title>"        mark a title watched (fuzzy match)
  whatnext stats                    genre/decade/runtime breakdown of your pool
  whatnext refresh                  retry TMDb enrichment for unenriched titles
  whatnext doctor                   check key, DNS, TMDb reachability & pool

%s
  --genre <name>        filter to a single genre
  --mood <name>         filter by mood (cozy, intense, mindbending, …)
  --type <movie|tv>     filter by media type
  --max-runtime <min>   cap runtime in minutes
  --on <provider>       filter to a streaming provider, e.g. Netflix

%s
  TMDB_API_KEY          free TMDb v3 key, required for enrichment
  WHATNEXT_REGION       ISO country code for watch-provider data (default %s)
`,
		c(aBold+aGold, "whatnext"),
		c(aViolet, "USAGE"),
		c(aViolet, "PICK FLAGS"),
		c(aViolet, "ENVIRONMENT"),
		store.DefaultRegion,
	)
}

// ---- import ----

func cmdImport(args []string) {
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	fs.Parse(args)
	if fs.NArg() < 1 {
		fail(fmt.Errorf("usage: whatnext import <path-to-export.zip|watchlist.csv>"))
	}
	path := fs.Arg(0)

	titles, err := letterboxd.ParseWatchlist(path)
	if err != nil {
		fail(err)
	}
	data, err := store.Load()
	if err != nil {
		fail(err)
	}
	added := 0
	for _, t := range titles {
		if data.Upsert(t) {
			added++
		}
	}
	if err := data.Save(); err != nil {
		fail(err)
	}
	fmt.Printf("Imported %s new titles (%s in pool).\n",
		c(aBold+aGold, fmt.Sprintf("%d", added)),
		c(aDim, fmt.Sprintf("%d", len(data.Titles))))

	runEnrichment(data, "enriching")
}

// ---- refresh ----

func cmdRefresh(args []string) {
	data, err := store.Load()
	if err != nil {
		fail(err)
	}
	if enrich.CountPending(data, store.ResolveRegion()) == 0 {
		fmt.Println(c(aGreen, "✓ Everything is already enriched."))
		return
	}
	runEnrichment(data, "refreshing")
}

// runEnrichment enriches the pool with a live single-line progress readout.
func runEnrichment(data *store.Data, verb string) {
	client, err := tmdb.New(store.ResolveKey())
	if err != nil {
		fmt.Println(c(aAmber, "⚠ No TMDb API key — skipping enrichment."))
		fmt.Println(c(aDim, "  Run whatnext once for onboarding, or export TMDB_API_KEY, then: whatnext refresh"))
		return
	}
	region := store.ResolveRegion()
	total := enrich.CountPending(data, region)
	if total == 0 {
		return
	}
	fmt.Printf("%s %d titles via TMDb…\n", capitalize(verb), total)

	var found, missed, errs int
	enrich.Run(data, client, region, func(p enrich.Progress) {
		switch {
		case p.Err != nil:
			errs++
		case p.Found:
			found++
		default:
			missed++
		}
		// carriage-return progress line
		fmt.Printf("\r\033[K%s %d/%d  %s %s %s  %s",
			spinnerFrame(p.Done),
			p.Done, p.Total,
			c(aGreen, fmt.Sprintf("✓%d", found)),
			c(aAmber, fmt.Sprintf("?%d", missed)),
			c(aRed, fmt.Sprintf("✗%d", errs)),
			c(aDim, truncateStr(p.Title.Name, 30)),
		)
	})
	fmt.Print("\r\033[K")
	if err := data.Save(); err != nil {
		fail(err)
	}
	fmt.Printf("%s %d enriched, %d not found, %d errors\n",
		c(aGreen, "✓"), found, missed, errs)
}

var spin = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func spinnerFrame(i int) string { return c(aGold, spin[i%len(spin)]) }

// ---- doctor ----

const tmdbHost = "api.themoviedb.org"

// cmdDoctor runs a one-shot health check: API key, DNS, TMDb reachability, and
// pool stats. Exits non-zero if a hard problem (bad/missing key, unreachable
// API) is found, so it can gate scripts.
func cmdDoctor(_ []string) {
	fmt.Println(c(aBold+aGold, "whatnext doctor"))
	fmt.Println()
	ok := true

	// 1. API key presence (never print the value).
	key := store.ResolveKey()
	if key == "" {
		checkln(false, "API key", "no key (run onboarding or export TMDB_API_KEY)")
		ok = false
	} else {
		src := "config"
		if os.Getenv("TMDB_API_KEY") != "" {
			src = "env"
		}
		checkln(true, "API key", fmt.Sprintf("set (%d chars, from %s)", len(key), src))
	}

	// 2. DNS — resolve the API host and flag ISP block-page hijacking.
	ips, derr := net.LookupHost(tmdbHost)
	switch {
	case derr != nil:
		checkln(false, "DNS", "lookup failed: "+derr.Error())
		ok = false
	case isBlockPage(ips):
		warnln("DNS", fmt.Sprintf("%s → %s (looks like an ISP block page)", tmdbHost, strings.Join(ips, ", ")))
		fmt.Println(c(aDim, "            switch resolver to 1.1.1.1 / 8.8.8.8 — see README"))
		ok = false
	default:
		checkln(true, "DNS", fmt.Sprintf("%s → %s", tmdbHost, ips[0]))
	}

	// 3. TMDb reachability + key validity.
	if key != "" {
		client, _ := tmdb.New(key)
		lat, herr := client.Health()
		switch {
		case herr == nil:
			checkln(true, "TMDb API", fmt.Sprintf("reachable, key valid (%dms)", lat.Milliseconds()))
		case herr == tmdb.ErrBadKey:
			checkln(false, "TMDb API", "reachable but key rejected (401) — wrong key")
			ok = false
		default:
			checkln(false, "TMDb API", "unreachable: "+shortDoctorErr(herr))
			ok = false
		}
	} else {
		fmt.Printf("  %s %-12s %s\n", c(aDim, "–"), "TMDb API", c(aDim, "skipped (no key)"))
	}

	// 4. Watch-provider region.
	region := store.ResolveRegion()
	checkln(true, "Region", fmt.Sprintf("%s (watch-provider lookups)", region))

	// 5. Pool stats (always informational).
	if data, err := store.Load(); err == nil {
		total, watched, unenriched := 0, 0, 0
		for _, t := range data.Titles {
			total++
			if t.Watched {
				watched++
			}
			if !t.Enriched {
				unenriched++
			}
		}
		pending := enrich.CountPending(data, region)
		home, _ := os.UserHomeDir()
		checkln(true, "Pool", fmt.Sprintf("%d titles · %d watched · %d unenriched · %d need TMDb work  %s",
			total, watched, unenriched, pending, c(aDim, home+"/.whatnext")))
	} else {
		warnln("Pool", "could not read pool: "+err.Error())
	}

	fmt.Println()
	if ok {
		fmt.Println(c(aGreen, "✓ all good — run: whatnext refresh"))
		return
	}
	fmt.Println(c(aAmber, "⚠ issues above — fix, then re-run: whatnext doctor"))
	os.Exit(1)
}

func checkln(ok bool, label, detail string) {
	mark := c(aGreen, "✓")
	if !ok {
		mark = c(aRed, "✗")
	}
	fmt.Printf("  %s %-12s %s\n", mark, label, detail)
}

func warnln(label, detail string) {
	fmt.Printf("  %s %-12s %s\n", c(aAmber, "⚠"), label, c(aAmber, detail))
}

// isBlockPage reports whether the resolved IPs look like an ISP DNS-hijack
// block page rather than TMDb's real CloudFront addresses.
func isBlockPage(ips []string) bool {
	for _, ip := range ips {
		// Real TMDb is CloudFront (3.x / 13.x / 18.x …). Indian ISP block
		// pages land on 49.44.x.x and similar single-host ranges.
		if strings.HasPrefix(ip, "49.44.") || strings.HasPrefix(ip, "49.45.") {
			return true
		}
	}
	return false
}

func shortDoctorErr(err error) string {
	s := err.Error()
	if strings.Contains(s, "timeout") || strings.Contains(s, "deadline") {
		return "connection timed out (DNS block or offline?)"
	}
	if len(s) > 60 {
		s = s[:59] + "…"
	}
	return s
}

// ---- pick ----

func cmdPick(args []string) {
	fs := flag.NewFlagSet("pick", flag.ExitOnError)
	genre := fs.String("genre", "", "single genre filter")
	moodName := fs.String("mood", "", "mood filter")
	mediaType := fs.String("type", "", "media type: movie|tv")
	maxRuntime := fs.Int("max-runtime", 0, "max runtime in minutes")
	provider := fs.String("on", "", "streaming provider filter, e.g. Netflix")
	fs.Parse(args)

	data, err := store.Load()
	if err != nil {
		fail(err)
	}
	f := pick.Filter{Genre: *genre, MediaType: *mediaType, MaxRuntime: *maxRuntime, Provider: *provider}
	if *moodName != "" {
		cfg, err := mood.Load()
		if err != nil {
			fail(err)
		}
		g, ok := cfg.Genres(*moodName)
		if !ok {
			fmt.Fprintf(os.Stderr, "unknown mood %q. known: %s\n", *moodName, strings.Join(cfg.Names(), ", "))
			os.Exit(2)
		}
		f.MoodGenres = g
	}

	cands := pick.Candidates(data.Titles, f)
	t, ok := pick.Random(cands)
	if !ok {
		fmt.Println(c(aAmber, "No unwatched titles match those filters."))
		fmt.Println(c(aDim, "Try loosening a filter, or import more titles."))
		os.Exit(1)
	}

	meta := []string{}
	if len(t.Genres) > 0 {
		meta = append(meta, strings.Join(t.Genres, ", "))
	}
	if t.RuntimeMins > 0 {
		meta = append(meta, fmt.Sprintf("%d min", t.RuntimeMins))
	}
	if len(t.Providers) > 0 {
		meta = append(meta, "streaming on "+strings.Join(t.Providers, ", "))
	}
	line := fmt.Sprintf("🎬 %s", c(aBold+aGold, t.Name))
	if t.Year != "" {
		line += c(aDim, " ("+t.Year+")")
	}
	if len(meta) > 0 {
		line += c(aViolet, " — "+strings.Join(meta, " — "))
	}
	fmt.Println(line)
}

// ---- list ----

func cmdList(args []string) {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	watchedOnly := fs.Bool("watched", false, "show only watched titles")
	provider := fs.String("on", "", "show only titles streaming on this provider")
	fs.Parse(args)

	data, err := store.Load()
	if err != nil {
		fail(err)
	}
	shown := 0
	for _, t := range data.Titles {
		if *watchedOnly && !t.Watched {
			continue
		}
		if *provider != "" && !hasProviderCI(t.Providers, *provider) {
			continue
		}
		status := c(aDim, "○")
		if t.Watched {
			status = c(aGreen, "✓")
		} else if !t.Enriched {
			status = c(aAmber, "…")
		}
		name := t.Name
		if t.Year != "" {
			name += " (" + t.Year + ")"
		}
		meta := ""
		if t.Enriched && len(t.Genres) > 0 {
			meta = c(aDim, "  "+strings.Join(t.Genres, ", "))
			if t.RuntimeMins > 0 {
				meta += c(aDim, fmt.Sprintf(" · %dm", t.RuntimeMins))
			}
		}
		if len(t.Providers) > 0 {
			meta += c(aCyanBar(), "  "+strings.Join(t.Providers, ", "))
		}
		fmt.Printf("%s %-42s%s\n", status, name, meta)
		shown++
	}
	if shown == 0 {
		fmt.Println(c(aDim, "Nothing to show. Run: whatnext import <path>"))
	}
}

func hasProviderCI(providers []string, want string) bool {
	for _, p := range providers {
		if strings.EqualFold(p, want) {
			return true
		}
	}
	return false
}

// ---- stats ----

func cmdStats(_ []string) {
	data, err := store.Load()
	if err != nil {
		fail(err)
	}
	s := stats.Compute(data.Titles)

	if s.Total == 0 {
		fmt.Println(c(aDim, "Nothing to show. Run: whatnext import <path>"))
		return
	}

	fmt.Println(c(aBold+aGold, "whatnext stats"))
	fmt.Println()

	pct := 0
	if s.Total > 0 {
		pct = s.Watched * 100 / s.Total
	}
	fmt.Printf("  %-16s %d titles · %s watched (%d%%) · %s unwatched\n",
		"Pool", s.Total, c(aGreen, fmt.Sprintf("%d", s.Watched)), pct, c(aAmber, fmt.Sprintf("%d", s.Unwatched)))
	if s.Unenriched > 0 {
		fmt.Printf("  %-16s %s\n", "", c(aDim, fmt.Sprintf("%d not yet enriched", s.Unenriched)))
	}
	fmt.Printf("  %-16s %d movies · %d tv\n", "Media", s.MovieCount, s.TVCount)
	if s.RuntimeSamples > 0 {
		fmt.Printf("  %-16s %d min avg  %s\n", "Runtime", s.AvgRuntimeMins, c(aDim, fmt.Sprintf("(%d titles)", s.RuntimeSamples)))
	}

	if len(s.Genres) > 0 {
		fmt.Println()
		fmt.Println(c(aViolet, "GENRES"))
		printBars(genreBarLabels(s.Genres), genreBarCounts(s.Genres), aCyanBar())
	}

	if len(s.Decades) > 0 {
		fmt.Println()
		fmt.Println(c(aViolet, "DECADES"))
		labels := make([]string, len(s.Decades))
		counts := make([]int, len(s.Decades))
		for i, d := range s.Decades {
			labels[i] = fmt.Sprintf("%ds", d.Decade)
			counts[i] = d.Count
		}
		printBars(labels, counts, aGold)
	}
}

func genreBarLabels(gs []stats.GenreCount) []string {
	out := make([]string, len(gs))
	for i, g := range gs {
		out[i] = g.Genre
	}
	return out
}

func genreBarCounts(gs []stats.GenreCount) []int {
	out := make([]int, len(gs))
	for i, g := range gs {
		out[i] = g.Count
	}
	return out
}

func aCyanBar() string { return "\033[38;5;80m" }

// printBars renders a horizontal ASCII bar chart, scaled so the largest count
// fills barWidth cells.
func printBars(labels []string, counts []int, color string) {
	const barWidth = 30
	max := 0
	labelWidth := 0
	for i, n := range counts {
		if n > max {
			max = n
		}
		if len(labels[i]) > labelWidth {
			labelWidth = len(labels[i])
		}
	}
	if max == 0 {
		return
	}
	for i, n := range counts {
		filled := n * barWidth / max
		if filled == 0 && n > 0 {
			filled = 1
		}
		bar := strings.Repeat("█", filled)
		fmt.Printf("  %-*s %s %s\n", labelWidth, labels[i], c(color, bar), c(aDim, fmt.Sprintf("%d", n)))
	}
}

// ---- watched ----

func cmdWatched(args []string) {
	if len(args) < 1 {
		fail(fmt.Errorf("usage: whatnext watched \"<title>\""))
	}
	query := strings.Join(args, " ")

	data, err := store.Load()
	if err != nil {
		fail(err)
	}
	matches := pick.MatchByName(data.Titles, query)
	switch {
	case len(matches) == 0:
		fmt.Printf("No title matches %q.\n", query)
		os.Exit(1)
	case len(matches) > 1:
		fmt.Println(c(aAmber, "Multiple matches — be more specific (add the year):"))
		for _, mt := range matches {
			fmt.Printf("  · %s (%s)\n", mt.Title.Name, mt.Title.Year)
		}
		os.Exit(1)
	}
	idx := matches[0].Index
	data.Titles[idx].Watched = true
	if err := data.Save(); err != nil {
		fail(err)
	}
	fmt.Printf("%s Marked watched: %s\n", c(aGreen, "✓"), c(aBold, data.Titles[idx].Name))
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, c(aRed, "error: ")+err.Error())
	os.Exit(1)
}

func capitalize(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func truncateStr(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
