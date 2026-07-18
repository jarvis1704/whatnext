# `whatnext` — CLI Plan

A terminal tool that picks what you should watch next from your Letterboxd
watchlist, filtered by genre or mood. Built in Go, no external Go
dependencies (standard library only), talks to the TMDb API for
enrichment.

---

## 1. Goal & Philosophy

Letterboxd exports your watchlist as a bare CSV: just title, year, and a
URL. No genre, no runtime, no way to filter. `whatnext` closes that gap:

1. Import your Letterboxd export once.
2. Enrich each title with genre/runtime/type via TMDb (cached locally, so
   this only happens once per title).
3. Ask for a pick — optionally filtered by genre, mood, media type, or max
   runtime — and get a random suggestion from what's left.

Kept deliberately small: no database, no server, no config beyond two
JSON files. Everything lives in `~/.whatnext/`.

---

## 2. Tech Stack

- **Language:** Go 1.22 (standard library only — `encoding/csv`,
  `encoding/json`, `archive/zip`, `net/http`, `flag`). No `cobra`, no
  third-party modules. This keeps `go build` working offline with zero
  `go.sum` surprises.
- **External API:** [TMDb](https://www.themoviedb.org/documentation/api)
  — free API key, used only for enrichment (genre/runtime/movie-vs-tv).
- **Storage:** two flat JSON files under `~/.whatnext/`:
  - `data.json` — your enriched watchlist pool
  - `moods.json` — editable mood → genre mapping

No SQLite, no cloud sync. It's a personal CLI; flat files are enough and
stay human-inspectable.

---

## 3. Project Structure

```
whatnext/
├── go.mod                          # module whatnext
├── main.go                         # CLI entrypoint & subcommand dispatch
└── internal/
    ├── store/
    │   └── store.go                # Title struct, JSON load/save, Upsert
    ├── letterboxd/
    │   └── parse.go                # parses export .zip or raw watchlist.csv
    ├── tmdb/
    │   └── client.go                # TMDb search + details lookup
    ├── mood/
    │   └── mood.go                 # mood→genre config, defaults + persistence
    └── pick/
        └── pick.go                 # filter matching + random selection
```

`internal/` is used deliberately — nothing here needs to be importable by
outside packages, and it keeps `go doc whatnext` clean.

---

## 4. Data Model

```go
type Title struct {
    Name          string   // "Oldboy"
    Year          string   // "2003"
    LetterboxdURI string   // unique key — used to dedupe on import
    TMDbID        int      // set after enrichment
    MediaType     string   // "movie" | "tv"
    Genres        []string // ["Thriller", "Mystery", ...]
    RuntimeMins   int
    Enriched      bool     // false until TMDb lookup succeeds
    Watched       bool     // true once you mark it done
}
```

Stored as `{"titles": [...]}` in `data.json`. `LetterboxdURI` is the
dedupe key on import, so re-importing an updated export is always safe —
existing entries (and their `watched` flag / enrichment) are never
clobbered, only new titles are appended.

---

## 5. Package Responsibilities

### `internal/store`
Pure persistence layer. `Load()` returns an empty `Data{}` if no file
exists yet (first run). `Save()` writes pretty-printed JSON. `Upsert()`
appends only if the `LetterboxdURI` isn't already present — this is what
makes re-import idempotent.

### `internal/letterboxd`
`ParseWatchlist(path)` accepts either:
- the full export `.zip` you download from Letterboxd (Settings → Import
  & Export → Export Data) — it finds `watchlist.csv` inside automatically
- or a raw `watchlist.csv` if you've already unzipped it

CSV columns are matched by header name (`Name`, `Year`, `Letterboxd URI`)
rather than fixed position, since Letterboxd has changed column order
before.

### `internal/tmdb`
Thin HTTP client, no SDK. `Lookup(name, year)`:
1. Searches `/search/movie` with `primary_release_year` filter.
2. Falls back to unfiltered movie search if no match.
3. Falls back to `/search/tv` (with and without year).
4. Fetches `/movie/{id}` or `/tv/{id}` for genres + runtime.

Returns `ok=false` if nothing matched (title stays `Enriched: false` and
is retried on the next `refresh`).

### `internal/mood`
Ships sane defaults (cozy, intense, mindbending, sad, scary, epic,
laugh), each mapped to a list of TMDb genre names. Written to
`~/.whatnext/moods.json` on first run — after that, **the file is the
source of truth**, so you can freely add moods like `"rainy-day":
["Drama", "Romance"]` by hand.

### `internal/pick`
Pure filtering logic, decoupled from I/O so it's trivially testable:
- `Candidates(titles, Filter) []Title` — applies genre / mood / media
  type / max-runtime filters, always excludes `Watched: true`
- `Random(candidates) (Title, bool)` — uniform random pick, `ok=false` on
  empty pool

---

## 6. CLI Commands

All via `flag`-based subcommands in `main.go` (`os.Args[1]` dispatch —
no framework needed for 5 commands):

| Command | Purpose |
|---|---|
| `whatnext import <path>` | Parse a Letterboxd export (.zip or .csv), enrich new titles via TMDb, save to pool |
| `whatnext pick [--genre X] [--mood Y] [--type movie\|tv] [--max-runtime N]` | Random filtered pick from unwatched pool |
| `whatnext list [--watched]` | Print the pool (or just watched items) as a table |
| `whatnext watched "<title>"` | Mark a title watched by fuzzy name match; if ambiguous, list matches and ask to be more specific |
| `whatnext refresh` | Re-run TMDb enrichment for any titles where `Enriched == false` |

**Example session:**
```
$ whatnext import ~/Downloads/letterboxd-export.zip
Imported 214 new titles, enriching via TMDb...
✓ 214 enriched, 3 not found (will retry on next refresh)

$ whatnext pick --mood cozy --max-runtime 120
🎬 Paddington 2 (2017) — Comedy, Family, Adventure — 104 min

$ whatnext watched "Paddington 2"
✓ Marked watched.
```

---

## 7. Setup Requirements

1. **TMDb API key** — free, sign up at themoviedb.org → Settings → API →
   request a "Developer" key (v3 auth). Set as an environment variable:
   ```
   export TMDB_API_KEY="your-key-here"
   ```
   (add to `.zshrc`/`.bashrc`/Omarchy shell config for persistence)
2. **Letterboxd export** — Letterboxd.com → Settings → Import & Export →
   Export Data → download the zip. Re-export and re-`import` any time you
   add to your watchlist; it'll only add what's new.

---

## 8. Edge Cases to Handle

- **No TMDb match found** — keep the title in the pool as
  `Enriched: false`, excluded from genre/mood filters (but still visible
  via `list` and pickable if no filters are applied). Retry via
  `refresh`.
- **Ambiguous `watched` match** (e.g. multiple "It" entries) — list
  candidates with year, ask user to include the year to disambiguate.
- **Empty filtered pool** — `pick` should say so clearly ("No unwatched
  titles match those filters") rather than erroring, and suggest loosening
  filters.
- **Re-import safety** — never overwrite existing enrichment/watched
  status on already-known titles (handled by `Upsert`'s dedupe-by-URI).
- **Rate limiting** — TMDb's free tier is generous, but `import`/`refresh`
  should add a small delay (~250ms) between lookups to be a good citizen.

---

## 9. Build & Run

```bash
cd whatnext
go build -o whatnext .
./whatnext import ~/Downloads/letterboxd-export.zip
./whatnext pick --mood intense
```

No `go.sum` to worry about since there are zero external dependencies —
`go build` works completely offline.

---

## 10. Possible Future Enhancements

- `whatnext stats` — genre breakdown of your watchlist, average runtime, etc.
- Weighted picking (favor titles that have sat unwatched longest) as a
  `--weighted` flag on `pick`
- `whatnext undo` to unmark the last `watched` action
- Shell completion for `--genre`/`--mood` values based on what's actually
  in your pool

---

*Note: `store`, `letterboxd`, `tmdb`, `mood`, and `pick` packages were
already scaffolded in an earlier pass — only `main.go` (command wiring)
is left to write.*
