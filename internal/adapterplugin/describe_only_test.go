package adapterplugin

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// fakeIgnoreEOF, as an argument to the test binary, makes TestMain run a fake
// adapter that answers initialize and describe and then ignores EOF on stdin.
const fakeIgnoreEOF = "fake-adapter-ignore-eof"

func runFakeIgnoringEOF() {
	r := bufio.NewReader(os.Stdin)
	for {
		payload, err := readFrame(r)
		if err != nil {
			time.Sleep(30 * time.Second) // ignore EOF: stay up until killed
			os.Exit(0)
		}
		var msg message
		if json.Unmarshal(payload, &msg) != nil || msg.ID == nil {
			continue
		}
		var result any = map[string]any{}
		if msg.Method == methodInitialize {
			result = InitializeResult{ProtocolVersion: ProtocolVersion, AdapterVersion: "0", Concurrency: 1}
		}
		raw, _ := json.Marshal(result)
		out, _ := json.Marshal(message{JSONRPC: "2.0", ID: msg.ID, Result: raw})
		_ = writeFrame(os.Stdout, out)
	}
}

// TestDescribeExecutableIgnoresEOF: an adapter that stays up after stdin
// closes is killed after the exit grace, and the describe succeeds well
// before the context's deadline.
func TestDescribeExecutableIgnoresEOF(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	start := time.Now()
	if _, err := DescribeExecutable(ctx, []string{self, fakeIgnoreEOF}, t.TempDir()); err != nil {
		t.Fatalf("DescribeExecutable: %v", err)
	}
	if d := time.Since(start); d > exitGrace+3*time.Second {
		t.Fatalf("took %v; want about the exit grace (%v)", d, exitGrace)
	}
}

// TestDescribeExecutable reads echo's describe without configure.
func TestDescribeExecutable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	desc, err := DescribeExecutable(ctx, []string{echoBin}, t.TempDir())
	if err != nil {
		t.Fatalf("DescribeExecutable: %v", err)
	}
	if len(desc.Verbs) != 1 || desc.SchemaSource == nil || desc.SchemaSource.Kind != SchemaSourceResponse {
		t.Fatalf("describe = %+v", desc)
	}
}

// TestDescribeExecutableStderr: a child that dies before answering fails with
// its stderr in the error.
func TestDescribeExecutableStderr(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := DescribeExecutable(ctx, []string{"/bin/sh", "-c", "echo adapter-boom >&2; exit 3"}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "adapter-boom") {
		t.Fatalf("err = %v; want one carrying the child's stderr", err)
	}
}

// TestDescribeExecutableDeadline: a child that never answers is stopped at
// the context's deadline.
func TestDescribeExecutableDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := DescribeExecutable(ctx, []string{"/bin/sh", "-c", "sleep 30"}, t.TempDir())
	if err == nil {
		t.Fatal("want a deadline error")
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("took %v; want the deadline to stop it", d)
	}
}
