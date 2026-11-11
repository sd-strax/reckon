package adapterplugin

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// exitGrace is how long DescribeExecutable waits for the adapter to exit after
// stdin closes before it kills it.
const exitGrace = 2 * time.Second

// maxStderrBytes bounds the stderr DescribeExecutable keeps for its error.
const maxStderrBytes = 8 << 10

// DescribeExecutable spawns an adapter executable and runs only the first two
// handshake steps, initialize and describe (§4.1–§4.2), then closes its stdin.
// It never sends configure, so it needs no instance config and no credentials:
// it is for reading what an adapter describes, not for serving it. The child
// runs in dir with the same minimal environment the host gives a plugin (§2).
// ctx bounds the whole exchange; without a deadline the handshake timeout
// applies. On failure the error carries the tail of the child's stderr.
func DescribeExecutable(ctx context.Context, argv []string, dir string) (*DescribeResult, error) {
	if len(argv) == 0 {
		return nil, errors.New("describe: no executable")
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, handshakeTimeout)
		defer cancel()
	}
	lifeCtx, kill := context.WithCancel(ctx)
	defer kill()

	cmd := exec.CommandContext(lifeCtx, argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Env = minimalEnv()
	stderr := &tailBuffer{max: maxStderrBytes}
	cmd.Stderr = stderr
	// A grandchild holding stderr open must not hold Wait past the kill.
	cmd.WaitDelay = time.Second
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("describe: open stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("describe: open stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("describe: start %s: %w", argv[0], err)
	}

	c := newConn(stdin)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = c.runOn(stdout)
	}()
	finish := func(callErr error) error {
		_ = stdin.Close()
		if callErr != nil {
			kill()
		}
		waited := make(chan error, 1)
		go func() { waited <- cmd.Wait() }()
		var waitErr error
		select {
		case waitErr = <-waited:
		case <-time.After(exitGrace):
			// An adapter that ignores EOF is stopped, not waited on.
			kill()
			waitErr = <-waited
		}
		<-done
		if callErr == nil {
			return nil
		}
		if tail := strings.TrimSpace(stderr.String()); tail != "" {
			return fmt.Errorf("%w (stderr: %s)", callErr, tail)
		}
		if waitErr != nil {
			return fmt.Errorf("%w (exit: %w)", callErr, waitErr)
		}
		return callErr
	}

	var init InitializeResult
	if err := c.Call(ctx, methodInitialize, InitializeParams{
		EngineProtocolVersions: []int{ProtocolVersion},
		EngineVersion:          "describe",
	}, &init); err != nil {
		return nil, finish(fmt.Errorf("initialize: %w", err))
	}
	if init.ProtocolVersion != ProtocolVersion {
		return nil, finish(fmt.Errorf("initialize: adapter negotiated protocol %d, engine speaks %d", init.ProtocolVersion, ProtocolVersion))
	}
	var desc DescribeResult
	if err := c.Call(ctx, methodDescribe, struct{}{}, &desc); err != nil {
		return nil, finish(fmt.Errorf("describe: %w", err))
	}
	return &desc, finish(nil)
}

// tailBuffer keeps the last max bytes written to it; safe for the concurrent
// writer exec runs and a reader after Wait.
type tailBuffer struct {
	mu  sync.Mutex
	max int
	buf bytes.Buffer
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf.Write(p)
	if over := t.buf.Len() - t.max; over > 0 {
		t.buf.Next(over)
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.buf.String()
}
