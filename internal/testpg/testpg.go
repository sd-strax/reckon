// Package testpg holds what the per-package embedded-Postgres TestMains share.
package testpg

import (
	"fmt"
	"net"
)

// FreePort returns a loopback TCP port that was free a moment ago, for an
// embedded Postgres to bind. A fixed port meant one orphaned instance (a test
// binary that panicked and skipped TestMain's teardown) blocked every later
// run of the package; a fresh port per run means it only leaks until stopped
// (scripts/stop-test-pg.sh). The port is released before Postgres binds it, so
// another process could take it in between; the start then fails loudly.
func FreePort() (uint32, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("free port: %w", err)
	}
	defer func() { _ = l.Close() }()
	addr, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("free port: unexpected address %v", l.Addr())
	}
	return uint32(addr.Port), nil //nolint:gosec // a TCP port fits in uint32
}

// DSN is the connection string for a test database on the loopback port.
func DSN(port uint32, db string) string {
	return fmt.Sprintf("host=localhost port=%d user=test password=test dbname=%s sslmode=disable", port, db)
}
