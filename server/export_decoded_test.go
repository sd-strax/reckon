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
// its field, and the decoded text with its encoding named, on one line
// (adr/0300); a view without one adds nothing. The stored value is the fact
// and goes whole; the decoded text is clipped.
func TestDecodedSuffix(t *testing.T) {
	if got := decodedSuffix(ocsfview.View{}, nil); got != "" {
		t.Errorf("no decoded = %q; want nothing", got)
	}
	payload := []byte(`{"process":{"cmd_line":"` + encodedCmdLine + `"}}`)
	v := ocsfview.View{Decoded: &ocsfview.Decoded{Field: "process.cmd_line", Encoding: ocsfview.EncodingBase64UTF16LE, Text: "IEX (New-Object\n Net.WebClient)"}}
	want := " · process.cmd_line: " + encodedCmdLine + " · decoded (base64-utf16le): IEX (New-Object Net.WebClient)"
	if got := decodedSuffix(v, payload); got != want {
		t.Errorf("suffix = %q\n   want %q", got, want)
	}
	// Both texts are the attacker's and land in a code span: a backtick would
	// close it.
	v.Decoded.Text = "echo `whoami`; <img src=x>"
	hostile := []byte(`{"process":{"cmd_line":"sh -c \"echo ` + "`id`" + `\""}}`)
	got := decodedSuffix(v, hostile)
	if strings.Contains(got, "`") || !strings.Contains(got, "echo 'whoami'; <img src=x>") || !strings.Contains(got, `sh -c "echo 'id'"`) {
		t.Errorf("backticks survive into the code span: %q", got)
	}
	v.Decoded.Text = strings.Repeat("a", 500)
	got = decodedSuffix(v, nil)
	if len([]rune(got)) != len([]rune(" · decoded (base64-utf16le): "))+exportDecodedMax || !strings.HasSuffix(got, "…") {
		t.Errorf("long text not clipped to %d runes with an ellipsis: %d", exportDecodedMax, len([]rune(got)))
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

// TestRefLabeler_DecodedBesideTheStoredCommandLine: the cited-event line an
// export renders carries the command line as stored and its decoded text,
// since the sentence names the program and never the command line.
func TestRefLabeler_DecodedBesideTheStoredCommandLine(t *testing.T) {
	id := uuid.New()
	store := decodedEvidence{event: evidence.Event{
		ID: id, ClassUID: 1007, ClassName: "Process Activity",
		Time:    time.Date(2001, 1, 1, 10, 0, 0, 0, time.UTC),
		Payload: []byte(`{"activity_id":1,"process":{"name":"powershell.exe","pid":4242,"cmd_line":"` + encodedCmdLine + `"}}`),
	}}
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
