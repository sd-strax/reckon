package server

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/sd-strax/reckon/capability/ocsfview"
	"github.com/sd-strax/reckon/evidence"
)

const encodedCmdLine = "powershell.exe -NoProfile -EncodedCommand ZQBjAGgAbwA="

// TestDecodedSuffix: an export line carries the stored command line, named by
// its field, and the decoded text with its encoding named (adr/0300); a view
// without one adds nothing. Both are clipped, the stored value at the larger
// bound.
func TestDecodedSuffix(t *testing.T) {
	if got := decodedSuffix(ocsfview.View{}, nil); got != "" {
		t.Errorf("no decoded = %q; want nothing", got)
	}
	payload := []byte(`{"process":{"cmd_line":"` + encodedCmdLine + `"}}`)
	v := ocsfview.View{Decoded: &ocsfview.Decoded{Field: "process.cmd_line", Encoding: ocsfview.EncodingBase64UTF16LE, Text: "IEX (New-Object Net.WebClient)"}}
	want := " · process.cmd_line: " + encodedCmdLine + " · decoded (base64-utf16le): IEX (New-Object Net.WebClient)"
	if got := decodedSuffix(v, payload); got != want {
		t.Errorf("suffix = %q\n   want %q", got, want)
	}
	v.Decoded.Text = strings.Repeat("a", 500)
	long := []byte(`{"process":{"cmd_line":"` + strings.Repeat("b", 2000) + `"}}`)
	got := decodedSuffix(v, long)
	if !strings.Contains(got, strings.Repeat("b", exportStoredMax-1)+"…") || strings.Contains(got, strings.Repeat("b", exportStoredMax)) {
		t.Errorf("stored value not clipped to %d runes: %d", exportStoredMax, len([]rune(got)))
	}
	if !strings.HasSuffix(got, strings.Repeat("a", exportDecodedMax-1)+"…") {
		t.Errorf("decoded text not clipped to %d runes with an ellipsis", exportDecodedMax)
	}
	// A stored value the payload no longer carries under that path is omitted
	// rather than rendered empty.
	if got := decodedSuffix(v, []byte(`{"process":{}}`)); strings.Contains(got, "process.cmd_line:") {
		t.Errorf("empty stored value rendered: %q", got)
	}
}

// decodedEvidence serves one process event and no entities, so the labeler
// runs its whole path: the sentence, the stored command line, the decode.
type decodedEvidence struct {
	evidenceStore
	event evidence.Event
}

func (s decodedEvidence) Event(context.Context, uuid.UUID) (evidence.Event, error) {
	return s.event, nil
}

func (s decodedEvidence) EventEntities(context.Context, uuid.UUID) ([]evidence.Object, error) {
	return nil, nil
}

func processEvent(id uuid.UUID, process string) evidence.Event {
	return evidence.Event{
		ID: id, ClassUID: 1007, ClassName: "Process Activity",
		Time:    time.Date(2001, 1, 1, 10, 0, 0, 0, time.UTC),
		Payload: []byte(`{"activity_id":1,"process":` + process + `}`),
	}
}

// TestRefLabeler_DecodedBesideTheStoredCommandLine: the cited-event line an
// export renders carries the command line as stored and its decoded text,
// since the sentence names the program and never the command line.
func TestRefLabeler_DecodedBesideTheStoredCommandLine(t *testing.T) {
	id := uuid.New()
	store := decodedEvidence{event: processEvent(id, `{"name":"powershell.exe","pid":4242,"cmd_line":"`+encodedCmdLine+`"}`)}
	got := newRefLabeler(context.Background(), store).label(id.String())
	for _, want := range []string{
		"powershell.exe",
		"process.cmd_line: " + encodedCmdLine,
		"decoded (base64-utf16le): echo",
		"@ 10:00:00Z",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("label %q lacks %q", got, want)
		}
	}
	if strings.Index(got, encodedCmdLine) > strings.Index(got, "decoded (") {
		t.Errorf("the stored command line does not precede its decode: %q", got)
	}
}

// TestRefLabeler_SpanSafe: a cited label lands in a backtick code span, and
// every part of it is telemetry text: a backtick in the process name, the
// command line or the decoded text would close the span, a line break would
// break the row, so none survives where the backticks are added; the label
// itself stays as the telemetry has it, for the surfaces without a span.
func TestRefLabeler_SpanSafe(t *testing.T) {
	id := uuid.New()
	// "echo `whoami`" as base64 of UTF-16LE.
	store := decodedEvidence{event: processEvent(id, `{"name":"a`+"`"+`b.exe\nc","pid":1,"cmd_line":"powershell.exe -enc ZQBjAGgAbwAgAGAAdwBoAG8AYQBtAGkAYAA="}`)}
	rl := newRefLabeler(context.Background(), store)
	if raw := rl.label(id.String()); !strings.Contains(raw, "`") {
		t.Fatalf("the label itself should carry the telemetry text as is: %q", raw)
	}
	got := labeledList(rl, []string{id.String()})
	if strings.ContainsAny(strings.Trim(got, "`"), "`\n") {
		t.Errorf("backtick or line break survives into the code span: %q", got)
	}
	for _, want := range []string{"a'b.exe c", "decoded (base64-utf16le): echo 'whoami'"} {
		if !strings.Contains(got, want) {
			t.Errorf("label %q lacks %q", got, want)
		}
	}
}
