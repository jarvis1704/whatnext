// Package tmdb is a thin standard-library HTTP client for the TMDb v3 API,
// used to enrich watchlist titles with genre, runtime, media type, rating,
// director/creator, top cast, language, and production country.
package tmdb

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const base = "https://api.themoviedb.org/3"

// ErrNoKey is returned when TMDB_API_KEY is unset.
var ErrNoKey = fmt.Errorf("TMDB_API_KEY is not set")

// Client talks to TMDb using a v3 API key.
type Client struct {
	key  string
	http *http.Client
}

// New returns a Client, or ErrNoKey if key is empty.
func New(key string) (*Client, error) {
	if strings.TrimSpace(key) == "" {
		return nil, ErrNoKey
	}
	return &Client{
		key:  key,
		http: &http.Client{Timeout: 15 * time.Second},
	}, nil
}

// ErrBadKey signals the API rejected the key (HTTP 401).
var ErrBadKey = fmt.Errorf("TMDb rejected the API key (401)")

// Health verifies the key and reachability by hitting /configuration. It
// returns the round-trip latency, ErrBadKey on 401, or the transport error.
func (c *Client) Health() (time.Duration, error) {
	q := url.Values{"api_key": {c.key}}
	start := time.Now()
	resp, err := c.http.Get(base + "/configuration?" + q.Encode())
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	lat := time.Since(start)
	switch resp.StatusCode {
	case http.StatusOK:
		return lat, nil
	case http.StatusUnauthorized:
		return lat, ErrBadKey
	default:
		return lat, fmt.Errorf("unexpected status %d", resp.StatusCode)
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
	if err := c.get(fmt.Sprintf("/tv/%d", id), url.Values{"append_to_response": {"credits"}}, &dr); err != nil {
		return Result{}, false, err
	}
	rt := 0
	if len(dr.EpisodeRuntimes) > 0 {
		rt = dr.EpisodeRuntimes[0]
	}
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
	}, true, nil
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

// get issues a GET to base+path with the api_key appended and decodes JSON
// into out.
func (c *Client) get(path string, q url.Values, out any) error {
	if q == nil {
		q = url.Values{}
	}
	q.Set("api_key", c.key)
	u := base + path + "?" + q.Encode()

	resp, err := c.http.Get(u)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("tmdb %s: status %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
