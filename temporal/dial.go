package temporal

import (
	"context"
	"fmt"
	"time"

	"go.temporal.io/sdk/client"
)

// Dial timing. Every reckon Temporal client (worker, dispatch client, backend
// probe) comes through Dial so the start-up contract is in one place:
//
//   - The supervisor's Run context carries no deadline, so a dial that never
//     gives up would hang start-up forever → dialReadyWindow caps the total
//     patience (the same 30s budget Keycloak gets to become ready).
//   - Supervisor.Start is all-or-nothing: one component failing to start tears
//     the whole stack down. A dial that gives up on its first stall therefore
//     turns a transient hiccup (a resolver stall, the frontend still settling
//     after a cold boot) into a dead stack → attempts retry until the window
//     closes.
//
// Package-level so tests can shrink them; production never writes them.
var (
	dialReadyWindow    = 30 * time.Second
	dialAttemptTimeout = 5 * time.Second // the SDK's own GetSystemInfo default
	dialRetryInterval  = 500 * time.Millisecond
)

// Dial connects to the Temporal frontend at hostPort and returns a ready
// client, retrying a failed connect until ctx is done. A ctx without a deadline
// is bounded by dialReadyWindow. The caller owns the returned client and must
// Close it. Namespace defaults to "default".
//
// The SDK's Dial fails with "context deadline exceeded" whenever its initial
// GetSystemInfo call does not complete within one attempt budget — that covers
// a hung frontend, but also a connect that never got as far as the server (gRPC
// hands the channel no address until name resolution finishes). Retrying here
// keeps a brief stall from failing a component start.
func Dial(ctx context.Context, hostPort, namespace string) (client.Client, error) {
	if namespace == "" {
		namespace = "default"
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, dialReadyWindow)
		defer cancel()
	}
	var lastErr error
	for attempt := 1; ; attempt++ {
		c, err := client.DialContext(ctx, client.Options{
			HostPort:  hostPort,
			Namespace: namespace,
			ConnectionOptions: client.ConnectionOptions{
				GetSystemInfoTimeout: dialAttemptTimeout,
			},
		})
		if err == nil {
			return c, nil
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("dial temporal %s: gave up after %d attempt(s): %w", hostPort, attempt, lastErr)
		case <-time.After(dialRetryInterval):
		}
	}
}
