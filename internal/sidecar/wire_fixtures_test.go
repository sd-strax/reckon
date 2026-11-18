package sidecar

import (
	"reflect"
	"testing"

	"github.com/sd-strax/reckon/internal/apiversion"
	"github.com/sd-strax/reckon/internal/wirefixture"
	"github.com/sd-strax/reckon/internal/wirets"
)

const (
	sidecarFixtures = "../../workbench/src/wire/fixtures/sidecar"
	sidecarShapes   = "../../workbench/src/wire/sidecarShapes.generated.ts"
)

// TestWireFixtures keeps the sample of each result and notification the
// workbench decodes, and the two versions it checks (adr/0244). It also
// keeps the generated TypeScript for the same types, the params and the
// protocol version (adr/0278): make wire-fixtures writes both. The API
// version is generated with the backend's shapes.
func TestWireFixtures(t *testing.T) {
	dir := sidecarFixtures
	wirefixture.Check[initializeResult](t, dir, "initialize")
	wirefixture.Check[createSessionResult](t, dir, "createSession")
	wirefixture.Check[turnResult](t, dir, "turn")
	wirefixture.Check[proposeSOPResult](t, dir, "proposeSOP")
	wirefixture.Check[draftNarrativeResult](t, dir, "draftNarrative")
	wirefixture.Check[summarizeConcludedResult](t, dir, "summarizeConcluded")
	wirefixture.Check[turnTextNote](t, dir, "text")
	wirefixture.Check[turnStepNote](t, dir, "step")
	wirefixture.Check[turnToolCallNote](t, dir, "toolCall")
	wirefixture.Check[turnToolResultNote](t, dir, "toolResult")
	wirefixture.Check[getTokenParams](t, dir, "getToken")
	wirefixture.CheckValue(t, dir, "versions", struct {
		Protocol int `json:"protocol"`
		API      int `json:"api"`
	}{ProtocolVersion, apiversion.Current})
	checkWireValues(t)
	wirefixture.RequireWordsChecked(t)

	src, err := sidecarGenerator().Generate()
	if err != nil {
		t.Fatal(err)
	}
	wirefixture.CheckFile(t, sidecarShapes, src)
}

// sidecarGenerator registers every shape on the workbench ↔ sidecar
// connection. A decoder's name is its sample's name, which the workbench's
// fixture test holds one to one.
func sidecarGenerator() *wirets.Generator {
	g := wirets.New("internal/sidecar")
	g.Const("PROTOCOL_VERSION", ProtocolVersion)

	// What the sidecar sends: results, notifications, and getToken's params
	// (the one request the sidecar makes of the workbench).
	g.Decoder("initialize", reflect.TypeFor[initializeResult]())
	g.Decoder("createSession", reflect.TypeFor[createSessionResult]())
	g.Decoder("turn", reflect.TypeFor[turnResult]())
	g.Decoder("proposeSOP", reflect.TypeFor[proposeSOPResult]())
	g.Decoder("draftNarrative", reflect.TypeFor[draftNarrativeResult]())
	g.Decoder("summarizeConcluded", reflect.TypeFor[summarizeConcludedResult]())
	g.Decoder("text", reflect.TypeFor[turnTextNote]())
	g.Decoder("step", reflect.TypeFor[turnStepNote]())
	g.Decoder("toolCall", reflect.TypeFor[turnToolCallNote]())
	g.Decoder("toolResult", reflect.TypeFor[turnToolResultNote]())
	g.Decoder("getToken", reflect.TypeFor[getTokenParams]())

	// What the workbench sends: every method's params, and getToken's answer.
	g.Type(reflect.TypeFor[initializeParams]())
	g.Type(reflect.TypeFor[createSessionParams]())
	g.Type(reflect.TypeFor[turnParams]())
	g.Type(reflect.TypeFor[cancelParams]())
	g.Type(reflect.TypeFor[resetSessionParams]())
	g.Type(reflect.TypeFor[summarizeConcludedParams]())
	g.Type(reflect.TypeFor[draftNarrativeParams]())
	g.Type(reflect.TypeFor[proposeSOPParams]())
	g.Type(reflect.TypeFor[closeSessionParams]())
	g.Type(reflect.TypeFor[getTokenResult]())
	return g
}
