package store

import (
	"reflect"
	"testing"
)

func TestUpsertDedupesByTMDbIdentity(t *testing.T) {
	d := &Data{Titles: []Title{
		{Name: "Arrival", LetterboxdURI: "https://letterboxd.com/film/arrival-2016/", TMDbID: 329865, MediaType: "movie", Watched: true},
	}}
	added := d.Upsert(Title{Name: "Arrival", LetterboxdURI: TMDbKey("movie", 329865), TMDbID: 329865, MediaType: "movie"})
	if added || len(d.Titles) != 1 {
		t.Fatalf("Upsert added a TMDb duplicate of a Letterboxd import: %+v", d.Titles)
	}
	if !d.Titles[0].Watched {
		t.Error("existing watched flag clobbered")
	}
	// Same numeric ID but a different media type is a different title.
	if !d.Upsert(Title{Name: "Other", LetterboxdURI: TMDbKey("tv", 329865), TMDbID: 329865, MediaType: "tv"}) {
		t.Error("tv title with a colliding movie ID should be added")
	}
}

func TestUpsertDedupesByURI(t *testing.T) {
	d := &Data{}
	if !d.Upsert(Title{Name: "A", LetterboxdURI: "u"}) {
		t.Fatal("first insert should add")
	}
	if d.Upsert(Title{Name: "A again", LetterboxdURI: "u"}) {
		t.Fatal("same URI should not add")
	}
}

func TestSeriesSummaryAndCommitment(t *testing.T) {
	show := Title{MediaType: "tv", RuntimeMins: 45, Seasons: 3, Episodes: 30, TVStatus: "Ended", TVType: "Scripted"}
	if got := show.TotalRuntimeMins(); got != 1350 {
		t.Errorf("TotalRuntimeMins = %d, want 1350", got)
	}
	want := []string{"3 seasons", "30 eps", "~23h total", "Ended"}
	if got := show.SeriesSummary(); !reflect.DeepEqual(got, want) {
		t.Errorf("SeriesSummary = %q, want %q", got, want)
	}
	if !show.IsFinished() || show.IsMiniseries() {
		t.Errorf("finished=%v miniseries=%v", show.IsFinished(), show.IsMiniseries())
	}

	limited := Title{MediaType: "tv", Seasons: 1, TVStatus: "Ended"}
	if !limited.IsMiniseries() {
		t.Error("one finished season should count as a miniseries")
	}
	if (Title{MediaType: "movie", RuntimeMins: 120}).SeriesSummary() != nil {
		t.Error("movies have no series summary")
	}
}

func TestNeedsTVDetails(t *testing.T) {
	d := &Data{Titles: []Title{
		{Name: "old show", MediaType: "tv", Enriched: true},
		{Name: "new show", MediaType: "tv", Enriched: true, TVStatus: "Ended"},
		{Name: "movie", MediaType: "movie", Enriched: true},
		{Name: "unenriched", MediaType: "tv"},
	}}
	got := d.NeedsTVDetails()
	if len(got) != 1 || got[0].Name != "old show" {
		t.Fatalf("NeedsTVDetails = %+v", got)
	}
}
