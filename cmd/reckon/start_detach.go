package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/sd-strax/reckon/config"
	"github.com/sd-strax/reckon/internal/branding"
	"github.com/sd-strax/reckon/runtime"
	"github.com/sd-strax/reckon/server"
	"github.com/sd-strax/reckon/telemetry"
)

// `reckon start` (adr/0034-start-detach.md). Foreground is the default and
// stays the supervisor's natural shape: the process IS the stack, and a
// human reading the log live wants it on the console. --detach is the
// scripting shape: spawn that same foreground start as a background process
// in its own session, send its console to a file, and return once /status
// reports every component ready — so `stop → reset → start → seed → stop →
// start` can be a script.

type startOptions struct {
	detach  bool
	timeout time.Duration
}

func parseStartFlags(args []string) (startOptions, error) {
	fs := flag.NewFlagSet("start", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var o startOptions
	fs.BoolVar(&o.detach, "detach", false, "start the stack in the background and return once it is ready")
	fs.BoolVar(&o.detach, "d", false, "shorthand for --detach")
	fs.DurationVar(&o.timeout, "timeout", 3*time.Minute, "with --detach: how long to wait for the stack to be ready (first run downloads the bundled deps)")
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	if fs.NArg() > 0 {
		return o, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	return o, nil
}

func runStart(args []string) error {
	opts, err := parseStartFlags(args)
	if errors.Is(err, flag.ErrHelp) {
		return nil // usage already printed
	}
	if err != nil {
		return err
	}
	if !opts.detach {
		return runtime.Run(ossModuleBuilder)
	}
	return runStartDetached(opts.timeout)
}

// runStartDetached spawns `reckon start` detached and waits for readiness.
// Idempotent for scripts: if a supervisor is already running, it does not
// start a second one (the PID-file lock would refuse anyway) — it just waits
// for that one to be ready and reports it.
func runStartDetached(timeout time.Duration) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	pidPath := runtime.PIDFilePath(cfg)
	statusURL := fmt.Sprintf("http://localhost:%d/status", cfg.Backend.HTTPPort)

	if pid, alive := livePID(pidPath); alive {
		fmt.Printf("%s: supervisor already running (pid %d); waiting for it to be ready\n", branding.CLI, pid)
		return waitReady(statusURL, pid, "", nil, timeout)
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate own binary: %w", err)
	}
	logDir := telemetry.DefaultLogDir(cfg.Data.Dir)
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}
	logPath := filepath.Join(logDir, "start.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644) //nolint:gosec // operator-owned log under the data dir
	if err != nil {
		return fmt.Errorf("open %s: %w", logPath, err)
	}
	defer logFile.Close()
	_, _ = fmt.Fprintf(logFile, "\n=== %s start --detach at %s ===\n", branding.CLI, time.Now().UTC().Format(time.RFC3339))

	child := exec.Command(exe, "start") //nolint:gosec // re-executes this same binary with a fixed argument
	child.Stdout = logFile
	child.Stderr = logFile
	child.Stdin = nil
	child.Env = os.Environ()
	// Own session: the child survives this process (and its terminal) ending,
	// and a Ctrl-C here does not reach it.
	child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := child.Start(); err != nil {
		return fmt.Errorf("spawn %s start: %w", branding.CLI, err)
	}
	pid := child.Process.Pid
	fmt.Printf("%s: starting in the background (pid %d); console → %s\n", branding.CLI, pid, logPath)

	// Observe an early exit (bad config, port in use) so a failed start is
	// reported now, not as a timeout.
	exited := make(chan error, 1)
	go func() { exited <- child.Wait() }()

	return waitReady(statusURL, pid, logPath, exited, timeout)
}

// waitReady polls /status until Overall is "ok", the child exits, or the
// timeout passes. On timeout the child keeps running: the operator is told
// where to look, and the exit code says "not ready yet", not "failed".
func waitReady(statusURL string, pid int, logPath string, exited <-chan error, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		if ok, _ := statusOK(client, statusURL); ok {
			fmt.Printf("%s ready (pid %d): %s; %s stop to shut down\n", branding.CLI, pid, strings.TrimSuffix(statusURL, "/status"), branding.CLI)
			return nil
		}
		select {
		case err := <-exited:
			msg := "exited before becoming ready"
			if err != nil {
				msg = "exited before becoming ready: " + err.Error()
			}
			if logPath != "" {
				return fmt.Errorf("%s (see %s)", msg, logPath)
			}
			return errors.New(msg)
		case <-ticker.C:
		}
		if time.Now().After(deadline) {
			where := fmt.Sprintf("`%s status`", branding.CLI)
			if logPath != "" {
				where += " or " + logPath
			}
			return fmt.Errorf("not ready after %s; pid %d is still starting — follow %s, or retry with --timeout", timeout, pid, where)
		}
	}
}

// statusOK reports whether /status answers with every component ready.
func statusOK(client *http.Client, url string) (bool, error) {
	resp, err := client.Get(url) //nolint:gosec,noctx // localhost supervisor status; bounded by the client timeout
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	var st server.StatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		return false, err
	}
	return st.Overall == "ok", nil
}

// livePID reads the supervisor PID file and reports whether that process is
// alive (signal 0), mirroring the lock `reckon start` itself enforces.
func livePID(path string) (int, bool) {
	data, err := os.ReadFile(path) //nolint:gosec // the supervisor's own PID file under the data dir
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return pid, false
	}
	return pid, proc.Signal(syscall.Signal(0)) == nil
}
