// Package letterboxd parses a Letterboxd data export into store.Title values.
// It accepts either the full export .zip or an already-extracted
// watchlist.csv.
package letterboxd

import (
	"archive/zip"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/jarvis1704/whatnext/internal/store"
)

// ParseWatchlist reads titles from the given path. If path ends in .zip it is
// treated as a Letterboxd export archive and watchlist.csv is located inside;
// otherwise path is read as a raw CSV.
func ParseWatchlist(path string) ([]store.Title, error) {
	if strings.HasSuffix(strings.ToLower(path), ".zip") {
		return parseZip(path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parseCSV(f)
}

// parseZip opens the export archive and hands watchlist.csv to parseCSV.
func parseZip(path string) ([]store.Title, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	for _, f := range r.File {
		name := strings.ToLower(f.Name)
		if strings.HasSuffix(name, "watchlist.csv") {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return parseCSV(rc)
		}
	}
	return nil, fmt.Errorf("no watchlist.csv found inside %s", path)
}

// parseCSV reads the export CSV, matching columns by header name (Name, Year,
// Letterboxd URI) rather than fixed position — Letterboxd has reordered
// columns before.
func parseCSV(r io.Reader) ([]store.Title, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1 // tolerate ragged rows

	header, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("reading header: %w", err)
	}

	idx := map[string]int{}
	for i, h := range header {
		idx[strings.ToLower(strings.TrimSpace(h))] = i
	}
	nameCol, ok := idx["name"]
	if !ok {
		return nil, fmt.Errorf("CSV missing 'Name' column")
	}
	yearCol := idx["year"] // may be absent (-> 0), guarded below
	_, hasYear := idx["year"]
	uriCol, hasURI := idx["letterboxd uri"]

	var titles []store.Title
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		name := field(rec, nameCol)
		if strings.TrimSpace(name) == "" {
			continue
		}
		t := store.Title{Name: name}
		if hasYear {
			t.Year = field(rec, yearCol)
		}
		if hasURI {
			t.LetterboxdURI = field(rec, uriCol)
		}
		// Fall back to a synthetic key so titles without a URI still dedupe.
		if t.LetterboxdURI == "" {
			t.LetterboxdURI = "synthetic:" + t.Name + ":" + t.Year
		}
		titles = append(titles, t)
	}
	return titles, nil
}

// field safely reads column i from a record, returning "" if out of range.
func field(rec []string, i int) string {
	if i < 0 || i >= len(rec) {
		return ""
	}
	return strings.TrimSpace(rec[i])
}
