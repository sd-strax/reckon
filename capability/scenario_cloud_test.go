package capability

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sd-strax/reckon/identity"
)

// TestScenario_ExposedBucket_Normalizes loads every OCSF event of the
// cloud-rooted scenario and normalizes it: each of the 19 API-call files (ten
// baseline calls, seven incident calls, two history copies) is a typed
// observation, the principal, the account and each resource stitch to one id
// across the events that name them, the denied call carries its error, and no
// resource identified by its uid is asserted to belong to the event's account
// (every resource in the scenario has a uid). The scenario's verbs are wired
// separately; this guards the data and the normalizer together.
func TestScenario_ExposedBucket_Normalizes(t *testing.T) {
	files, err := filepath.Glob("../fixtures/exposed-bucket-via-access-key/*.json")
	if err != nil || len(files) == 0 {
		t.Fatalf("fixtures: %v (%d files)", err, len(files))
	}
	reg := testRegistry()

	principals := map[identity.STIXID]bool{}
	accounts := map[identity.STIXID]bool{}
	resourceIDs := map[string]map[identity.STIXID]bool{} // resource name → ids
	var uidBelongs []identity.STIXID
	apiFiles := map[string]int{} // file prefix → 6003 events
	var denied int
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Ocsf map[string]any `json:"ocsf"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if doc.Ocsf == nil {
			continue
		}
		res, err := reg.Normalize(makeEvent(doc.Ocsf))
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if toInt(doc.Ocsf["class_uid"]) != 6003 {
			continue
		}
		base := filepath.Base(f)
		prefix, _, _ := strings.Cut(base, "-")
		apiFiles[prefix]++
		if len(res.SCOs) == 0 || len(res.ObservedData) != 1 || res.ObservedData[0].Provenance.Normalizer != "api_activity" {
			t.Errorf("%s: opaque or not api_activity: %d SCOs, %+v", base, len(res.SCOs), res.ObservedData)
			continue
		}
		for _, s := range res.SCOs {
			switch s.Type {
			case identity.TypeUserAccount:
				if s.Properties["user_id"] == "arn:aws:iam::111122223333:user/deploy-ci" {
					principals[s.ID] = true
				}
			case identity.TypeCloudAccount:
				accounts[s.ID] = true
			case identity.TypeCloudResource:
				name, _ := s.Properties["name"].(string)
				if resourceIDs[name] == nil {
					resourceIDs[name] = map[identity.STIXID]bool{}
				}
				resourceIDs[name][s.ID] = true
				if s.Properties["uid"] == nil {
					t.Errorf("%s: resource %s has no uid; the scenario names every resource by uid", base, s.ID)
				}
			}
		}
		for _, r := range res.Relationships {
			if r.Type == RelBelongsTo && strings.HasPrefix(string(r.SourceRef), identity.TypeCloudResource+"--") {
				uidBelongs = append(uidBelongs, r.SourceRef)
			}
		}
		ext := res.ObservedData[0].Extensions
		if base == "api-denied-1.json" {
			denied++
			if ext["error"] != "AccessDenied" || ext["status"] != "Failure" {
				t.Errorf("denied call ext = %v; want error AccessDenied, status Failure", ext)
			}
		}
	}
	if apiFiles["api"] != 17 || apiFiles["history"] != 2 || len(apiFiles) != 2 {
		t.Errorf("API-call files by prefix = %v; want api 17, history 2", apiFiles)
	}
	if denied != 1 {
		t.Errorf("denied calls seen = %d; want 1", denied)
	}
	if len(principals) != 1 {
		t.Errorf("deploy-ci principals = %v; want one", principals)
	}
	if len(accounts) != 1 {
		t.Errorf("accounts = %v; want one", accounts)
	}
	for _, name := range []string{"acme-internal-exports", "svc-backup", "acme-release-artifacts"} {
		if len(resourceIDs[name]) != 1 {
			t.Errorf("resource %s ids = %v; want one", name, resourceIDs[name])
		}
	}
	if len(uidBelongs) != 0 {
		t.Errorf("uid-identified resources with a belongs-to edge: %v; want none", uidBelongs)
	}
}
