// Package reckon holds the demo content `reckon init` materializes into a fresh
// install so the bundled lateral-movement scenario runs out of the box, without
// a repo checkout. The embedded files are the very same fixtures/ and examples/
// the capability + action smoke tests load from disk — one source of truth,
// embedded here for the binary and read directly there by the tests.
package reckon

import "embed"

// DemoFS carries the demo fixture scenario (OCSF events + declared write
// results), the example tenant capability/action configs, and the demo
// knowledge pack (SOPs + prior-case summaries under demo/knowledge). `reckon
// demo seed` materializes the fixtures/configs into the install's config
// directory (see runtime.SeedDemo) and loads the knowledge pack into the
// knowledge corpus (server seeds it). Kept in the module root so the //go:embed
// patterns can reach these trees (embed cannot traverse `..`).
//
//go:embed fixtures examples demo
var DemoFS embed.FS

// DemoKnowledgeSOPs / DemoKnowledgeCases are the embedded knowledge-pack
// subtrees the demo seed loads: institutional SOPs (markdown + frontmatter) and
// prior concluded-case summaries (JSON), both scoped to the DemoScenario world
// so similar-case recall and SOP consultation have real material to surface.
const (
	DemoKnowledgeSOPs  = "demo/knowledge/sops"
	DemoKnowledgeCases = "demo/knowledge/cases"
)

// DemoScenario is the fixture scenario `reckon demo seed` wires by default
// (03 §9, 08 §9). It names the fixture subdirectory under fixtures/.
const DemoScenario = "lateral-movement-via-rdp"

// DemoConfigBase is the base filename (no extension) of the default scenario's
// example capability and action configs under examples/{capability,action}/.
// It differs from DemoScenario: the configs are named for the workflow, the
// fixtures for the full scenario id.
const DemoConfigBase = "lateral-movement"

// DemoScenarios maps every bundled fixture scenario to the base filename of its
// example capability + action configs (adr/0058). The stack is wired for ONE
// scenario at a time — the fixture adapter answers from one directory, and two
// scenarios' wildcards would answer each other's questions — so `reckon demo
// seed --scenario` and `reckon demo scenario` select among these. The eval
// corpus (design/10 §6) has one driver script per scenario; a prompt rule is
// a principle only when it holds on more than one shape of case.
var DemoScenarios = map[string]string{
	DemoScenario:               DemoConfigBase,
	"impossible-travel-signin": "impossible-travel",
}

// DemoConfigBaseFor returns the example-config base name for a bundled
// scenario, or false when no such scenario is bundled.
func DemoConfigBaseFor(scenario string) (string, bool) {
	base, ok := DemoScenarios[scenario]
	return base, ok
}
