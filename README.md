# whatnext

Pick what to watch next from your Letterboxd watchlist — filtered by genre,
mood, media type, or runtime. A fast, animated terminal app written in Go.

Run it with **no arguments** for the full-screen interactive TUI, or use the
classic subcommands for scripting.

## Install

**Download a binary** — grab the archive for your OS/arch from the
[latest release](https://github.com/jarvis1704/whatnext/releases/latest),
extract it, and put `whatnext` somewhere on your `PATH`:

```bash
# example: Linux/amd64 — swap linux_amd64 for darwin_amd64, darwin_arm64,
# linux_arm64, or windows_amd64 (as a .zip) as needed
curl -sSL https://github.com/jarvis1704/whatnext/releases/latest/download/whatnext_linux_amd64.tar.gz \
  | tar -xz whatnext
sudo mv whatnext /usr/local/bin/
```

**With Go installed:**

```bash
go install github.com/jarvis1704/whatnext@latest
```

**From source:**

```bash
git clone https://github.com/jarvis1704/whatnext.git
cd whatnext
go build -o whatnext .
```

Requires Go 1.22+. Dependencies are the [Charm](https://charm.sh) TUI stack
(Bubble Tea / Lip Gloss / Bubbles).

## Setup

1. **TMDb API key** (for genre/runtime enrichment — free v3 key):
   ```bash
   export TMDB_API_KEY="your-key-here"
   ```
   Without it, import and pick still work; enrichment is simply skipped.

2. **Letterboxd export**: Letterboxd → Settings → Import & Export → Export Data.
   Import the `.zip` directly — no need to unzip.

## Interactive TUI

```bash
./whatnext
```

- **🎲 Pick something** — tune mood / type / runtime / streaming provider,
  then a slot-machine reveal lands on a random unwatched title.
- **🗂 Browse pool** — scroll your watchlist, `/` to live-search by name,
  toggle watched with `w`.
- **📥 Import export** — point it at a `.zip` or `watchlist.csv`.
- **✨ Refresh enrichment** — retry TMDb lookups for unenriched titles, with a
  live progress bar.
- **📊 Stats** — genre breakdown, decade histogram, avg runtime.
- **🌍 Region** — set the country used for "where to watch" lookups.

Keys: arrows / `j`/`k` to move, `↵` to select, `esc` to go back, `q` to quit.

## Subcommands

```bash
./whatnext import ~/Downloads/letterboxd-export.zip
./whatnext pick --mood intense --max-runtime 120
./whatnext pick --genre Thriller --type movie
./whatnext pick --on Netflix
./whatnext list [--watched] [--on <provider>]
./whatnext watched "Paddington 2"
./whatnext stats
./whatnext region [<code>]
./whatnext refresh
./whatnext doctor
```

### `doctor`

One-shot health check — run it first if enrichment misbehaves:

```
whatnext doctor

  ✓ API key      set (32 chars)
  ✓ DNS          api.themoviedb.org → 3.175.86.67
  ✓ TMDb API     reachable, key valid (142ms)
  ✓ Pool         214 titles · 30 watched · 3 unenriched  ~/.whatnext
```

It flags a missing/rejected key, and detects **ISP DNS-hijack block pages**
(common in India — TMDb resolves to a `49.44.x.x` block IP). Fix: point your
resolver at `1.1.1.1` / `8.8.8.8`. Exits non-zero on any hard failure, so it's
safe to gate scripts on.

## Storage

Everything lives in `~/.whatnext/`:

- `data.json` — your enriched watchlist pool
- `moods.json` — editable mood → genre mapping (add your own, e.g.
  `"rainy-day": ["Drama", "Romance"]`)

Re-importing an updated export is always safe: titles dedupe by Letterboxd URI,
so existing enrichment and watched flags are never clobbered.
