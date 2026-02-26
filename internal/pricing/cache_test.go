package pricing

import (
	"os"
	"path/filepath"
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
	if _, err := os.Stat(filepath.Join(dir, cacheFileName)); err != nil {
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
	if err := os.WriteFile(filepath.Join(dir, cacheFileName), []byte("not json"), 0o644); err != nil {
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
