package enrich

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/jarvis1704/whatnext/internal/store"
	"github.com/jarvis1704/whatnext/internal/tmdb"
)

// cacheEntry is one cached TMDb identity lookup: a title's genre/runtime/
// rating metadata, or a recorded miss (Found=false) so a title TMDb doesn't
// recognize isn't re-queried on every refresh.
type cacheEntry struct {
	Result tmdb.Result `json:"result"`
	Found  bool        `json:"found"`
}

// Cache is an on-disk, name+year keyed cache of TMDb identity lookups,
// shared by every worker in a concurrent Run. It deliberately caches
// identity lookups only (genres/runtime/rating are effectively static) —
// watch-provider data is never cached here, since NeedsProviders exists
// precisely to keep that data fresh as services change.
type Cache struct {
	mu      sync.Mutex
	entries map[string]cacheEntry
	dirty   bool
}

func cachePath() (string, error) {
	dir, err := store.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "tmdb_cache.json"), nil
}

// loadCache reads the cache from disk. A missing or corrupt file yields an
// empty cache rather than an error — the cache is a pure optimization.
func loadCache() *Cache {
	c := &Cache{entries: map[string]cacheEntry{}}
	path, err := cachePath()
	if err != nil {
		return c
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 {
		return c
	}
	var entries map[string]cacheEntry
	if err := json.Unmarshal(raw, &entries); err == nil && entries != nil {
		c.entries = entries
	}
	return c
}

func cacheKey(name, year string) string {
	return strings.ToLower(strings.TrimSpace(name)) + "|" + strings.TrimSpace(year)
}

func (c *Cache) get(name, year string) (cacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[cacheKey(name, year)]
	return e, ok
}

func (c *Cache) put(name, year string, e cacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[cacheKey(name, year)] = e
	c.dirty = true
}

// save writes the cache back to disk atomically via a temp file + rename.
func (c *Cache) save() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	path, err := cachePath()
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(c.entries, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
