// Command reckon-adapter-aws-security-lake is a NATIVE_API-class reckon adapter
// (design/11; adr/0322) that reads AWS Security Lake: the CloudTrail
// management-event table (OCSF API Activity, class 6003) through an Athena
// query-access subscriber, and the resource-context snapshot (OCSF Cloud
// Resources Inventory Info, class 5023) from AWS Config. It answers the three
// cloud control-plane verbs (03 §2.10): get_cloud_api_activity,
// get_cloud_resource_history and get_cloud_resource_context.
//
// It is a static Go binary — no runtime block, no child process — speaking the
// reckon plugin protocol on stdin/stdout, with the AWS SDK for Go v2 for
// signing, retries and assume-role refresh. Credentials come only from the
// resolved configure values; the default credential chain, shared
// configuration files and the instance metadata service are never consulted.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "reckon-adapter-"+adapterName+":", err)
		os.Exit(1)
	}
}

func run() error {
	b := newBridge()
	srv := &server{out: os.Stdout, bridge: b}
	if err := srv.serve(os.Stdin); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

// --- reckon plugin protocol (adapter side) ---

type message struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method,omitempty"`
	Params  json.RawMessage  `json:"params,omitempty"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   *wireError       `json:"error,omitempty"`
}

type wireError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// server reads Content-Length-framed JSON-RPC from the engine and dispatches to
// the bridge. One request at a time (the adapter declares concurrency 1).
type server struct {
	out    io.Writer
	bridge *bridge
}

func (s *server) serve(r io.Reader) error {
	br := bufio.NewReader(r)
	for {
		payload, err := readFrame(br)
		if err != nil {
			return err
		}
		var msg message
		if err := json.Unmarshal(payload, &msg); err != nil {
			continue
		}
		if msg.Method == "" || msg.ID == nil {
			continue // the engine only sends requests
		}
		result, rerr := s.bridge.handle(context.Background(), msg.Method, msg.Params)
		if rerr != nil {
			s.write(message{ID: msg.ID, Error: rerr})
			continue
		}
		raw, err := json.Marshal(result)
		if err != nil {
			s.write(message{ID: msg.ID, Error: &wireError{Code: -32603, Message: "marshal result: " + err.Error()}})
			continue
		}
		s.write(message{ID: msg.ID, Result: raw})
	}
}

func (s *server) write(msg message) {
	msg.JSONRPC = "2.0"
	payload, err := json.Marshal(msg)
	if err != nil {
		return
	}
	if _, err := fmt.Fprintf(s.out, "Content-Length: %d\r\n\r\n", len(payload)); err != nil {
		return
	}
	_, _ = s.out.Write(payload)
}

// readFrame reads one Content-Length framed message (11 §2).
func readFrame(r *bufio.Reader) ([]byte, error) {
	length := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if name, value, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return nil, fmt.Errorf("bad Content-Length %q", value)
			}
			length = n
		}
	}
	if length < 0 {
		return nil, fmt.Errorf("frame missing Content-Length")
	}
	payload := make([]byte, length)
	_, err := io.ReadFull(r, payload)
	return payload, err
}

// classErr builds a reckon-taxonomy error (data.class) for a read failure
// (03 §6.2).
func classErr(code int, msg, class string) *wireError {
	data, _ := json.Marshal(map[string]string{"class": class})
	return &wireError{Code: code, Message: msg, Data: data}
}
