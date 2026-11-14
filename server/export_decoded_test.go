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
	if got := decodedSuffix(ocsfview.View{}); got != "" {
		t.Errorf("no decoded = %q; want nothing", got)
	}
	v := ocsfview.View{Decoded: &ocsfview.Decoded{Field: "process.cmd_line", Stored: encodedCmdLine, Encoding: ocsfview.EncodingBase64UTF16LE, Text: "IEX (New-Object Net.WebClient)"}}
	want := " · process.cmd_line: " + encodedCmdLine + " · decoded (base64-utf16le): IEX (New-Object Net.WebClient)"
	if got := decodedSuffix(v); got != want {
		t.Errorf("suffix = %q\n   want %q", got, want)
	}
	v.Decoded.Text = strings.Repeat("a", 500)
	v.Decoded.Stored = strings.Repeat("b", 2000)
	got := decodedSuffix(v)
	if !strings.Contains(got, strings.Repeat("b", exportStoredMax-1)+"…") || strings.Contains(got, strings.Repeat("b", exportStoredMax)) {
		t.Errorf("stored value not clipped to %d runes: %d", exportStoredMax, len([]rune(got)))
	}
	if !strings.HasSuffix(got, strings.Repeat("a", exportDecodedMax-1)+"…") {
		t.Errorf("decoded text not clipped to %d runes with an ellipsis", exportDecodedMax)
	}
	// A view with no stored value names no field rather than an empty one.
	v.Decoded.Stored = ""
	if got := decodedSuffix(v); strings.Contains(got, "process.cmd_line:") {
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

// TestRefLabeler_DecodedJobAction (adr/0356): from OCSF 1.9.0 a scheduled
// job's command line is one of its actions. The export line names the action
// the command line was read from and shows it as stored, whichever action it
// was, and names job.cmd_line when no action carries one.
func TestRefLabeler_DecodedJobAction(t *testing.T) {
	jobEvent := func(id uuid.UUID, job string) evidence.Event {
		return evidence.Event{
			ID: id, ClassUID: 1006, ClassName: "Scheduled Job Activity",
			Time:    time.Date(2001, 1, 1, 10, 0, 0, 0, time.UTC),
			Payload: []byte(`{"activity_id":1,"activity_name":"Create","job":` + job + `}`),
		}
	}
	tests := []struct {
		name  string
		job   string
		field string
	}{
		{"the first action", `{"name":"Updater","job_actions":[{"cmd_line":"` + encodedCmdLine + `"},{"cmd_line":"b.exe"}]}`, "job.job_actions[0].cmd_line"},
		{"a later action", `{"name":"Updater","job_actions":[{"desc":"send mail"},{"cmd_line":"` + encodedCmdLine + `"}]}`, "job.job_actions[1].cmd_line"},
		{"no action has a command", `{"name":"Updater","cmd_line":"` + encodedCmdLine + `","job_actions":[{"desc":"send mail"}]}`, "job.cmd_line"},
		{"the deprecated field alone", `{"name":"Updater","cmd_line":"` + encodedCmdLine + `"}`, "job.cmd_line"},
	}
	for _, tt := range tests {
		id := uuid.New()
		got := newRefLabeler(context.Background(), decodedEvidence{event: jobEvent(id, tt.job)}).label(id.String())
		for _, want := range []string{"Updater", " · " + tt.field + ": " + encodedCmdLine, "decoded (base64-utf16le): echo"} {
			if !strings.Contains(got, want) {
				t.Errorf("%s: label %q lacks %q", tt.name, got, want)
			}
		}
		if strings.Index(got, encodedCmdLine) > strings.Index(got, "decoded (") {
			t.Errorf("%s: the stored command line does not precede its decode: %q", tt.name, got)
		}
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
