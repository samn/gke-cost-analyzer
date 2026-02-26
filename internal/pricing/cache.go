package pricing

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const (
	defaultCacheDir = "autopilot-cost-analyzer"
	cacheFileName   = "prices.json"
	defaultCacheTTL = 24 * time.Hour
)

// CachedPrices is the on-disk format for cached pricing data.
type CachedPrices struct {
	FetchedAt time.Time `json:"fetched_at"`
	Prices    []Price   `json:"prices"`
}

// Cache manages reading and writing pricing data to a local file cache.
type Cache struct {
	dir string
	ttl time.Duration
	now func() time.Time // for testing
}

// CacheOption configures a Cache.
type CacheOption func(*Cache)

// WithCacheDir overrides the cache directory.
func WithCacheDir(dir string) CacheOption {
	return func(c *Cache) { c.dir = dir }
}

// WithCacheTTL overrides the cache TTL.
func WithCacheTTL(ttl time.Duration) CacheOption {
	return func(c *Cache) { c.ttl = ttl }
}

// WithNowFunc overrides the time source (for testing).
func WithNowFunc(fn func() time.Time) CacheOption {
	return func(c *Cache) { c.now = fn }
}

// NewCache creates a new Cache with the given options.
func NewCache(opts ...CacheOption) (*Cache, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}

	c := &Cache{
		dir: filepath.Join(cacheDir, defaultCacheDir),
		ttl: defaultCacheTTL,
		now: time.Now,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

func (c *Cache) path() string {
	return filepath.Join(c.dir, cacheFileName)
}

// Load reads cached prices from disk. Returns nil if the cache is missing or expired.
func (c *Cache) Load() (*CachedPrices, error) {
	data, err := os.ReadFile(c.path())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var cached CachedPrices
	if err := json.Unmarshal(data, &cached); err != nil {
		return nil, nil // treat corrupt cache as cache miss
	}

	if c.now().Sub(cached.FetchedAt) > c.ttl {
		return nil, nil // expired
	}

	return &cached, nil
}

// Save writes prices to the cache file on disk.
func (c *Cache) Save(prices []Price) error {
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return err
	}

	cached := CachedPrices{
		FetchedAt: c.now(),
		Prices:    prices,
	}

	data, err := json.MarshalIndent(cached, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(c.path(), data, 0o644)
}
