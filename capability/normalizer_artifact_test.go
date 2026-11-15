package capability

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/sd-strax/reckon/identity"
	"github.com/sd-strax/reckon/stix"
)

// recordArtifactOnly checks that a result names nothing but the artifact that
// stands for the stored record (§4.13, adr/0357), and returns it. The artifact
// is the result's one SCO and the observation's one object_ref, with an
// extracted-from edge from the event.
func recordArtifactOnly(t *testing.T, evt OcsfEvent, res NormalizationResult) SCO {
	t.Helper()
	if len(res.SCOs) != 1 || res.SCOs[0].Type != identity.TypeArtifact {
		t.Fatalf("SCOs = %+v; want the record's artifact alone", res.SCOs)
	}
	a := res.SCOs[0]
	if len(res.ObservedData) != 1 {
		t.Fatalf("ObservedData = %d; want 1", len(res.ObservedData))
	}
	if refs := res.ObservedData[0].ObjectRefs; len(refs) != 1 || refs[0] != a.ID {
		t.Errorf("object_refs = %v; want the artifact %s alone", refs, a.ID)
	}
	edge := false
	for _, e := range res.Edges {
		if e.Type == EdgeExtractedFrom && e.SourceRef == a.ID && e.OcsfEventID == evt.ID.String() {
			edge = true
		}
	}
	if !edge {
		t.Errorf("edges = %+v; want an extracted-from edge from the artifact to the event", res.Edges)
	}
	return a
}

// checkRecordArtifact checks the artifact's shape: the url names the record
// by its SHA-256, the hash is the SHA-256 of the payload's canonical JSON, the type is JSON,
// and the id is minted from that hash under the tenant namespace.
func checkRecordArtifact(t *testing.T, evt OcsfEvent, a SCO) {
	t.Helper()
	if got := a.Properties["mime_type"]; got != "application/json" {
		t.Errorf("mime_type = %v; want application/json", got)
	}
	b, err := json.Marshal(evt.Payload)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	hash := hex.EncodeToString(sum[:])
	// The url names the record by its content, the same SHA-256 as its
	// hashes (adr/0357).
	if got, want := a.Properties["url"], "reckon://ocsf-record/sha256/"+hash; got != want {
		t.Errorf("url = %v; want %s", got, want)
	}
	hashes, _ := a.Properties["hashes"].(map[string]string)
	if len(hashes) != 1 || hashes["SHA-256"] != hash {
		t.Errorf("hashes = %v; want SHA-256 %s", a.Properties["hashes"], hash)
	}
	if id, _ := identity.NewResolver(testNS).Artifact(evt.Payload); a.ID != id {
		t.Errorf("id = %s; want %s, minted from the hash", a.ID, id)
	}
	obj, err := stix.Build(a.STIXObject())
	if err != nil {
		t.Fatalf("serialize the artifact: %v", err)
	}
	for _, k := range []string{"url", "hashes", "mime_type"} {
		if obj[k] == nil {
			t.Errorf("serialized artifact has no top-level %s: %v", k, obj)
		}
	}
}

// TestRecordArtifact (§4.13, adr/0357): a case record and an event the opaque
// default handles give no entity, so each observation lists the artifact that
// stands for the stored record. An event with an entity gets no artifact.
func TestRecordArtifact(t *testing.T) {
	reg := testRegistry()
	for name, payload := range map[string]map[string]any{
		"case record (2005)": casePayload(),
		"opaque (5021)": {
			"class_uid": 5021, "time": ms("2026-04-20T14:32:11Z"), "message": "a vendor row",
		},
	} {
		evt := makeEvent(payload)
		res, err := reg.Normalize(evt)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		checkRecordArtifact(t, evt, recordArtifactOnly(t, evt, res))
	}

	res, err := reg.Normalize(makeEvent(processPayload()))
	if err != nil {
		t.Fatal(err)
	}
	if n := len(scosOf(res, identity.TypeArtifact)); n != 0 {
		t.Errorf("an event with entities has %d artifacts; want none", n)
	}
}

// TestRecordArtifactIdentity: the artifact's id depends on the payload alone,
// within a tenant. The same record ingested twice is one artifact, and the
// same record in another tenant is another.
func TestRecordArtifactIdentity(t *testing.T) {
	p := casePayload()
	a, _ := identity.NewResolver(testNS).Artifact(p)
	b, _ := identity.NewResolver(testNS).Artifact(casePayload())
	if a != b {
		t.Errorf("the same payload minted %s and %s; want one id", a, b)
	}
	if other, _ := identity.NewResolver(testNS).Scoped("other").Artifact(p); other == a {
		t.Error("another namespace minted the same artifact id")
	}
	p["status"] = "Closed"
	if c, _ := identity.NewResolver(testNS).Artifact(p); c == a {
		t.Error("a changed payload minted the same artifact id")
	}
}
