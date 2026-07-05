package pricing

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	defaultCacheDir             = "gke-cost-analyzer"
	defaultCacheFileName        = "prices.json"
	defaultComputeCacheFileName = "compute_prices.json"
)

// DefaultCacheTTL is how long cached prices stay fresh. Long-running daemons
// also use it to decide when to refresh their in-memory price tables.
const DefaultCacheTTL = 24 * time.Hour

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
// All public methods are safe for concurrent use. Autopilot and Compute
// Engine prices live in separate files by default — their on-disk shapes
// decode into each other without error, so sharing a file would silently
// corrupt whichever loads second.
type Cache struct {
	mu              sync.Mutex
	dir             string
	ttl             time.Duration
	now             func() time.Time // for testing
	fileName        string
	computeFileName string
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

// WithCacheFileName overrides the cache file name (for both price kinds).
func WithCacheFileName(name string) CacheOption {
	return func(c *Cache) {
		c.fileName = name
		c.computeFileName = name
	}
}

// NewCache creates a new Cache with the given options.
func NewCache(opts ...CacheOption) (*Cache, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}

	c := &Cache{
		dir:             filepath.Join(cacheDir, defaultCacheDir),
		ttl:             DefaultCacheTTL,
		now:             time.Now,
		fileName:        defaultCacheFileName,
		computeFileName: defaultComputeCacheFileName,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

func (c *Cache) path() string {
	return filepath.Join(c.dir, c.fileName)
}

// computePath returns the file used for Compute Engine prices — by default a
// separate file from the Autopilot cache (the two on-disk shapes decode into
// each other without error).
func (c *Cache) computePath() string {
	return filepath.Join(c.dir, c.computeFileName)
}

// loadCached is the generic implementation for loading cached data from disk.
func loadCached[T cachedData](c *Cache, path string) (*T, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var cached T
	if err := json.Unmarshal(data, &cached); err != nil {
		log.Printf("Warning: corrupt price cache at %s, treating as cache miss: %v", path, err)
		return nil, nil
	}

	if c.now().Sub(cached.fetchedAt()) > c.ttl {
		return nil, nil // expired
	}

	return &cached, nil
}

// saveCached is the generic implementation for writing cached data to disk.
// The write goes through a temp file + rename so a crash or a concurrently
// reading process can never observe a truncated cache file.
func saveCached[T any](c *Cache, path string, cached T) error {
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cached, "", "  ")
	if err != nil {
		return err
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// Load reads cached prices from disk. Returns nil if the cache is missing or expired.
func (c *Cache) Load() (*CachedPrices, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return loadCached[CachedPrices](c, c.path())
}

// Save writes prices to the cache file on disk.
func (c *Cache) Save(prices []Price) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return saveCached(c, c.path(), CachedPrices{
		FetchedAt: c.now(),
		Prices:    prices,
	})
}

// LoadComputePrices reads cached compute prices from disk. Returns nil if the cache is missing or expired.
func (c *Cache) LoadComputePrices() (*CachedComputePrices, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return loadCached[CachedComputePrices](c, c.computePath())
}

// SaveComputePrices writes compute prices to the cache file on disk.
func (c *Cache) SaveComputePrices(prices []ComputePrice) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return saveCached(c, c.computePath(), CachedComputePrices{
		FetchedAt: c.now(),
		Prices:    prices,
	})
}
