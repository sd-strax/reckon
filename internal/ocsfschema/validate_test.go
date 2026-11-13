package ocsfschema

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
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
	if got, want := paths(ValidateFields(event)), []string{"email.attachments", "email.files", "process"}; !reflect.DeepEqual(got, want) {
		t.Errorf("problems at 1.1.0 = %v; want %v", got, want)
	}
	event["metadata"] = map[string]any{"version": "1.9.0"}
	if got, want := paths(ValidateFields(event)), []string{"email.attachments", "process"}; !reflect.DeepEqual(got, want) {
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
			got := s.ValidateFields(tc.class, decode(t, tc.js))
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

// A scalar's type is checked against the schema's type. A derived type is
// checked as the base type the export's type list gives it.
func TestValidateScalarTypes(t *testing.T) {
	s := mustLoad(t, "1.1.0")
	for _, tc := range []struct {
		name  string
		class int
		js    string
		want  []string // "path: fragment of the reason"
	}{
		{"every type well-formed", 3002, `{"time":1776695531000,"time_dt":"2026-04-20T14:32:11Z","is_mfa":true,"severity_id":3,"message":"m",
			"user":{"name":"jdoe","uid":"S-1"},"src_endpoint":{"ip":"10.0.0.1","port":3389,"hostname":"h","mac":"aa:bb","location":{"coordinates":[1.5,-2]}},
			"duration":12,"count":2}`, nil},
		{"timestamp_t holding a string", 3002, `{"time":"2026-04-20T14:32:11Z"}`, []string{"time: timestamp_t"}},
		{"timestamp_t holding a fraction", 3002, `{"time":1776695531000.5}`, []string{"time: a whole number"}},
		{"timestamp_t inside an object", 1007, `{"process":{"created_time":"2026-04-20T14:32:11Z","parent_process":{"created_time":1}}}`, []string{"process.created_time: timestamp_t"}},
		{"datetime_t holding a number", 3002, `{"time_dt":1776695531000}`, []string{"time_dt: datetime_t"}},
		{"string_t holding a number", 3002, `{"message":42}`, []string{"message: a string"}},
		{"string_t holding a boolean", 3002, `{"message":true}`, []string{"message: a string"}},
		{"username_t holding a number", 3002, `{"user":{"name":7}}`, []string{"user.name: username_t"}},
		{"ip_t holding a number", 3002, `{"src_endpoint":{"ip":167772161}}`, []string{"src_endpoint.ip: ip_t"}},
		{"port_t holding a string", 3002, `{"src_endpoint":{"port":"3389"}}`, []string{"src_endpoint.port: port_t"}},
		{"integer_t holding a string", 3002, `{"severity_id":"3"}`, []string{"severity_id: integer_t"}},
		{"integer_t holding a fraction", 3002, `{"severity_id":3.5}`, []string{"severity_id: a whole number"}},
		{"boolean_t holding a string", 3002, `{"is_mfa":"true"}`, []string{"is_mfa: boolean_t"}},
		{"boolean_t holding a number", 3002, `{"is_mfa":1}`, []string{"is_mfa: boolean_t"}},
		{"float_t holding a whole number", 3002, `{"src_endpoint":{"location":{"coordinates":[1,2]}}}`, nil},
		{"float_t holding a string", 3002, `{"src_endpoint":{"location":{"coordinates":["1.5",2]}}}`, []string{"src_endpoint.location.coordinates[]: float_t"}},
		{"a list of strings holding a number", 3005, `{"privileges":["a",1]}`, []string{"privileges[]: string_t"}},
		{"json_t accepts any scalar", 2004, `{"evidences":[{"data":1},{"data":true},{"data":"x"}]}`, nil},
		{"unmapped accepts any scalar", 3002, `{"unmapped":{"time":"yesterday","n":"1"}}`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := s.ValidateFields(tc.class, decode(t, tc.js))
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

// Every scalar type an attribute uses, at every release checked in, leads to
// a base type the check knows, or to json_t. A release that adds a base type
// fails here, not silently.
func TestEveryScalarTypeHasABase(t *testing.T) {
	versions, err := Versions()
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range versions {
		s := mustLoad(t, v)
		seen := map[string]bool{}
		check := func(e *entity) {
			for name, attr := range e.Attributes {
				if attr.Type == "object_t" || attr.Type == "json_t" || seen[attr.Type] {
					continue
				}
				seen[attr.Type] = true
				if base := s.baseType(attr.Type); scalarWant[base] == "" {
					t.Errorf("OCSF %s: %s.%s is a %s, whose base type %s is not checked", v, e.key, name, attr.Type, base)
				}
			}
		}
		for _, c := range s.classes {
			check(c)
		}
		for _, o := range s.objects {
			check(o)
		}
		if s.baseType("timestamp_t") != "long_t" || s.baseType("username_t") != "string_t" || s.baseType("ip_t") != "string_t" {
			t.Errorf("OCSF %s: timestamp_t, username_t, ip_t resolve to %s, %s, %s", v, s.baseType("timestamp_t"), s.baseType("username_t"), s.baseType("ip_t"))
		}
	}
}

// Go's own numbers are read as JSON's are: an adapter may write an int64 of
// epoch milliseconds, and a time.Time or a string in a timestamp is caught.
func TestValidateTypedGoScalars(t *testing.T) {
	s := mustLoad(t, "1.1.0")
	ok := map[string]any{
		"time": int64(1776695531000), "severity_id": 3, "is_mfa": true, "message": "m",
		"src_endpoint": map[string]any{"port": uint16(3389), "location": map[string]any{"coordinates": []float64{1.5, 2}}},
		"count":        json.Number("2"),
	}
	if got := s.ValidateFields(3002, ok); len(got) != 0 {
		t.Errorf("well-typed Go values: %v", got)
	}
	bad := map[string]any{
		"time": time.Unix(0, 0), "time_dt": time.Unix(0, 0), "severity_id": 3.5, "message": json.Number("1"), "count": json.Number("2.5"),
	}
	if got, want := paths(s.ValidateFields(3002, bad)), []string{"count", "message", "severity_id", "time", "time_dt"}; !reflect.DeepEqual(got, want) {
		t.Errorf("problems = %v; want %v", got, want)
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
	if got := ValidateFields(decode(t, `{"class_uid":5023,"metadata":{"version":"1.4.0"}}`)); len(got) != 0 {
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
	got := ValidateFields(event)
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
	for _, p := range s.ValidateFields(6003, event) {
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

// A whole event is also checked for the fields its class requires and for
// its enum values and their captions (adr/0356).
func TestValidateRequiredAndEnums(t *testing.T) {
	const head = `"class_uid":3006,"category_uid":3,"type_uid":300603,"severity_id":1,"time":1,"metadata":{"version":"1.1.0"`
	for name, tc := range map[string]struct {
		js   string
		want []string
	}{
		"a whole event":                    {`{` + head + `},"activity_id":3,"activity_name":"add user","group":{"name":"g"}}`, nil},
		"required fields missing":          {`{"class_uid":3006,"metadata":{"version":"1.1.0"}}`, []string{"activity_id", "category_uid", "group", "severity_id", "time", "type_uid"}},
		"a profile the event does not use": {`{` + head + `},"activity_id":3,"group":{"name":"g"}}`, nil},
		"a profile the event uses":         {`{` + head + `,"profiles":["cloud"]},"activity_id":3,"group":{"name":"g"}}`, []string{"cloud"}},
		"a value the enum lacks":           {`{` + head + `},"activity_id":42,"group":{"name":"g"}}`, []string{"activity_id"}},
		"a caption of another value":       {`{` + head + `},"activity_id":3,"activity_name":"GROUP_ADD","group":{"name":"g"}}`, []string{"activity_name"}},
		"Other takes the source's word":    {`{` + head + `},"activity_id":99,"activity_name":"GROUP_ADD","type_uid":300699,"group":{"name":"g"}}`, nil},
		"a caption with no value":          {`{` + head + `},"activity_id":3,"status":"Succeeded","group":{"name":"g"}}`, []string{"status"}},
		"an enum inside an object":         {`{` + head + `},"activity_id":3,"group":{"name":"g"},"user":{"type_id":1,"type":"Admin"}}`, []string{"user.type"}},
	} {
		if got := paths(ValidateEvent(decode(t, tc.js))); !slices.Equal(got, tc.want) {
			t.Errorf("%s: problems = %v; want %v", name, got, tc.want)
		}
	}

	// A field the source cannot fill is passed over by name, and only when
	// the problem is that it is missing.
	bad := ValidateEvent(decode(t, `{`+head+`},"activity_id":42}`))
	if got, want := paths(WithoutRequired(bad, "group", "activity_id")), []string{"activity_id"}; !slices.Equal(got, want) {
		t.Errorf("WithoutRequired = %v; want %v", got, want)
	}
}
