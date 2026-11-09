package agent

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// pointerRe is the one form a ledger provenance pointer takes: a GitHub issue
// number or a 7-character commit — resolvable by anyone with the repo.
var pointerRe = regexp.MustCompile(`^(#[1-9][0-9]*|[0-9a-f]{7})$`)

// TestPrompt_Frozen pins the prompt version for each framing dial (adr/0057,
// adr/0232). The version covers every word the turn loop puts in front of the
// model, and every change to those words moves the version the eval baselines
// are accepted under (design/10 §4.4), so a change must be deliberate: update
// the golden here and in TestPrompt_ComponentsFrozen, the ledger entry of what
// moved, and rerun the per-model baselines. A refactor that keeps the bytes
// leaves this test green; that is the point of pinning the hash rather than
// the text.
func TestPrompt_Frozen(t *testing.T) {
	golden := map[string]string{
		framingLeadingNull: "457998381a4d1f0a7c9f34a41a9f23cdcc1cd624cf83a435a3c78dc8484aac1f",
		framingLeadingOnly: "15dc2b4e747008fe89f7e04254fc4a6c6abf1b64a7791b15dcb6c09f6abe71b0",
	}
	for dial, want := range golden {
		if got := PromptVersion(dial); got != want {
			t.Errorf("prompt version under %s = %s; want %s\n  %s", dial, got, want, whatMoved(dial))
		}
	}
}

// whatMoved says why a version moved, from the components against their
// goldens: a reordering, the example fixtures, or the text the model reads.
func whatMoved(dial string) string {
	var moved []string
	for i, c := range promptComponents(dial) {
		name := c.Name
		if i == 0 && dial == framingLeadingOnly {
			name = "prompt/leading_only"
		}
		if componentGolden[name] != digest(c.Bytes) {
			moved = append(moved, c.Name)
		}
	}
	switch {
	case len(moved) == 0:
		return "every component matches its golden, so their order changed: engineTexts, intrinsicTools, or the order promptComponents builds. If that is deliberate, update the golden; no text the model reads moved."
	case !slices.ContainsFunc(moved, func(n string) bool { return n != "tools/request_action+catalog" && n != "tools/<verb>" }):
		return fmt.Sprintf("only %v moved: the catalog- and verb-shaped tool text, or the example fixtures it is rendered from (exampleCatalog, exampleVerb), which no model is shown. TestPrompt_ComponentsFrozen has the digests.", moved)
	default:
		return fmt.Sprintf("what the model is read changed: %v. If that is deliberate: update this golden and TestPrompt_ComponentsFrozen's, the ledger entry of what moved (Revised, and PendingValidation for a prompt rule), and rerun the per-model eval baselines (make eval; make eval-accept) — design/10 §4.4.", moved)
	}
}

// TestSideInstructions_Frozen pins the side completions' version. A change
// re-attributes the summary and SOP draft; no baseline rerun (adr/0232).
func TestSideInstructions_Frozen(t *testing.T) {
	const want = "04f09ec1bbec49017ac3742feba5544b9ef85f5cbeb2ef1558cf2604d0078952"
	if got := SideInstructionVersion(); got != want {
		t.Errorf("side instruction version = %s; want %s\n"+
			"  a side instruction changed; TestPrompt_ComponentsFrozen names which. If that is deliberate: update both goldens and the entry's Revised.",
			got, want)
	}
}

// componentGolden is the digest of every hashed component, by name, so a
// moved version says what moved: the prompt, a tool, an engine text, or a side
// instruction.
var componentGolden = map[string]string{
	"prompt":                                           "7ba7d77c1ba5b94dbb28a9eff9bd949ac6559b9a9789e23f6f69599590806cc0",
	"prompt/leading_only":                              "fa7a504531324a86c4960f517e5a4a62e32f7a0e99b69218a9f2a23c071cdda6",
	"tools/recall_sops":                                "8c6809eb0dd1c94cbeb3a8e14fcae26eb627b2ca29785cc9b4fddfdcb347861c",
	"tools/read_sop":                                   "d2a3d735e2d12528e5f651c56b8403778179d882ddb5a9405f88326d358a1b28",
	"tools/recall_similar_investigations":              "013a01bbff06558a7d972be60d2e93248f23e54e71e1f5ce87196e800d45d711",
	"tools/prior_investigations":                       "433c5d17cb369e9f19d36a54571b007c5230278d076c129006b037d61341f49e",
	"tools/propose_hypothesis":                         "7e8b3212c19b856139929b44c97ebea7eef78588a613b189a26ff9eb0828baf9",
	"tools/record_prediction":                          "de9fe394674042ace225eb4ef59c1fed2b9b92f2de865b06d69430d7a95ad73c",
	"tools/evaluate_hypothesis":                        "9ea21732ba5c7000c7940a635f7871c066a9fb687da5b2b751f00f5f74e1da97",
	"tools/record_prediction_outcome":                  "146ac06325ffa4fb1805a7cdf3844ca15968be16c3947518cce7e4e1d7e8ed52",
	"tools/request_action":                             "117932bb6e2e9b1eeb5b1974a37f1a5b0a2d3278adca0efc57d6f38c7e70c02e",
	"tools/list_actions":                               "149708b898187a9bf3a486e36bd969630944a5fa234e72a4c40cf1f90a77cbea",
	"tools/read_result":                                "e8a77530cd8f75be5ce8c3d39c8fbc1a777defb2a5e04b4ee86e2b37e4fb4f3b",
	"tools/request_action+catalog":                     "6306b2cfe0054491a3b2760418af0bcfde7761285b813ef888e291cbfb3a58d2",
	"tools/<verb>":                                     "cddfd2dd48b53db6e0285ae8f58f88fd5eda6024e8cdabf7a7257760f4ad8cd8",
	"engine-text/clock":                                "9d82e0ccdff304d70aafa9f32ea959243dd87ef82b25b1919c0ea2ddde472f06",
	"engine-text/reasoning-state":                      "e1aec4e82563e9898ba2876b6a9fa94d9bf4cc3842a259177a7b5c50cb1fed23",
	"engine-text/reasoning-state-hypothesis":           "81ddac439fe7e550516bca3404472b52888af2240b01f41df7b05a9639a000b5",
	"engine-text/reasoning-state-labels":               "0f22835e53c9aab150886e651d575a3f28e6e911e658c2cf92f57c14bddcf0f9",
	"engine-text/reasoning-state-statement":            "37f95fb8135ad4717fed23fd94a20f127f604de39fa7813355a87b67b7824dcb",
	"engine-text/reasoning-state-positional-name":      "d413dd752d36afdd14077b848a708130876e87e142dac78fe9aaac092828ec76",
	"engine-text/reasoning-state-prediction-name":      "a8481799ee1b2c2d0cf22c74febdb4ad0f7c0c2fa0548c29dc440d625b8b814d",
	"engine-text/reasoning-state-no-predictions":       "2505f8e58d444fcc63df47349329663515c583be6bad3378581af92a263ee62b",
	"engine-text/reasoning-state-predictions":          "efba82ad046b26af5ad3eff480ab6ecbdeb6985d835c2f400cf7d03a9261731d",
	"engine-text/reasoning-state-tally-count":          "2537e8c094a34d3c67a2b6113eff2607b9113a58546a08e80395cf66d99e9565",
	"engine-text/reasoning-state-tally-order":          "c8fabe7d63b8eadbf67b36b9b84570cb135c89181ec8ce6395b1c120a3f99d2c",
	"engine-text/reasoning-state-untested":             "fa87d821293d818561bdd8d34436c2328d51125b2190ee43336338d4be0346a7",
	"engine-text/reasoning-state-discriminates":        "c09643f962cc36be8c73c96cc48d13a34e7de3bad72e67e41ea2bf55d16484db",
	"engine-text/reasoning-state-manual":               "5cb5e6f71f386bcd004aa00159b5f6f7ebe92d42c292603e8645320757651d33",
	"engine-text/reasoning-state-prediction-statement": "c666754d04e37145f69da3bf498cccffc021e321053ee42db344f9aa93317336",
	"engine-text/reasoning-state-outstanding":          "7b5544d51b8930406a1f4660a51a76d97ace5b321810deee2ef4cc39c67209cd",
	"engine-text/reasoning-state-holds":                "d645afd9771eff5457530cd22a4a132df01e5ab4916babd32c93974f56243e4b",
	"engine-text/reasoning-state-holds-separator":      "0a2d3f8f9b727cf73e6c7e44f2feefc41855195b3010a3c4f6b05c33aeb484a9",
	"engine-text/reasoning-state-until-tested":         "e20f72a19b42f8cd760341cdfd1373c46e3a85abe04426a7e670ae3067e1d7c5",
	"engine-text/reasoning-state-none-outstanding":     "74bdd932e3857bdb9cc61294fca59cde47f7256118b40e62a2973ff5c9dda764",
	"engine-text/reasoning-state-end":                  "cfae0d4248f7142f7b17f826cd7a519280e312577690e957830d23dcf35a3fff",
	"engine-text/engine-state":                         "326ac218467fa6502216367f229114106afac3f0ba7cbe6a11ad1dbd5baacccc",
	"engine-text/engine-state-row":                     "ed344954243d0f1c410b348c2518dfa3b60f0f942d6edf46b7fb63c2f864c731",
	"engine-text/engine-record":                        "bed3db1eb1d51a388f65181f4e2e8369f3981dfc7b5024ca48667166e4e187b6",
	"engine-text/engine-record-entry":                  "5d8b71dc61ad9b0da47dac263ed6486ea54bc3936c1fef95856ac5b39ddb5446",
	"engine-text/engine-record-not-on-record":          "8b260cce172581e12983ba7974fbe61ee7a63496358cd60b4226bdbd66813326",
	"engine-text/engine-record-no-action":              "e67c320fc03407c9424756949aaa6e866949eb4de3b4cde2d93f2f5515781739",
	"engine-text/footer-separator":                     "01ba4719c80b6fe911b091a7c05124b64eeece964e09c058ef8f9805daca546b",
	"engine-text/context-condensed":                    "5783443024fd84eaa7cb79cc0c0e1b851034e5d0a1ead879fd20077986fde566",
	"engine-text/result-removed":                       "3a582f8069cb3c8fe217ad0f9cdf4b6c32e9e4dd067eab8eeb6413d59602cc67",
	"engine-text/result-removed-rerun":                 "7f7d25f939a14f96e66971af180518f78006e49b7d0288d0ee67fe2efd13ac51",
	"engine-text/result-removed-read":                  "12b3acd734cbf6c9a95532992263bf2d45c4d8454005cce03ca67a2b8793bde3",
	"engine-text/result-paged":                         "69cce5b6388c9bc1c50ea07c86675615f3bb70de60ca6717f4cd66f47542c7a6",
	"engine-text/result-oversized":                     "29ba18cf3dbbca2eb992acdf990bce4ec6a77fad686011ea63d5926dc4061a03",
	"engine-text/result-bounded-shape":                 "f2990b07741a8e5d481b8bb54073424870f3f6df286e46902eb0de34652e1fa3",
	"engine-text/tool-cut-off":                         "5ccc2e8a89e99f51f9db79118a6c80edaeed67999778651bd7d2ecf204d6d044",
	"engine-text/prompt-seed":                          "3178d6e9524d98f221c086147a8136871db3c9de812e647af9583db25a66e14d",
	"engine-text/prompt-case-seed":                     "f893878b496f92aecc358852953249550dffb9e9247ee93d1a0cba0a4f159abe",
	"engine-text/prompt-degraded":                      "4dcc300552c6f82cff49a819b33069beacef67fa399d0ce3052292e14383d00b",
	"engine-text/list-separator":                       "0a07f659461970d8d8dcefe4fff96a1745599810dc7d47391e95c803b7b2072c",
	"engine-text/clipped":                              "b4935894c8ce16bde67bbffc672428b1533c4f1e1e8b1ce21a2933183db6bfa5",
	"side/summarize":                                   "876f69bf2774f065305f8afbadbaacbf00592ac42769161d64d3b5aadd8735e6",
	"side/narrative":                                   "7b5acdbdead0dd2a29020c7ec41466e6ea2dda068aec05637c8171702edd9052",
	"side/propose-sop":                                 "78af65d40b45f73007c9cb90ff306f68f2a6e9f6843facb0849ad159f1a73fab",
	"side/propose-sop-anyway":                          "53d278cafac910a914934aaa0fe71703e446f79f62c9ed88d8c0e63259d44969",
}

// TestPrompt_ComponentsFrozen names what moved when a version did: each
// hashed component against its golden digest, and any component added or
// gone.
func TestPrompt_ComponentsFrozen(t *testing.T) {
	got := map[string]string{}
	for _, c := range promptComponents(framingLeadingNull) {
		got[c.Name] = digest(c.Bytes)
	}
	got["prompt/leading_only"] = digest(promptComponents(framingLeadingOnly)[0].Bytes)
	for _, c := range sideComponents() {
		got[c.Name] = digest(c.Bytes)
	}
	for name, want := range componentGolden {
		switch g, ok := got[name]; {
		case !ok:
			t.Errorf("%s is in the golden but no longer hashed", name)
		case g != want:
			t.Errorf("%s changed: digest %s; want %s", name, g, want)
		}
	}
	for name, g := range got {
		if _, ok := componentGolden[name]; !ok {
			t.Errorf("%s is hashed but has no golden: add %q: %q", name, name, g)
		}
	}
}

// TestModelTextLedger_Complete refuses model-facing text without provenance:
// every engine text, side instruction and intrinsic tool carries what
// introduced it, in the prompt ledger's pointer form, and engine and side
// texts a unique id.
func TestModelTextLedger_Complete(t *testing.T) {
	checkPointers := func(what, origin string, revised []string) {
		if !pointerRe.MatchString(origin) {
			t.Errorf("%s: Origin %q is not one pointer — want an issue (\"#54\") or a 7-char commit", what, origin)
		}
		for _, rev := range revised {
			if !pointerRe.MatchString(rev) {
				t.Errorf("%s: Revised entry %q is not a pointer", what, rev)
			}
		}
	}
	for ledger, entries := range map[string][]modelText{"engine-text": engineTexts, "side": sideInstructions} {
		seen := map[string]bool{}
		for _, e := range entries {
			what := ledger + "/" + e.ID
			switch {
			case e.ID == "":
				t.Errorf("%s entry %q has no ID", ledger, e.Text)
			case seen[e.ID]:
				t.Errorf("%s: ID used twice", what)
			}
			seen[e.ID] = true
			checkPointers(what, e.Origin, e.Revised)
			if e.Text == "" {
				t.Errorf("%s: Text is empty", what)
			}
		}
	}
	for _, tl := range intrinsicTools(nil) {
		checkPointers("tools/"+tl.def.Name, tl.origin, tl.revised)
	}
}

// TestPromptLedger_Complete refuses a rule without provenance: every rendered
// bullet must carry a stable ID, what introduced it, the principle it encodes
// (stated without scenario vocabulary), and at least one scenario its wording
// was graded against. A rule that exists only because one transcript went
// wrong cannot hide here as prose.
func TestPromptLedger_Complete(t *testing.T) {
	seen := map[string]bool{}
	for _, sec := range promptLedger {
		if sec.Title == "" || len(sec.Rules) == 0 {
			t.Errorf("ledger section %q is empty", sec.Title)
		}
		for _, r := range sec.Rules {
			switch {
			case r.ID == "":
				t.Errorf("rule %q has no ID", r.Text)
			case seen[r.ID]:
				t.Errorf("rule ID %q is used twice", r.ID)
			}
			seen[r.ID] = true
			if !pointerRe.MatchString(r.Origin) {
				t.Errorf("rule %s: Origin %q is not one pointer — want an issue (\"#54\") or a 7-char commit (\"80420c3\")", r.ID, r.Origin)
			}
			for _, rev := range r.Revised {
				if !pointerRe.MatchString(rev) {
					t.Errorf("rule %s: Revised entry %q is not a pointer — want an issue (\"#60\") or a 7-char commit", r.ID, rev)
				}
			}
			if strings.TrimSpace(r.Why) == "" {
				t.Errorf("rule %s: Why is empty — state the principle, or the rule is a patch", r.ID)
			}
			switch {
			case len(r.Validated) == 0 && r.PendingValidation == "":
				t.Errorf("rule %s: neither Validated nor PendingValidation is set — a rule is graded against at least one scenario, or names the issue whose runs will grade it", r.ID)
			case len(r.Validated) > 0 && r.PendingValidation != "":
				t.Errorf("rule %s: both Validated and PendingValidation are set — a wording is either accepted or awaiting acceptance", r.ID)
			}
			if r.Text == "" {
				t.Errorf("rule %s: Text is empty", r.ID)
			}
			if strings.HasPrefix(r.Text, "- ") || strings.HasSuffix(r.Text, "\n") {
				t.Errorf("rule %s: Text carries its own bullet or newline; the renderer adds those", r.ID)
			}
		}
	}
	// Every rule renders: the prompt under each dial contains each rule's text
	// for that dial, so the ledger and the prompt cannot drift apart.
	for _, dial := range []string{framingLeadingNull, framingLeadingOnly} {
		p := systemPrompt(Investigation{HypothesisFraming: dial}, nil)
		for _, sec := range promptLedger {
			for _, r := range sec.Rules {
				if !strings.Contains(p, "- "+r.render(dial)+"\n") {
					t.Errorf("rule %s is in the ledger but not rendered under %s", r.ID, dial)
				}
			}
		}
	}
}
