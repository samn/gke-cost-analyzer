package cmd

import (
	"testing"
	"time"
)

func TestWatchRequiresRegion(t *testing.T) {
	saved := region
	defer func() { region = saved }()
	region = ""

	err := runWatch(rootCmd, nil)
	if err == nil {
		t.Fatal("expected error when --region is missing")
	}
	if err.Error() != "--region is required" {
		t.Errorf("error should mention --region, got: %v", err)
	}
}

func TestWatchRejectsZeroInterval(t *testing.T) {
	saved := region
	savedInterval := watchInterval
	defer func() {
		region = saved
		watchInterval = savedInterval
	}()
	region = "us-central1"
	watchInterval = 0

	err := runWatch(rootCmd, nil)
	if err == nil {
		t.Fatal("expected error for zero interval")
	}
	if err.Error() != "--interval must be positive" {
		t.Errorf("error should mention --interval, got: %v", err)
	}
}

func TestWatchRejectsNegativeInterval(t *testing.T) {
	saved := region
	savedInterval := watchInterval
	defer func() {
		region = saved
		watchInterval = savedInterval
	}()
	region = "us-central1"
	watchInterval = -5 * time.Second

	err := runWatch(rootCmd, nil)
	if err == nil {
		t.Fatal("expected error for negative interval")
	}
	if err.Error() != "--interval must be positive" {
		t.Errorf("error should mention --interval, got: %v", err)
	}
}
