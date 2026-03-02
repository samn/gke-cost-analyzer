package sentry_test

import (
	"testing"

	appSentry "github.com/samn/autopilot-cost-analyzer/internal/sentry"
)

func TestInit_NoDSN(t *testing.T) {
	t.Setenv("SENTRY_DSN", "")

	cleanup, err := appSentry.Init("test-version")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// cleanup should be a no-op, not nil.
	cleanup()
}

func TestInit_InvalidDSN(t *testing.T) {
	t.Setenv("SENTRY_DSN", "not-a-valid-dsn")

	_, err := appSentry.Init("test-version")
	if err == nil {
		t.Fatal("expected error for invalid DSN, got nil")
	}
}

func TestInit_ValidDSN(t *testing.T) {
	// Use the standard Sentry test DSN format.
	t.Setenv("SENTRY_DSN", "https://key@sentry.io/123")

	cleanup, err := appSentry.Init("v1.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer cleanup()
}

func TestCaptureError_NoInit(t *testing.T) {
	// Should not panic when Sentry is not initialised.
	t.Setenv("SENTRY_DSN", "")
	_, _ = appSentry.Init("")

	appSentry.CaptureError(nil)
}

func TestRecoverAndCapture_NoPanic(t *testing.T) {
	// Should not panic when called without a preceding panic.
	t.Setenv("SENTRY_DSN", "")
	_, _ = appSentry.Init("")

	appSentry.RecoverAndCapture()
}

func TestRecoverAndCapture_WithPanic(t *testing.T) {
	// Should capture the panic and re-panic so the caller sees a non-zero exit.
	t.Setenv("SENTRY_DSN", "")
	_, _ = appSentry.Init("")

	var rePanicked bool
	func() {
		defer func() {
			if r := recover(); r != nil {
				rePanicked = true
			}
		}()
		defer appSentry.RecoverAndCapture()
		panic("test panic")
	}()

	if !rePanicked {
		t.Fatal("expected RecoverAndCapture to re-panic after capturing")
	}
}
