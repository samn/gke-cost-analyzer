// Package sentry provides Sentry error-reporting initialization.
// Sentry is only enabled when the SENTRY_DSN environment variable is set.
package sentry

import (
	"os"
	"time"

	"github.com/getsentry/sentry-go"
)

// flushTimeout is the maximum time to wait for buffered events to be sent
// during Flush.
const flushTimeout = 2 * time.Second

// Init initialises the Sentry SDK for error reporting only (no tracing).
// If SENTRY_DSN is not set, Init is a no-op and the returned cleanup
// function does nothing.  The release string is attached to every event.
func Init(release string) (cleanup func(), err error) {
	noop := func() {}

	dsn := os.Getenv("SENTRY_DSN")
	if dsn == "" {
		return noop, nil
	}

	if err := sentry.Init(sentry.ClientOptions{
		Dsn:     dsn,
		Release: release,
	}); err != nil {
		return noop, err
	}

	return func() { sentry.Flush(flushTimeout) }, nil
}

// CaptureError sends an error event to Sentry.
// It is safe to call even when Sentry has not been initialised.
func CaptureError(err error) {
	sentry.CaptureException(err)
}

// RecoverAndCapture recovers from a panic and sends the event to Sentry.
// Call it as a deferred function.
func RecoverAndCapture() {
	sentry.Recover()
}
