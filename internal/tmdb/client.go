// Package tmdb is a thin standard-library HTTP client for the TMDb v3 API,
// used only to enrich watchlist titles with genre, runtime, and media type.
package tmdb

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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
	Runtime         int   `json:"runtime"`          // movie
	EpisodeRuntimes []int `json:"episode_run_time"` // tv
}

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
	if err := c.get(fmt.Sprintf("/movie/%d", id), nil, &dr); err != nil {
		return Result{}, false, err
	}
	return Result{
		TMDbID:      id,
		MediaType:   "movie",
		Genres:      genreNames(dr),
		RuntimeMins: dr.Runtime,
	}, true, nil
}

func (c *Client) tvDetails(id int) (Result, bool, error) {
	var dr detailResp
	if err := c.get(fmt.Sprintf("/tv/%d", id), nil, &dr); err != nil {
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
	}, true, nil
}

func genreNames(dr detailResp) []string {
	out := make([]string, 0, len(dr.Genres))
	for _, g := range dr.Genres {
		out = append(out, g.Name)
	}
	return out
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
