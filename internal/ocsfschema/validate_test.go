package ocsfschema

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func paths(problems []Problem) []string {
	out := make([]string, 0, len(problems))
	for _, p := range problems {
		out = append(out, p.Path)
	}
	return out
}

func decode(t *testing.T, js string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(js), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// A field OCSF lacks at the declared version is reported, and one it has, or
// one under unmapped, is not.
func TestValidateEventUndefinedFields(t *testing.T) {
	event := decode(t, `{"class_uid":4009,"metadata":{"version":"1.1.0"},
		"email":{"from":"a@b.example","to":["c@d.example"],"files":[{"name":"x"}],"attachments":[]},
		"unmapped":{"bcc":["e@f.example"],"anything":{"at":["all"]}},
		"process":{"pid":1}}`)
	if got, want := paths(ValidateEvent(event)), []string{"email.attachments", "email.files", "process"}; !reflect.DeepEqual(got, want) {
		t.Errorf("problems at 1.1.0 = %v; want %v", got, want)
	}
	event["metadata"] = map[string]any{"version": "1.9.0"}
	if got, want := paths(ValidateEvent(event)), []string{"email.attachments", "process"}; !reflect.DeepEqual(got, want) {
		t.Errorf("problems at 1.9.0 = %v; want %v", got, want)
	}
}

// Each value's shape is checked: a list field holds a list, an object field
// an object, a scalar field a scalar.
func TestValidateShapes(t *testing.T) {
	s := mustLoad(t, "1.1.0")
	for _, tc := range []struct {
		name  string
		class int
		js    string
		want  []string // "path: fragment of the reason"
	}{
		{"well-formed", 3005, `{"privileges":["a","b"],"user":{"name":"jdoe","groups":[{"name":"g"}]},"time":1,"metadata":{"version":"1.1.0","product":{"name":"p"}}}`, nil},
		{"a list field holding a scalar", 3005, `{"privileges":"SeDebugPrivilege"}`, []string{"privileges: a list"}},
		{"a list field holding an object", 2004, `{"evidences":{"process":{"pid":1}}}`, []string{"evidences: a list"}},
		{"an object field holding a scalar", 3002, `{"user":"jdoe"}`, []string{"user: an object"}},
		{"an object field holding a list", 3002, `{"user":[{"name":"jdoe"}]}`, []string{"user: an object"}},
		{"a scalar field holding an object", 3002, `{"user":{"name":{"first":"j"}}}`, []string{"user.name: username_t"}},
		{"a scalar field holding a list", 3002, `{"user":{"name":["j"]}}`, []string{"user.name: username_t"}},
		{"a list of scalars holding an object", 3005, `{"privileges":["a",{"name":"b"}]}`, []string{"privileges[]: string_t"}},
		{"a list of objects holding a scalar", 3002, `{"user":{"groups":["g"]}}`, []string{"user.groups[]: an object"}},
		{"hashes in the object form", 1001, `{"file":{"name":"a","hashes":{"SHA-256":"ab"}}}`, []string{"file.hashes: a list"}},
		{"an unknown field inside a list entry", 2004, `{"evidences":[{"process":{"pid":1},"device":{"hostname":"h"}}]}`, []string{"evidences[].device: does not define"}},
		{"json_t accepts anything", 2004, `{"evidences":[{"data":{"any":["thing"]}},{"data":"text"},{"data":[1]}]}`, nil},
		{"unmapped accepts anything", 3002, `{"unmapped":{"a":{"b":[1,{"c":2}]}}}`, nil},
		{"null is an absent field", 3002, `{"user":null,"unmapped":null,"time":null}`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := s.Validate(tc.class, decode(t, tc.js))
			if len(got) != len(tc.want) {
				t.Fatalf("problems = %v; want %v", got, tc.want)
			}
			for i, w := range tc.want {
				path, frag, _ := strings.Cut(w, ": ")
				if got[i].Path != path || !strings.Contains(got[i].Reason, frag) {
					t.Errorf("problem %d = %q; want %s with %q", i, got[i], path, frag)
				}
			}
		})
	}
}

// An event that declares no version, an unknown one, or no class is a
// problem by itself.
func TestValidateEventNeedsVersionAndClass(t *testing.T) {
	for name, tc := range map[string]struct{ js, path string }{
		"no metadata":     {`{"class_uid":3002}`, "metadata.version"},
		"no version":      {`{"class_uid":3002,"metadata":{"product":{"name":"p"}}}`, "metadata.version"},
		"unknown version": {`{"class_uid":3002,"metadata":{"version":"0.9.0"}}`, "metadata.version"},
		"no class":        {`{"metadata":{"version":"1.1.0"}}`, "class_uid"},
		"unknown class":   {`{"class_uid":5023,"metadata":{"version":"1.1.0"}}`, ""},
	} {
		got := ValidateEvent(decode(t, tc.js))
		if len(got) != 1 || got[0].Path != tc.path {
			t.Errorf("%s: problems = %v; want one at %q", name, got, tc.path)
		}
	}
	if got := ValidateEvent(decode(t, `{"class_uid":5023,"metadata":{"version":"1.4.0"}}`)); len(got) != 0 {
		t.Errorf("a 1.4.0 Cloud Resources Inventory event: %v", got)
	}
}

// An adapter passes the event it built, with Go's typed maps and slices.
func TestValidateTypedGoValues(t *testing.T) {
	event := map[string]any{
		"class_uid":  3005,
		"metadata":   map[string]any{"version": "1.1.0"},
		"user":       map[string]string{"name": "jdoe"},
		"privileges": []string{"a"},
		"resource":   []map[string]any{{"name": "x"}},
		"actor":      map[string]any{"user": map[string]any{"groups": []map[string]any{{"name": "g", "nope": 1}}}},
	}
	got := ValidateEvent(event)
	if want := []string{"actor.user.groups[].nope", "resource"}; !reflect.DeepEqual(paths(got), want) {
		t.Errorf("problems = %v; want %v", got, want)
	}
}

// Every path Validate accepts, Resolve accepts too: the two agree on what a
// class defines.
func TestValidateAgreesWithResolve(t *testing.T) {
	s := mustLoad(t, Ceiling)
	event := decode(t, `{"device":{"hostname":"h","agent_list":[{"uid":"a"}],"nope":1},"resources":[{"zone":"z","group":{"name":"g"}}],"cloud":{"account":{"uid":"1"}},"nope":{"x":1}}`)
	bad := map[string]bool{}
	for _, p := range s.Validate(6003, event) {
		bad[p.Path] = true
	}
	for path, defined := range map[string]bool{
		"device.hostname": true, "device.agent_list[].uid": true, "device.nope": false,
		"resources[].zone": true, "resources[].group.name": true, "cloud.account.uid": true, "nope": false,
	} {
		if s.HasPath(6003, path) != defined || bad[path] == defined {
			t.Errorf("%s: Resolve says defined=%v, Validate reported=%v; want defined=%v", path, s.HasPath(6003, path), bad[path], defined)
		}
	}
}
