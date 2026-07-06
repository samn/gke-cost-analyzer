package pricing

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestCacheSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)

	cache, err := NewCache(
		WithCacheDir(dir),
		WithNowFunc(func() time.Time { return now }),
	)
	if err != nil {
		t.Fatal(err)
	}

	prices := []Price{
		{Region: "us-central1", ResourceType: CPU, Tier: OnDemand, UnitPrice: 0.035},
		{Region: "us-central1", ResourceType: Memory, Tier: OnDemand, UnitPrice: 0.004},
		{Region: "us-central1", ResourceType: CPU, Tier: Spot, UnitPrice: 0.01},
	}

	if err := cache.Save(prices); err != nil {
		t.Fatal(err)
	}

	// Verify file was written
	if _, err := os.Stat(filepath.Join(dir, defaultCacheFileName)); err != nil {
		t.Fatal("cache file not created")
	}

	loaded, err := cache.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded == nil {
		t.Fatal("expected cached prices, got nil")
	}
	if len(loaded.Prices) != len(prices) {
		t.Fatalf("expected %d prices, got %d", len(prices), len(loaded.Prices))
	}

	for i, p := range loaded.Prices {
		if p.Region != prices[i].Region || p.ResourceType != prices[i].ResourceType ||
			p.Tier != prices[i].Tier || p.UnitPrice != prices[i].UnitPrice {
			t.Errorf("price %d mismatch: got %+v, want %+v", i, p, prices[i])
		}
	}
}

func TestCacheExpiry(t *testing.T) {
	dir := t.TempDir()
	savedAt := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)

	// Save with time at savedAt
	saveCache, err := NewCache(
		WithCacheDir(dir),
		WithCacheTTL(1*time.Hour),
		WithNowFunc(func() time.Time { return savedAt }),
	)
	if err != nil {
		t.Fatal(err)
	}

	prices := []Price{
		{Region: "us-central1", ResourceType: CPU, Tier: OnDemand, UnitPrice: 0.035},
	}
	if err := saveCache.Save(prices); err != nil {
		t.Fatal(err)
	}

	// Load 30 minutes later — should succeed
	loadCache, err := NewCache(
		WithCacheDir(dir),
		WithCacheTTL(1*time.Hour),
		WithNowFunc(func() time.Time { return savedAt.Add(30 * time.Minute) }),
	)
	if err != nil {
		t.Fatal(err)
	}

	loaded, err := loadCache.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded == nil {
		t.Fatal("expected cached prices within TTL, got nil")
	}

	// Load 2 hours later — should return nil (expired)
	expiredCache, err := NewCache(
		WithCacheDir(dir),
		WithCacheTTL(1*time.Hour),
		WithNowFunc(func() time.Time { return savedAt.Add(2 * time.Hour) }),
	)
	if err != nil {
		t.Fatal(err)
	}

	loaded, err = expiredCache.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded != nil {
		t.Fatal("expected nil for expired cache")
	}
}

func TestCacheMissingFile(t *testing.T) {
	dir := t.TempDir()

	cache, err := NewCache(WithCacheDir(dir))
	if err != nil {
		t.Fatal(err)
	}

	loaded, err := cache.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded != nil {
		t.Fatal("expected nil for missing cache file")
	}
}

func TestCacheCorruptFile(t *testing.T) {
	dir := t.TempDir()

	// Write garbage to the cache file
	if err := os.WriteFile(filepath.Join(dir, defaultCacheFileName), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	cache, err := NewCache(WithCacheDir(dir))
	if err != nil {
		t.Fatal(err)
	}

	loaded, err := cache.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded != nil {
		t.Fatal("expected nil for corrupt cache file")
	}
}

func TestCacheLoadReadError(t *testing.T) {
	// Point cache at a file (not a directory) so ReadFile fails with a non-NotExist error.
	dir := t.TempDir()
	filePath := filepath.Join(dir, "blockingfile")
	if err := os.WriteFile(filePath, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Use the file as the cache dir — reading filePath/prices.json will fail
	// because filePath is a file, not a directory.
	cache, err := NewCache(WithCacheDir(filePath))
	if err != nil {
		t.Fatal(err)
	}

	_, err = cache.Load()
	if err == nil {
		t.Fatal("expected error when cache dir is a file")
	}
}

func TestCacheSaveCreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "nested", "cache")

	cache, err := NewCache(WithCacheDir(subDir))
	if err != nil {
		t.Fatal(err)
	}

	prices := []Price{
		{Region: "us-central1", ResourceType: CPU, Tier: OnDemand, UnitPrice: 0.035},
	}

	if err := cache.Save(prices); err != nil {
		t.Fatalf("Save should create nested dirs: %v", err)
	}

	// Verify the file exists
	if _, err := os.Stat(filepath.Join(subDir, defaultCacheFileName)); err != nil {
		t.Fatal("expected cache file to exist in nested dir")
	}
}

func TestCacheTTLOption(t *testing.T) {
	dir := t.TempDir()
	savedAt := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)

	// Save with default TTL
	saveCache, err := NewCache(
		WithCacheDir(dir),
		WithCacheTTL(10*time.Minute),
		WithNowFunc(func() time.Time { return savedAt }),
	)
	if err != nil {
		t.Fatal(err)
	}

	prices := []Price{
		{Region: "us-central1", ResourceType: CPU, Tier: OnDemand, UnitPrice: 0.035},
	}
	if err := saveCache.Save(prices); err != nil {
		t.Fatal(err)
	}

	// Load 5 minutes later — should succeed with 10m TTL
	loadCache, err := NewCache(
		WithCacheDir(dir),
		WithCacheTTL(10*time.Minute),
		WithNowFunc(func() time.Time { return savedAt.Add(5 * time.Minute) }),
	)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := loadCache.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded == nil {
		t.Fatal("expected valid cache within custom TTL")
	}

	// Load 15 minutes later — should expire with 10m TTL
	expiredCache, err := NewCache(
		WithCacheDir(dir),
		WithCacheTTL(10*time.Minute),
		WithNowFunc(func() time.Time { return savedAt.Add(15 * time.Minute) }),
	)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err = expiredCache.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded != nil {
		t.Fatal("expected nil for cache expired beyond custom TTL")
	}
}

func TestCacheSaveIsAtomic(t *testing.T) {
	// Save must go through a temp file + rename (new inode each save) so a
	// crash or concurrent process can never observe a truncated cache file.
	dir := t.TempDir()
	c, err := NewCache(WithCacheDir(dir))
	if err != nil {
		t.Fatal(err)
	}

	prices := []Price{{Region: "us-central1", ResourceType: CPU, Tier: OnDemand, UnitPrice: 0.01}}
	if err := c.Save(prices); err != nil {
		t.Fatal(err)
	}
	inodeBefore := cacheFileInode(t, c.path())

	if err := c.Save(prices); err != nil {
		t.Fatal(err)
	}
	if inodeAfter := cacheFileInode(t, c.path()); inodeAfter == inodeBefore {
		t.Error("cache save rewrote the file in place (same inode); expected temp-file + rename")
	}

	// No temp remnants.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("expected only the cache file, got %d entries", len(entries))
	}
}

func cacheFileInode(t *testing.T, path string) uint64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Skip("inode not available on this platform")
	}
	return st.Ino
}

func TestComputeCacheUsesSeparateDefaultFile(t *testing.T) {
	// A bare NewCache() must not mix Autopilot and Compute Engine prices in
	// one file: the two on-disk shapes decode into each other without error,
	// silently corrupting whichever loads second.
	dir := t.TempDir()
	c, err := NewCache(WithCacheDir(dir))
	if err != nil {
		t.Fatal(err)
	}

	if err := c.SaveComputePrices([]ComputePrice{
		{Region: "us-central1", MachineFamily: "n2", ResourceType: CPU, Tier: OnDemand, UnitPrice: 0.03},
	}); err != nil {
		t.Fatal(err)
	}

	// The Autopilot cache must still be a miss.
	got, err := c.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("Autopilot cache should be a miss after saving compute prices, got %+v", got)
	}

	// And the compute cache loads its own data.
	cached, err := c.LoadComputePrices()
	if err != nil {
		t.Fatal(err)
	}
	if cached == nil || len(cached.Prices) != 1 {
		t.Fatalf("compute cache miss after save: %+v", cached)
	}
}
