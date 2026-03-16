package pricing

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const (
	defaultCacheDir      = "autopilot-cost-analyzer"
	defaultCacheFileName = "prices.json"
	defaultCacheTTL      = 24 * time.Hour
)

// CachedPrices is the on-disk format for cached pricing data.
type CachedPrices struct {
	FetchedAt time.Time `json:"fetched_at"`
	Prices    []Price   `json:"prices"`
}

// CachedComputePrices is the on-disk format for cached compute pricing data.
type CachedComputePrices struct {
	FetchedAt time.Time      `json:"fetched_at"`
	Prices    []ComputePrice `json:"prices"`
}

// cachedData is a constraint for types that can be stored in the cache.
type cachedData interface {
	CachedPrices | CachedComputePrices
	fetchedAt() time.Time
}

func (c CachedPrices) fetchedAt() time.Time        { return c.FetchedAt }
func (c CachedComputePrices) fetchedAt() time.Time { return c.FetchedAt }

// Cache manages reading and writing pricing data to a local file cache.
type Cache struct {
	dir      string
	ttl      time.Duration
	now      func() time.Time // for testing
	fileName string
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

// WithCacheFileName overrides the cache file name.
func WithCacheFileName(name string) CacheOption {
	return func(c *Cache) { c.fileName = name }
}

// NewCache creates a new Cache with the given options.
func NewCache(opts ...CacheOption) (*Cache, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}

	c := &Cache{
		dir:      filepath.Join(cacheDir, defaultCacheDir),
		ttl:      defaultCacheTTL,
		now:      time.Now,
		fileName: defaultCacheFileName,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

func (c *Cache) path() string {
	return filepath.Join(c.dir, c.fileName)
}

// loadCached is the generic implementation for loading cached data from disk.
func loadCached[T cachedData](c *Cache) (*T, error) {
	data, err := os.ReadFile(c.path())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var cached T
	if err := json.Unmarshal(data, &cached); err != nil {
		return nil, nil // treat corrupt cache as cache miss
	}

	if c.now().Sub(cached.fetchedAt()) > c.ttl {
		return nil, nil // expired
	}

	return &cached, nil
}

// saveCached is the generic implementation for writing cached data to disk.
func saveCached[T any](c *Cache, cached T) error {
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cached, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(c.path(), data, 0o644)
}

// Load reads cached prices from disk. Returns nil if the cache is missing or expired.
func (c *Cache) Load() (*CachedPrices, error) {
	return loadCached[CachedPrices](c)
}

// Save writes prices to the cache file on disk.
func (c *Cache) Save(prices []Price) error {
	return saveCached(c, CachedPrices{
		FetchedAt: c.now(),
		Prices:    prices,
	})
}

// LoadComputePrices reads cached compute prices from disk. Returns nil if the cache is missing or expired.
func (c *Cache) LoadComputePrices() (*CachedComputePrices, error) {
	return loadCached[CachedComputePrices](c)
}

// SaveComputePrices writes compute prices to the cache file on disk.
func (c *Cache) SaveComputePrices(prices []ComputePrice) error {
	return saveCached(c, CachedComputePrices{
		FetchedAt: c.now(),
		Prices:    prices,
	})
}
