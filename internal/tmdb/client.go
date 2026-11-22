// Package tmdb is a thin standard-library HTTP client for the TMDb v3 API,
// used to enrich watchlist titles with genre, runtime, media type, rating,
// director/creator, top cast, language, and production country.
package tmdb

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// DefaultBaseURL is the public TMDb v3 API root.
const DefaultBaseURL = "https://api.themoviedb.org/3"

// ErrNoKey is returned when TMDB_API_KEY is unset.
var ErrNoKey = fmt.Errorf("TMDB_API_KEY is not set")

// Client talks to TMDb using a v3 API key.
type Client struct {
	key   string
	base  string // API root; DefaultBaseURL outside tests
	http  *http.Client
	edges *edgeStats // per-address reliability, shared by the dialer and fetch
}

// New returns a Client, or ErrNoKey if key is empty.
func New(key string) (*Client, error) {
	if strings.TrimSpace(key) == "" {
		return nil, ErrNoKey
	}
	edges := newEdgeStats()
	return &Client{
		key:   key,
		base:  DefaultBaseURL,
		http:  newHTTPClient(edges),
		edges: edges,
	}, nil
}

// ErrBadKey signals the API rejected the key (HTTP 401).
var ErrBadKey = fmt.Errorf("TMDb rejected the API key (401)")

// Health verifies the key and reachability by hitting /configuration. It
// returns the round-trip latency, ErrBadKey on 401, or the transport error.
func (c *Client) Health() (time.Duration, error) {
	q := url.Values{"api_key": {c.key}}
	start := time.Now()
	status, _, err := c.fetch("/configuration", c.base+"/configuration?"+q.Encode())
	lat := time.Since(start)
	if err != nil {
		return 0, err
	}
	switch status {
	case http.StatusOK:
		return lat, nil
	case http.StatusUnauthorized:
		return lat, ErrBadKey
	default:
		return lat, fmt.Errorf("unexpected status %d", status)
	}
}

// Result is the enrichment payload for a single title.
type Result struct {
	TMDbID      int
	MediaType   string // "movie" | "tv"
	Genres      []string
	RuntimeMins int
	VoteAverage float64 // TMDb rating, 0-10
	Popularity  float64 // TMDb popularity score

	Director string   // movie: crew job "Director"; tv: joined "created by" names
	Cast     []string // top-billed cast, in credited order
	Language string   // friendly name of the original_language code
	Country  string   // joined production country names

	// TV-only commitment data; zero for movies.
	Seasons  int    // number_of_seasons
	Episodes int    // number_of_episodes
	TVStatus string // "Returning Series" | "Ended" | "Canceled" | "In Production" | …
	TVType   string // "Scripted" | "Miniseries" | "Documentary" | "Reality" | …
}

type searchResp struct {
	Results []struct {
		ID int `json:"id"`
	} `json:"results"`
}

type detailResp struct {
	Genres []struct {
		Name string `json:"name"`
	} `json:"genres"`
	Runtime             int     `json:"runtime"`          // movie
	EpisodeRuntimes     []int   `json:"episode_run_time"` // tv
	VoteAverage         float64 `json:"vote_average"`
	Popularity          float64 `json:"popularity"`
	OriginalLanguage    string  `json:"original_language"`
	ProductionCountries []struct {
		Name string `json:"name"`
	} `json:"production_countries"`
	CreatedBy []struct { // tv only
		Name string `json:"name"`
	} `json:"created_by"`
	NumberOfSeasons  int    `json:"number_of_seasons"`  // tv only
	NumberOfEpisodes int    `json:"number_of_episodes"` // tv only
	Status           string `json:"status"`
	Type             string `json:"type"` // tv only
	LastEpisode      *struct {
		Runtime int `json:"runtime"`
	} `json:"last_episode_to_air"` // tv only
	Season1 *struct {
		Episodes []struct {
			Runtime int `json:"runtime"`
		} `json:"episodes"`
	} `json:"season/1"` // tv only, via append_to_response=season/1
	Credits struct {
		Cast []struct {
			Name string `json:"name"`
		} `json:"cast"`
		Crew []struct {
			Name string `json:"name"`
			Job  string `json:"job"`
		} `json:"crew"`
	} `json:"credits"` // populated via append_to_response=credits
}

// maxCast caps how many top-billed cast members are kept per title.
const maxCast = 4

// Lookup resolves a title to genre/runtime/media-type. It tries a
// year-filtered movie search, then unfiltered movie, then tv (with and
// without year). ok is false when nothing matched — the caller should leave
// the title unenriched and retry later.
func (c *Client) Lookup(name, year string) (Result, bool, error) {
	// 1 & 2: movie search, year-filtered then loose.
	if id, ok, err := c.searchMovie(name, year); err != nil {
		return Result{}, false, err
	} else if ok {
		return c.movieDetails(id)
	}
	// 3: tv search, year-filtered then loose.
	if id, ok, err := c.searchTV(name, year); err != nil {
		return Result{}, false, err
	} else if ok {
		return c.tvDetails(id)
	}
	return Result{}, false, nil
}

func (c *Client) searchMovie(name, year string) (int, bool, error) {
	if year != "" {
		q := url.Values{"query": {name}, "primary_release_year": {year}}
		if id, ok, err := c.firstResult("/search/movie", q); err != nil || ok {
			return id, ok, err
		}
	}
	return c.firstResult("/search/movie", url.Values{"query": {name}})
}

func (c *Client) searchTV(name, year string) (int, bool, error) {
	if year != "" {
		q := url.Values{"query": {name}, "first_air_date_year": {year}}
		if id, ok, err := c.firstResult("/search/tv", q); err != nil || ok {
			return id, ok, err
		}
	}
	return c.firstResult("/search/tv", url.Values{"query": {name}})
}

func (c *Client) firstResult(path string, q url.Values) (int, bool, error) {
	var sr searchResp
	if err := c.get(path, q, &sr); err != nil {
		return 0, false, err
	}
	if len(sr.Results) == 0 {
		return 0, false, nil
	}
	return sr.Results[0].ID, true, nil
}

func (c *Client) movieDetails(id int) (Result, bool, error) {
	var dr detailResp
	if err := c.get(fmt.Sprintf("/movie/%d", id), url.Values{"append_to_response": {"credits"}}, &dr); err != nil {
		return Result{}, false, err
	}
	return Result{
		TMDbID:      id,
		MediaType:   "movie",
		Genres:      genreNames(dr),
		RuntimeMins: dr.Runtime,
		VoteAverage: dr.VoteAverage,
		Popularity:  dr.Popularity,
		Director:    director(dr),
		Cast:        topCast(dr),
		Language:    languageName(dr.OriginalLanguage),
		Country:     countryNames(dr),
	}, true, nil
}

func (c *Client) tvDetails(id int) (Result, bool, error) {
	var dr detailResp
	if err := c.get(fmt.Sprintf("/tv/%d", id), url.Values{"append_to_response": {"credits,season/1"}}, &dr); err != nil {
		return Result{}, false, err
	}
	rt := episodeRuntime(dr)
	return Result{
		TMDbID:      id,
		MediaType:   "tv",
		Genres:      genreNames(dr),
		RuntimeMins: rt,
		VoteAverage: dr.VoteAverage,
		Popularity:  dr.Popularity,
		Director:    creatorNames(dr), // tv has no single director; credit the creator(s)
		Cast:        topCast(dr),
		Language:    languageName(dr.OriginalLanguage),
		Country:     countryNames(dr),
		Seasons:     dr.NumberOfSeasons,
		Episodes:    dr.NumberOfEpisodes,
		TVStatus:    dr.Status,
		TVType:      dr.Type,
	}, true, nil
}

// Details fetches full metadata for a known TMDb ID. mediaType is "movie"
// or "tv".
func (c *Client) Details(mediaType string, id int) (Result, error) {
	var (
		r   Result
		err error
	)
	switch mediaType {
	case "movie":
		r, _, err = c.movieDetails(id)
	case "tv":
		r, _, err = c.tvDetails(id)
	default:
		return Result{}, fmt.Errorf("unknown media type %q", mediaType)
	}
	return r, err
}

// SearchHit is one candidate from a free-text TMDb search.
type SearchHit struct {
	TMDbID     int
	MediaType  string // "movie" | "tv"
	Name       string
	Year       string // release / first-air year, "" if unknown
	Overview   string
	Popularity float64
}

// maxHits caps how many search results are returned.
const maxHits = 10

type multiResp struct {
	Results []struct {
		ID           int     `json:"id"`
		MediaType    string  `json:"media_type"` // multi only
		Title        string  `json:"title"`      // movie
		Name         string  `json:"name"`       // tv
		ReleaseDate  string  `json:"release_date"`
		FirstAirDate string  `json:"first_air_date"`
		Overview     string  `json:"overview"`
		Popularity   float64 `json:"popularity"`
	} `json:"results"`
}

// Search runs a free-text search. mediaType "movie" or "tv" restricts the
// search to that kind; "" searches both (people are dropped). Results keep
// TMDb's relevance order. A trailing year ("shogun 2024") is split off and
// used to prefer hits from that year, since TMDb doesn't parse it.
func (c *Client) Search(query, mediaType string) ([]SearchHit, error) {
	name, year := SplitYear(query)
	hits, err := c.search(name, mediaType)
	if err != nil || year == "" {
		return hits, err
	}
	var same []SearchHit
	for _, h := range hits {
		if h.Year == year {
			same = append(same, h)
		}
	}
	if len(same) == 0 {
		return hits, nil
	}
	return same, nil
}

// SplitYear splits a trailing 4-digit year (1880-2099) off a query:
// "shogun 2024" → ("shogun", "2024"). A query that is only a year (the
// film "1917") is left whole.
func SplitYear(query string) (name, year string) {
	q := strings.TrimSpace(query)
	i := strings.LastIndexByte(q, ' ')
	if i < 0 {
		return q, ""
	}
	last := strings.Trim(q[i+1:], "()")
	if len(last) != 4 || (last[:2] != "18" && last[:2] != "19" && last[:2] != "20") {
		return q, ""
	}
	for _, r := range last {
		if r < '0' || r > '9' {
			return q, ""
		}
	}
	if last < "1880" {
		return q, ""
	}
	return strings.TrimSpace(q[:i]), last
}

func (c *Client) search(query, mediaType string) ([]SearchHit, error) {
	path := "/search/multi"
	switch mediaType {
	case "movie":
		path = "/search/movie"
	case "tv":
		path = "/search/tv"
	case "":
	default:
		return nil, fmt.Errorf("unknown media type %q", mediaType)
	}
	var mr multiResp
	if err := c.get(path, url.Values{"query": {query}}, &mr); err != nil {
		return nil, err
	}
	hits := make([]SearchHit, 0, maxHits)
	for _, r := range mr.Results {
		kind := mediaType
		if kind == "" {
			kind = r.MediaType
		}
		if kind != "movie" && kind != "tv" {
			continue // a person, or something new TMDb added
		}
		h := SearchHit{TMDbID: r.ID, MediaType: kind, Overview: r.Overview, Popularity: r.Popularity}
		date := r.ReleaseDate
		h.Name = r.Title
		if kind == "tv" {
			h.Name, date = r.Name, r.FirstAirDate
		}
		if len(date) >= 4 {
			h.Year = date[:4]
		}
		hits = append(hits, h)
		if len(hits) == maxHits {
			break
		}
	}
	return hits, nil
}

// episodeRuntime picks a typical episode length. episode_run_time is
// deprecated and empty for most newer shows, so fall back to the median of
// season 1's episodes (robust to a long pilot or finale), then to the latest
// aired episode.
func episodeRuntime(dr detailResp) int {
	if len(dr.EpisodeRuntimes) > 0 {
		return dr.EpisodeRuntimes[0]
	}
	if dr.Season1 != nil {
		var rts []int
		for _, e := range dr.Season1.Episodes {
			if e.Runtime > 0 {
				rts = append(rts, e.Runtime)
			}
		}
		if len(rts) > 0 {
			sort.Ints(rts)
			return rts[len(rts)/2]
		}
	}
	if dr.LastEpisode != nil {
		return dr.LastEpisode.Runtime
	}
	return 0
}

type providersResp struct {
	Results map[string]struct {
		Flatrate []struct {
			ProviderName string `json:"provider_name"`
		} `json:"flatrate"`
	} `json:"results"`
}

// WatchProviders returns the subscription ("flatrate") streaming services
// offering the given title in region (an ISO 3166-1 country code, e.g. "US").
// A nil, non-error result means the title isn't offered on any subscription
// service in that region (it may still be available to rent/buy).
func (c *Client) WatchProviders(mediaType string, id int, region string) ([]string, error) {
	var pr providersResp
	if err := c.get(fmt.Sprintf("/%s/%d/watch/providers", mediaType, id), nil, &pr); err != nil {
		return nil, err
	}
	entry, ok := pr.Results[strings.ToUpper(region)]
	if !ok || len(entry.Flatrate) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(entry.Flatrate))
	for _, p := range entry.Flatrate {
		out = append(out, p.ProviderName)
	}
	sort.Strings(out)
	return out, nil
}

func genreNames(dr detailResp) []string {
	out := make([]string, 0, len(dr.Genres))
	for _, g := range dr.Genres {
		out = append(out, g.Name)
	}
	return out
}

// director returns the first crew member credited as "Director", or "" if
// none is listed (credits are only present when fetched with
// append_to_response=credits).
func director(dr detailResp) string {
	for _, m := range dr.Credits.Crew {
		if m.Job == "Director" {
			return m.Name
		}
	}
	return ""
}

// creatorNames joins a tv show's "created by" credits — tv has no single
// director, since episodes within one season can have different ones.
func creatorNames(dr detailResp) string {
	names := make([]string, 0, len(dr.CreatedBy))
	for _, p := range dr.CreatedBy {
		names = append(names, p.Name)
	}
	return strings.Join(names, ", ")
}

// topCast returns up to maxCast top-billed cast members, in credited order.
func topCast(dr detailResp) []string {
	n := len(dr.Credits.Cast)
	if n > maxCast {
		n = maxCast
	}
	out := make([]string, 0, n)
	for _, p := range dr.Credits.Cast[:n] {
		out = append(out, p.Name)
	}
	return out
}

// countryNames joins a title's production country names.
func countryNames(dr detailResp) string {
	names := make([]string, 0, len(dr.ProductionCountries))
	for _, c := range dr.ProductionCountries {
		names = append(names, c.Name)
	}
	return strings.Join(names, ", ")
}

// languageNames maps common ISO 639-1 codes to a friendly display name.
// Anything not listed falls back to the uppercased code, which is still
// meaningful (e.g. "PT", "SV").
var languageNames = map[string]string{
	"en": "English", "ko": "Korean", "ja": "Japanese", "fr": "French",
	"es": "Spanish", "de": "German", "it": "Italian", "hi": "Hindi",
	"zh": "Mandarin", "cn": "Cantonese", "pt": "Portuguese", "ru": "Russian",
	"sv": "Swedish", "da": "Danish", "no": "Norwegian", "fi": "Finnish",
	"nl": "Dutch", "pl": "Polish", "tr": "Turkish", "ar": "Arabic",
	"th": "Thai", "id": "Indonesian", "he": "Hebrew", "el": "Greek",
	"ta": "Tamil", "te": "Telugu", "bn": "Bengali", "ml": "Malayalam",
	"pa": "Punjabi", "mr": "Marathi", "gu": "Gujarati", "kn": "Kannada",
}

func languageName(code string) string {
	if code == "" {
		return ""
	}
	if name, ok := languageNames[code]; ok {
		return name
	}
	return strings.ToUpper(code)
}

// redact strips the request URL — which carries the api_key query param —
// from a transport error, so the key never reaches logs or the terminal.
func redact(path string, err error) error {
	var uerr *url.Error
	if errors.As(err, &uerr) {
		return fmt.Errorf("tmdb %s: %w", path, uerr.Err)
	}
	return err
}

// get issues a GET to base+path with the api_key appended and decodes JSON
// into out.
func (c *Client) get(path string, q url.Values, out any) error {
	if q == nil {
		q = url.Values{}
	}
	q.Set("api_key", c.key)
	u := c.base + path + "?" + q.Encode()

	status, body, err := c.fetch(path, u)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("tmdb %s: status %d", path, status)
	}
	return json.Unmarshal(body, out)
}
