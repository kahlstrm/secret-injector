//go:build integration

package resolvercontract

import (
	"testing"

	"go.uber.org/goleak"
)

// assertNoGoroutineLeaks fails the test if resolving starts a goroutine that
// outlives it. A backend whose client owns a connection pool — gRPC, most
// obviously — leaks one pool per resolver unless something releases it, and
// nothing in the CLI or the library API does.
//
// Goroutines already running are ignored, since the caller has usually built its
// backend, and any container, before reaching the contract. Idle HTTP connections
// are ignored too: net/http keeps a reader and writer per pooled connection and
// reaps them on its own schedule, which is not a leak the resolver controls.
func assertNoGoroutineLeaks(t *testing.T) {
	t.Helper()

	options := []goleak.Option{
		goleak.IgnoreCurrent(),
		goleak.IgnoreTopFunction("net/http.(*persistConn).readLoop"),
		goleak.IgnoreTopFunction("net/http.(*persistConn).writeLoop"),
		goleak.IgnoreAnyFunction("internal/poll.runtime_pollWait"),
	}
	t.Cleanup(func() { goleak.VerifyNone(t, options...) })
}
