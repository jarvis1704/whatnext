package pick

import (
	"reflect"
	"testing"

	"github.com/jarvis1704/whatnext/internal/store"
)

func TestFilterByQuery(t *testing.T) {
	titles := []store.Title{
		{Name: "Oldboy"},
		{Name: "Paddington 2"},
		{Name: "Parasite"},
	}

	cases := []struct {
		query string
		want  []int
	}{
		{"", []int{0, 1, 2}},
		{"pa", []int{1, 2}},
		{"OLD", []int{0}},
		{"zzz", nil},
	}
	for _, c := range cases {
		got := FilterByQuery(titles, c.query)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("FilterByQuery(%q) = %v, want %v", c.query, got, c.want)
		}
	}
}
