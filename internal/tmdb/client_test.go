package tmdb

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeTMDb serves canned JSON per path and returns a Client pointed at it.
func fakeTMDb(t *testing.T, routes map[string]string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	c, err := New("test-key")
	if err != nil {
		t.Fatal(err)
	}
	c.base = srv.URL
	return c
}

func TestSearchMultiDropsPeopleAndMapsFields(t *testing.T) {
	c := fakeTMDb(t, map[string]string{
		"/search/multi": `{"results":[
			{"id":95396,"media_type":"tv","name":"Severance","first_air_date":"2022-02-17","overview":"Work/life."},
			{"id":1,"media_type":"person","name":"Adam Scott"},
			{"id":329865,"media_type":"movie","title":"Arrival","release_date":"2016-11-10"},
			{"id":2,"media_type":"tv","name":"No Date"}
		]}`,
	})
	hits, err := c.Search("sev", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 3 {
		t.Fatalf("got %d hits, want 3 (person dropped): %+v", len(hits), hits)
	}
	if h := hits[0]; h.TMDbID != 95396 || h.MediaType != "tv" || h.Name != "Severance" || h.Year != "2022" {
		t.Errorf("tv hit = %+v", h)
	}
	if h := hits[1]; h.MediaType != "movie" || h.Name != "Arrival" || h.Year != "2016" {
		t.Errorf("movie hit = %+v", h)
	}
	if hits[2].Year != "" {
		t.Errorf("missing date should give empty year, got %q", hits[2].Year)
	}
}

func TestSearchTVOnlyUsesTVEndpoint(t *testing.T) {
	c := fakeTMDb(t, map[string]string{
		"/search/tv": `{"results":[{"id":7,"name":"Shōgun","first_air_date":"2024-02-27"}]}`,
	})
	hits, err := c.Search("shogun", "tv")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].MediaType != "tv" || hits[0].Name != "Shōgun" {
		t.Fatalf("hits = %+v", hits)
	}
}

func TestTVDetailsCommitmentAndRuntimeFallback(t *testing.T) {
	c := fakeTMDb(t, map[string]string{
		"/tv/95396": `{
			"genres":[{"name":"Drama"}],
			"episode_run_time":[],
			"last_episode_to_air":{"runtime":52},
			"number_of_seasons":2,"number_of_episodes":19,
			"status":"Returning Series","type":"Scripted",
			"created_by":[{"name":"Dan Erickson"}]
		}`,
	})
	r, err := c.Details("tv", 95396)
	if err != nil {
		t.Fatal(err)
	}
	if r.RuntimeMins != 52 {
		t.Errorf("RuntimeMins = %d, want 52 from last_episode_to_air", r.RuntimeMins)
	}
	if r.Seasons != 2 || r.Episodes != 19 || r.TVStatus != "Returning Series" || r.TVType != "Scripted" {
		t.Errorf("commitment fields = %+v", r)
	}
	if r.Director != "Dan Erickson" || r.MediaType != "tv" {
		t.Errorf("identity fields = %+v", r)
	}
}

func TestLookupFallsBackToTV(t *testing.T) {
	c := fakeTMDb(t, map[string]string{
		"/search/movie": `{"results":[]}`,
		"/search/tv":    `{"results":[{"id":9}]}`,
		"/tv/9":         `{"episode_run_time":[30],"number_of_seasons":1,"status":"Ended"}`,
	})
	r, ok, err := c.Lookup("Fleabag", "")
	if err != nil || !ok {
		t.Fatalf("Lookup ok=%v err=%v", ok, err)
	}
	if r.MediaType != "tv" || r.TMDbID != 9 || r.RuntimeMins != 30 {
		t.Errorf("r = %+v", r)
	}
}

func TestSplitYear(t *testing.T) {
	cases := map[string][2]string{
		"shogun 2024":       {"shogun", "2024"},
		"Dune (2021)":       {"Dune", "2021"},
		"1917":              {"1917", ""},
		"blade runner 2049": {"blade runner", "2049"},
		"ocean's 11":        {"ocean's 11", ""},
		"apollo 13":         {"apollo 13", ""},
	}
	for in, want := range cases {
		n, y := SplitYear(in)
		if n != want[0] || y != want[1] {
			t.Errorf("SplitYear(%q) = (%q, %q), want %q", in, n, y, want)
		}
	}
}

func TestSearchPrefersTrailingYear(t *testing.T) {
	c := fakeTMDb(t, map[string]string{
		"/search/multi": `{"results":[
			{"id":1,"media_type":"tv","name":"Shōgun","first_air_date":"1980-09-15"},
			{"id":2,"media_type":"tv","name":"Shōgun","first_air_date":"2024-02-27"}
		]}`,
	})
	hits, err := c.Search("shogun 2024", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].TMDbID != 2 {
		t.Fatalf("hits = %+v, want only the 2024 show", hits)
	}
}

func TestEpisodeRuntimeMedianOfSeasonOne(t *testing.T) {
	c := fakeTMDb(t, map[string]string{
		"/tv/5": `{"episode_run_time":[],"last_episode_to_air":{"runtime":80},
			"season/1":{"episodes":[{"runtime":57},{"runtime":50},{"runtime":0},{"runtime":40},{"runtime":53}]}}`,
	})
	r, err := c.Details("tv", 5)
	if err != nil {
		t.Fatal(err)
	}
	if r.RuntimeMins != 53 {
		t.Errorf("RuntimeMins = %d, want 53 (median of season 1, ignoring 0)", r.RuntimeMins)
	}
}

func TestTransportErrorRedactsKey(t *testing.T) {
	c, _ := New("super-secret-key")
	c.base = "http://127.0.0.1:1" // nothing listens here
	_, err := c.Search("x", "")
	if err == nil {
		t.Fatal("expected a transport error")
	}
	if strings.Contains(err.Error(), "super-secret-key") {
		t.Fatalf("error leaks the API key: %v", err)
	}
}
