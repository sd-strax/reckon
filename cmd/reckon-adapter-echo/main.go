// Command reckon-adapter-echo is a reference out-of-process adapter: a minimal,
// self-contained implementation of the reckon plugin protocol
// (design/11-adapter-plugins.md §4) that depends on nothing in the engine. It
// exists to (1) back the adapterplugin host tests with a real child process and
// (2) serve as the smallest possible worked example of "write a process, speak
// the protocol" (§1) — the conformance reference the `reckon adapter test` verb
// (§7) will later exercise.
//
// It speaks JSON-RPC 2.0 over Content-Length-framed stdio and answers the whole
// handshake + serve surface. Behavior is driven by operation name so the tests
// can exercise the success paths, the 03 §6.2 / 08 §5 error taxonomies, and
// crash-restart supervision without any engine coupling:
//
//	invoke  get_host_context  → one OCSF event echoing the params
//	invoke  boom_fallthrough  → error with data.class = FALLTHROUGH
//	invoke  crash             → the process exits (tests respawn)
//	dispatch <any>            → SUCCEEDED WriteResult, one target OK
//	dispatch boom_fatal       → error with data.class = FATAL_ERROR
//	configure {reject:true}   → rejects (tests handshake → UNHEALTHY)
//	configure {degraded:true} → health reports one degraded capability (11 §4.4)
//	configure {degraded_value:v} → health sends v verbatim as degraded (malformed-list tests)
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

func main() {
	a := &adapter{out: os.Stdout}
	if err := a.serve(os.Stdin); err != nil && !errors.Is(err, io.EOF) {
		fmt.Fprintln(os.Stderr, "reckon-adapter-echo:", err)
		os.Exit(1)
	}
}

type adapter struct {
	out      io.Writer
	degraded bool // configure {degraded:true}: health reports a degraded capability
	// degradedValue, from configure {degraded_value:v}, is sent verbatim as
	// health's degraded field.
	degradedValue any
}

// --- JSON-RPC envelope ---

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

func (a *adapter) serve(r io.Reader) error {
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
			continue // we only receive requests
		}
		result, rerr := a.handle(msg.Method, msg.Params)
		if rerr != nil {
			a.write(message{JSONRPC: "2.0", ID: msg.ID, Error: rerr})
			continue
		}
		raw, _ := json.Marshal(result)
		a.write(message{JSONRPC: "2.0", ID: msg.ID, Result: raw})
	}
}

func (a *adapter) handle(method string, params json.RawMessage) (any, *wireError) {
	switch method {
	case "initialize":
		return map[string]any{
			"protocol_version": 1,
			"adapter_version":  "0.0.1",
			"concurrency":      1,
		}, nil
	case "describe":
		return describeResult(), nil
	case "configure":
		var p struct {
			Config map[string]any `json:"config"`
		}
		_ = json.Unmarshal(params, &p)
		if v, ok := p.Config["reject"].(bool); ok && v {
			return nil, &wireError{Code: -32000, Message: "configure rejected by test config"}
		}
		a.degraded, _ = p.Config["degraded"].(bool)
		a.degradedValue = p.Config["degraded_value"]
		return map[string]any{"ok": true}, nil
	case "invoke":
		return a.invoke(params)
	case "dispatch":
		return a.dispatch(params)
	case "health":
		if a.degradedValue != nil {
			return map[string]any{"healthy": true, "message": "ok", "degraded": a.degradedValue}, nil
		}
		if a.degraded {
			return map[string]any{"healthy": true, "message": "ok", "degraded": []map[string]any{{
				"operation":  "get_host_context",
				"capability": "host filter",
				"reason":     "degraded by test config",
			}}}, nil
		}
		return map[string]any{"healthy": true, "message": "ok"}, nil
	default:
		return nil, &wireError{Code: -32601, Message: "unknown method " + method}
	}
}

func (a *adapter) invoke(params json.RawMessage) (any, *wireError) {
	var p struct {
		Operation string         `json:"operation"`
		Params    map[string]any `json:"params"`
	}
	_ = json.Unmarshal(params, &p)
	switch p.Operation {
	case "boom_fallthrough":
		return nil, classErr("no data for this target", "FALLTHROUGH")
	case "crash":
		os.Exit(1)
	}
	// Echo the params into one synthetic OCSF Authentication (3002) event.
	// The event is OCSF (adr/0356): it says its class and version, and the
	// echoed operation and params, which OCSF has no field for, ride under
	// unmapped, where OCSF puts a source's own fields.
	return map[string]any{
		"source_tool": "echo:" + p.Operation,
		"events": []map[string]any{{
			"class_uid":  3002,
			"class_name": "Authentication",
			"time":       time.Unix(0, 0).UTC(),
			"raw": map[string]any{
				"class_uid": 3002,
				"metadata": map[string]any{
					"version": ocsfVersion,
					"product": map[string]any{"vendor_name": "reckon", "name": "echo"},
				},
				"unmapped": map[string]any{"operation": p.Operation, "echo": p.Params},
			},
		}},
	}, nil
}

// ocsfVersion is the OCSF release the echoed event is written in: the oldest
// one the engine reads.
const ocsfVersion = "1.1.0"

func (a *adapter) dispatch(params json.RawMessage) (any, *wireError) {
	var p struct {
		Operation      string         `json:"operation"`
		Params         map[string]any `json:"params"`
		IdempotencyKey string         `json:"idempotency_key"`
	}
	_ = json.Unmarshal(params, &p)
	if p.Operation == "boom_fatal" {
		return nil, classErr("target not found", "FATAL_ERROR")
	}
	return map[string]any{
		"final_outcome":      "SUCCEEDED",
		"per_target_results": map[string]any{"target-0": "OK"},
		"adapter_request_id": "echo-" + p.IdempotencyKey,
		"audit_depth":        "FULL",
	}, nil
}

func classErr(msg, class string) *wireError {
	data, _ := json.Marshal(map[string]any{"class": class})
	return &wireError{Code: -32000, Message: msg, Data: data}
}

func describeResult() map[string]any {
	return map[string]any{
		"verbs": []map[string]any{{
			"verb":   "get_host_context",
			"intent": "Echo host context for testing.",
			"inputs": []map[string]any{{"name": "host", "type": "entity", "required": true}},
			"output": "host context",
		}},
		"action_types": []map[string]any{{
			"action_type":   "host.isolate",
			"intent":        "Echo host isolation for testing.",
			"default_tier":  "T3",
			"reversibility": "reversible",
			"reversible_by": "host.unisolate",
		}},
		"operations": []map[string]any{
			{"name": "get_host_context", "params": map[string]any{"type": "object"}},
			{"name": "host.isolate", "params": map[string]any{"type": "object"}},
		},
		"config_schema": map[string]any{"type": "object"},
		// It decodes nothing it does not own: its fields are its own echo, the
		// response kind (11 §4.4).
		"schema_source": map[string]any{"kind": "response"},
	}
}

func (a *adapter) write(msg message) {
	msg.JSONRPC = "2.0"
	payload, err := json.Marshal(msg)
	if err != nil {
		return
	}
	if _, err := fmt.Fprintf(a.out, "Content-Length: %d\r\n\r\n", len(payload)); err != nil {
		return
	}
	_, _ = a.out.Write(payload)
}

// readFrame reads one Content-Length framed message (§2 transport).
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
