// Package cache is a small JSON file cache with a TTL, used for pricing.
package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type entry struct {
	At   time.Time       `json:"at"`
	Data json.RawMessage `json:"data"`
}

type Cache struct {
	path    string
	ttl     time.Duration
	mu      sync.Mutex
	entries map[string]entry
	dirty   bool
}

// Dir returns the cache directory. NCLI_CACHE_DIR overrides it.
func Dir() string {
	if d := os.Getenv("NCLI_CACHE_DIR"); d != "" {
		return d
	}
	d, err := os.UserCacheDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "ncli")
	}
	return filepath.Join(d, "ncli")
}

// Open loads <cache dir>/<name>. A missing or corrupt file starts empty.
func Open(name string, ttl time.Duration) *Cache {
	c := &Cache{path: filepath.Join(Dir(), name), ttl: ttl, entries: map[string]entry{}}
	if b, err := os.ReadFile(c.path); err == nil {
		_ = json.Unmarshal(b, &c.entries)
	}
	return c
}

func (c *Cache) Get(key string, v any) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok || time.Since(e.At) > c.ttl {
		return false
	}
	return json.Unmarshal(e.Data, v) == nil
}

func (c *Cache) Set(key string, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = entry{At: time.Now(), Data: b}
	c.dirty = true
}

// Save writes the cache if anything changed. Expired entries are dropped.
func (c *Cache) Save() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.dirty {
		return nil
	}
	for k, e := range c.entries {
		if time.Since(e.At) > c.ttl {
			delete(c.entries, k)
		}
	}
	b, err := json.Marshal(c.entries)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	c.dirty = false
	return os.Rename(tmp, c.path)
}
