package eval

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sd-strax/reckon/agent"
	"github.com/sd-strax/reckon/config"
	"github.com/sd-strax/reckon/internal/branding"
)

// RunConfig assembles one eval run (10 §2): one scenario, one model, N trials
// against the RUNNING local stack (`reckon start` with the scenario's
// capability/action config). The harness does not boot the stack — it drives
// the same backend an analyst would.
type RunConfig struct {
	ScenarioPath string
	// Model overrides the provider model; empty = RECKON_MODEL or the default.
	Model string
	// ArtifactDir receives the report + per-trial records (transcripts included;
	// local artifacts, never committed — 10 §4.3). Empty = eval/artifacts/<run>.
	ArtifactDir string
	// Log receives progress lines; nil = silent. It is called from several
	// goroutines at once (one per in-flight trial), so it must be safe for
	// concurrent use — testing.T.Logf is.
	Log func(format string, args ...any)
	// Parallel caps how many trials run at once; 0 = RECKON_EVAL_PARALLEL,
	// else every trial concurrently. 1 serializes (easier to read live logs).
	Parallel int
	// Stagger spaces trial starts: trial n begins (n-1)×Stagger after trial 1,
	// so concurrent trials don't open on the provider in the same instant (a
	// synchronized burst is the likeliest 429). 0 = RECKON_EVAL_STAGGER, else
	// defaultStagger; negative disables.
	Stagger time.Duration
}

// defaultStagger is the gap between concurrent trial starts: long enough to
// de-synchronize the opening calls, short against a ~4-minute trial.
const defaultStagger = 5 * time.Second

// Run executes the scenario end to end and returns the graded report. The
// report is also written to the artifact dir alongside the per-trial records.
func Run(ctx context.Context, rc RunConfig) (*Report, error) {
	logf := rc.Log
	if logf == nil {
		logf = func(string, ...any) {}
	}

	s, err := LoadScenario(rc.ScenarioPath)
	if err != nil {
		return nil, err
	}

	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("eval: ANTHROPIC_API_KEY is not set (the loop is BYOK)")
	}
	model := rc.Model
	if model == "" {
		model = envOr("RECKON_MODEL", agent.DefaultAnthropicModel)
	}

	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("eval: load config: %w", err)
	}
	issuer := fmt.Sprintf("http://localhost:%d/realms/%s", cfg.Keycloak.HTTPPort, cfg.Keycloak.Realm)
	backendURL := fmt.Sprintf("http://localhost:%d", cfg.Backend.HTTPPort)

	user := envOr("RECKON_USER", branding.CLI+"-admin")
	pass := envOr("RECKON_PASSWORD", branding.CLI)
	humanCred, err := agent.NewCredential(ctx, issuer, branding.CLI, user, pass)
	if err != nil {
		return nil, fmt.Errorf("eval: login (human client): %w\n  hint: the shipped realm has no login user and ROPC is off — run `%s dev-auth` first", err, branding.CLI)
	}
	agentCred, err := agent.NewCredential(ctx, issuer, branding.CLI+"-agent", user, pass)
	if err != nil {
		return nil, fmt.Errorf("eval: login (agent client): %w", err)
	}
	client := agent.NewClient(backendURL, agentCred, humanCred)

	// The stack is wired for one fixture scenario at a time (adr/0058). Check
	// it is this driver's before spending a token: one search_alerts call, whose
	// events carry the fixture adapter's source_tool.
	if s.FixtureScenario != "" {
		if err := checkWiredScenario(ctx, client, s.FixtureScenario); err != nil {
			return nil, err
		}
	}

	// The served action vocabulary: H3's ground truth, the declared parameter
	// schemas (H5's), and part of the run's attribution. An error means the
	// action layer is off — recorded as such.
	var catalog []string
	actionInputs := map[string][]ParamSpec{}
	catalogHash := "unavailable"
	if actionTypes, err := client.ListActionTypes(ctx); err == nil {
		for _, a := range actionTypes {
			catalog = append(catalog, a.Descriptor.ActionType)
			var specs []ParamSpec
			for _, in := range a.Descriptor.Inputs {
				if in.Type == "entity" {
					continue // entity inputs ride targets (08 §3)
				}
				specs = append(specs, ParamSpec{Name: in.Name, Required: in.Required})
			}
			actionInputs[a.Descriptor.ActionType] = specs
		}
		if raw, err := json.Marshal(actionTypes); err == nil {
			sum := sha256.Sum256(raw)
			catalogHash = hex.EncodeToString(sum[:])
		}
	}

	logf("eval: scenario %s — %d turns × %d trials on %s (costs real tokens)",
		s.ID, len(s.Turns), s.Trials, model)

	artifactDir := rc.ArtifactDir
	if artifactDir == "" {
		artifactDir = filepath.Join("artifacts", fmt.Sprintf("%s-%s", s.ID, time.Now().Format("20060102-150405")))
	}
	if err := os.MkdirAll(artifactDir, 0o750); err != nil {
		return nil, fmt.Errorf("eval: create artifact dir: %w", err)
	}

	// Trials run concurrently: each is a fresh investigation driven by its own
	// Session, sharing only the backend client and the two credentials (both
	// goroutine-safe), so nothing couples them but the provider's rate limit —
	// which the Anthropic client's backoff rides out. Wall time is one trial's,
	// not N. Parallel bounds the fan-out (RECKON_EVAL_PARALLEL; 0 = all N).
	parallel := rc.Parallel
	if parallel <= 0 {
		parallel = envInt("RECKON_EVAL_PARALLEL", s.Trials)
	}
	if parallel <= 0 || parallel > s.Trials {
		parallel = s.Trials
	}
	stagger := rc.Stagger
	if stagger == 0 {
		stagger = envDuration("RECKON_EVAL_STAGGER", defaultStagger)
	}
	if stagger < 0 {
		stagger = 0
	}
	logf("eval: running %d trial(s) at a time, starts staggered by %s", parallel, stagger)

	type trialOutcome struct {
		rec   *TrialRecord
		stamp promptStamp
		err   error
	}
	outcomes := make([]trialOutcome, s.Trials)
	sem := make(chan struct{}, parallel)
	var wg sync.WaitGroup
	for n := 1; n <= s.Trials; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			// Deterministic stagger rather than random jitter: the same
			// spacing every run keeps runs comparable, and (n-1)×stagger
			// guarantees no two trials open on the provider together.
			if wait := time.Duration(n-1) * stagger; wait > 0 {
				select {
				case <-time.After(wait):
				case <-ctx.Done():
					outcomes[n-1] = trialOutcome{err: ctx.Err()}
					return
				}
			}
			sem <- struct{}{}
			defer func() { <-sem }()
			logf("eval: trial %d/%d started", n, s.Trials)
			tr, stamp, err := runTrial(ctx, client, backendURL, humanCred, s, model, apiKey, n, logf)
			if err != nil {
				logf("eval: trial %d aborted: %v", n, err)
			}
			if tr != nil {
				tr.ActionCatalog = catalog
				tr.ActionInputs = actionInputs
				// Each trial owns its own file, so the write needs no lock.
				if raw, err := json.MarshalIndent(tr, "", "  "); err == nil {
					_ = os.WriteFile(filepath.Join(artifactDir, fmt.Sprintf("trial-%d.json", n)), raw, 0o600)
				}
			}
			outcomes[n-1] = trialOutcome{rec: tr, stamp: stamp, err: err}
		}(n)
	}
	wg.Wait()

	// Fold in trial order so verdict columns, trial errors, and the prompt
	// stamp are deterministic regardless of which goroutine finished first.
	var (
		trials      []*TrialRecord
		trialErrors []string
		prompt      promptStamp
	)
	for i, o := range outcomes {
		n := i + 1
		if o.err != nil {
			trialErrors = append(trialErrors, fmt.Sprintf("trial %d: %v", n, o.err))
		}
		if prompt.Version == "" && o.stamp.Version != "" {
			// Prompt-version identifier (10 §1.4): the BASE prompt's version
			// (agent.PromptVersion — prompt code + framing dial), the same for
			// every trial, run, and model on the same code. The as-run prompt,
			// investigation context included, is kept per trial in the
			// artifact (TrialRecord.SystemPrompt), not hashed (#20).
			prompt = o.stamp
		}
		if o.rec != nil {
			trials = append(trials, o.rec)
		}
	}
	if len(trials) == 0 {
		return nil, fmt.Errorf("eval: no trial completed: %v", trialErrors)
	}

	var runUsage Usage
	for _, tr := range trials {
		for _, turn := range tr.Turns {
			runUsage.Add(turn.Usage)
		}
	}
	judge := judgeFromEnv(apiKey, logf)
	report := &Report{
		Attribution: Attribution{
			PromptVersion:     prompt.Version,
			HypothesisFraming: prompt.Framing,
			Model:             model,
			ActionCatalogHash: catalogHash,
			ScenarioID:        s.ID,
			DriverHash:        s.Hash,
			CatalogueVersion:  CatalogueVersion,
			Trials:            s.Trials,
		},
		Results:     GradeWith(s, trials, judge),
		TrialErrors: trialErrors,
		Usage:       runUsage,
	}
	stampJudge(report, judge)
	if raw, err := json.MarshalIndent(report, "", "  "); err == nil {
		_ = os.WriteFile(filepath.Join(artifactDir, "report.json"), raw, 0o600)
	}
	logf("eval: artifacts in %s", artifactDir)
	return report, nil
}

// promptStamp is the run's prompt attribution, read off the session: the base
// prompt version and the framing dial it was taken under.
type promptStamp struct {
	Version string
	Framing string
}

// runTrial creates + activates a fresh investigation, then drives the shipped
// Session through the script. Returns the trial record (possibly partial, with
// Aborted set) and the session's prompt attribution.
func runTrial(ctx context.Context, client *agent.Client, backendURL string, human agent.TokenSource,
	s *Scenario, model, apiKey string, trial int, logf func(string, ...any)) (*TrialRecord, promptStamp, error) {
	invID, err := createActiveInvestigation(ctx, backendURL, human,
		fmt.Sprintf("eval: %s — trial %d", s.ID, trial))
	if err != nil {
		return nil, promptStamp{}, err
	}

	session, err := agent.NewSession(ctx, agent.Config{
		Backend: client,
		LLM: &agent.Anthropic{APIKey: apiKey, Model: model, OnRetry: func(attempt int, wait time.Duration, err error) {
			logf("eval: trial %d — provider backpressure (attempt %d), retrying in %s: %v", trial, attempt, wait.Round(time.Second), err)
		}},
		InvestigationID: invID,
	})
	if err != nil {
		return nil, promptStamp{}, fmt.Errorf("assemble session: %w", err)
	}
	stamp := promptStamp{Version: session.PromptVersion(), Framing: session.HypothesisFraming()}

	tr := &TrialRecord{Trial: trial, InvestigationID: invID, SystemPrompt: session.System()}
	if inv, err := client.GetInvestigation(ctx, invID); err == nil {
		tr.HypothesisFraming = inv.HypothesisFraming
	}
	reader := readerFromEnv(apiKey)
	for i, spec := range s.Turns {
		if spec.AttestManual != nil {
			// The scripted analyst answers the manual tests (adr/0069): the
			// Record result click, with the scenario's authored answer, the
			// reader deciding which way it points for each prediction's wording.
			recs, err := attestManualPredictions(ctx, client, backendURL, human, invID, i, *spec.AttestManual, reader)
			tr.Attestations = append(tr.Attestations, recs...)
			if err != nil {
				return tr, stamp, fmt.Errorf("turn %d: attest manual predictions: %w", i, err)
			}
			if len(recs) == 0 {
				logf("eval: trial %d turn %d — attest_manual: no UNTESTED manual prediction to answer (no-op)", trial, i+1)
			}
			for _, rec := range recs {
				logf("eval: trial %d turn %d — driver attested %s as %s (%s)", trial, i+1, rec.PredictionRef, rec.Status, rec.Reason)
			}
		}
		if spec.AckHypotheses {
			// The scripted analyst adopts what the agent proposed (a human act
			// the aggregate enforces) so the turn can drive OPEN hypotheses.
			n, err := ackProposedHypotheses(ctx, client, backendURL, human, invID)
			if err != nil {
				return tr, stamp, fmt.Errorf("turn %d: ack hypotheses: %w", i, err)
			}
			logf("eval: trial %d turn %d — driver acknowledged %d PROPOSED hypothesis(es)", trial, i+1, n)
		}
		res, err := session.Turn(ctx, spec.User)
		rec := TurnRecord{Index: i, UserMsg: spec.User}
		if res != nil {
			rec.Text = res.Text
			rec.Transcript = res.Transcript
			rec.InterpretationID = res.InterpretationID
			rec.ToolRounds = res.ToolRounds
			rec.Usage = Usage{Input: res.Usage.Input, Output: res.Usage.Output,
				CacheWrite: res.Usage.CacheWrite, CacheRead: res.Usage.CacheRead}
			for _, tc := range res.ToolCalls {
				rec.ToolCalls = append(rec.ToolCalls, ToolCall{ToolName: tc.ToolName, Args: string(tc.Args)})
			}
		}
		if err != nil {
			// The turn failed (model call or commit) — the record is not the
			// committed truth, so the trial stops here rather than grading
			// unpersisted output (10 §1.1).
			rec.Err = err.Error()
			tr.Turns = append(tr.Turns, rec)
			tr.Aborted = true
			return tr, stamp, fmt.Errorf("turn %d: %w", i, err)
		}
		tr.Turns = append(tr.Turns, rec)
		logf("eval: trial %d turn %d/%d — %d tool rounds", trial, i+1, len(s.Turns), res.ToolRounds)
	}

	// Capture the investigation's actions from the durable view at trial end —
	// the event-log layer read back through the product API (10 §1.1b), which
	// the event graders (G1) grade. Best-effort: a fetch failure leaves Actions
	// nil and those graders report NOT_EXERCISED rather than silently passing.
	if acts, err := client.ListActions(ctx, invID); err == nil {
		tr.Actions = make([]ActionRow, 0, len(acts))
		for _, a := range acts {
			tr.Actions = append(tr.Actions, ActionRow{
				ActionID:     a.ActionID,
				ActionType:   a.ActionType,
				Status:       a.Status,
				IsReversal:   a.IsReversal,
				EvidenceRefs: a.EvidenceRefs,
			})
		}
	} else {
		logf("eval: trial %d — actions view fetch failed (event graders NOT_EXERCISED): %v", trial, err)
	}
	// The reasoning set at trial end — R3's ground truth for "did a rival still
	// have an untested discriminating prediction when the agent decided?".
	if tr.Hypotheses = hypothesesSnapshot(ctx, client, invID); tr.Hypotheses == nil {
		logf("eval: trial %d — hypotheses view fetch failed (R3 NOT_EXERCISED)", trial)
	}
	return tr, stamp, nil
}

// checkWiredScenario asserts the running stack replays the fixture scenario the
// driver script was written against: the events a bound verb returns are
// stamped source_tool "fixture:<scenario>" (03 §9.2). A mismatch names the
// swap command rather than letting the run grade the wrong world.
func checkWiredScenario(ctx context.Context, client *agent.Client, want string) error {
	raw, err := client.InvokeCapability(ctx, "search_alerts", agent.InvokeInput{Extra: map[string]any{"filter": "*"}})
	if err != nil {
		return fmt.Errorf("eval: probe the wired scenario (search_alerts): %w\n  is the stack up with a demo scenario wired? (`%s demo seed --scenario %s`)", err, branding.CLI, want)
	}
	var resp struct {
		Events []struct {
			SourceTool string `json:"source_tool"`
		} `json:"events"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return fmt.Errorf("eval: parse the scenario probe: %w", err)
	}
	if len(resp.Events) == 0 {
		return fmt.Errorf("eval: the wired scenario returned no alerts — the stack is not replaying %s (`%s demo scenario %s`, stack down, then start)", want, branding.CLI, want)
	}
	const prefix = "fixture:"
	got := strings.TrimPrefix(resp.Events[0].SourceTool, prefix)
	if !strings.HasPrefix(resp.Events[0].SourceTool, prefix) || got != want {
		return fmt.Errorf("eval: the stack is wired for scenario %q; this driver needs %q.\n  stop the stack, run `%s demo scenario %s`, start it, and rerun", got, want, branding.CLI, want)
	}
	return nil
}

// createActiveInvestigation provisions one fresh investigation per trial —
// trials must not share reasoning state — via the product API on the human
// token (create + activate are the analyst's acts).
func createActiveInvestigation(ctx context.Context, backendURL string, human agent.TokenSource, title string) (string, error) {
	var created struct {
		AggregateID string `json:"aggregate_id"`
	}
	if err := postJSON(ctx, human, backendURL+"/api/investigations",
		map[string]any{"title": title}, &created); err != nil {
		return "", fmt.Errorf("create investigation: %w", err)
	}
	if err := postJSON(ctx, human, backendURL+"/api/investigations/"+created.AggregateID+"/lifecycle",
		map[string]any{"transition": "activate"}, nil); err != nil {
		return "", fmt.Errorf("activate investigation: %w", err)
	}
	return created.AggregateID, nil
}

// hypothesesSnapshot reads the investigation's reasoning set through the
// product API (handles, statuses, predictions + discriminates) — the state R3
// grades against. nil on failure.
func hypothesesSnapshot(ctx context.Context, client *agent.Client, invID string) []HypothesisRow {
	raw, err := client.ListHypotheses(ctx, invID)
	if err != nil {
		return nil
	}
	var v struct {
		Hypotheses []HypothesisRow `json:"hypotheses"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil
	}
	return v.Hypotheses
}

// attestManualPredictions records, with the human token, an attested outcome
// for every UNTESTED manual prediction in the investigation — the tracker's
// Record result, scripted (adr/0054). The authored answer is the same for each
// (one person said one thing); the reader decides per prediction whether that
// answer confirms or disconfirms its wording. Without a reader the step
// cannot read the answer and fails loudly rather than guess.
func attestManualPredictions(ctx context.Context, client *agent.Client, backendURL string, human agent.TokenSource,
	invID string, turn int, spec AttestSpec, reader Reader) ([]AttestationRecord, error) {
	var out []AttestationRecord
	for _, h := range hypothesesSnapshot(ctx, client, invID) {
		for _, p := range h.Predictions {
			if p.Status != "UNTESTED" || p.TestKind != "manual" {
				continue
			}
			if reader == nil {
				return out, fmt.Errorf("prediction %s is manual and untested but no reader is configured to read the authored answer (ANTHROPIC_API_KEY)", p.ID)
			}
			hyp := h.Handle
			if hyp == "" {
				hyp = h.ID
			}
			status, reason, err := reader.Read(ctx, ReadQuestion{Hypothesis: hyp, Statement: p.Statement, Content: spec.Content})
			if err != nil {
				return out, err
			}
			if err := postJSON(ctx, human, backendURL+"/api/interpretations", map[string]any{
				"investigation_ref":   invID,
				"interpretation_type": "prediction",
				"prediction_ref":      p.ID,
				"prediction_status":   status,
				"rationale":           "eval driver: result recorded by the scripted analyst from the scenario's authored answer (" + reason + ")",
				"attestation": map[string]any{
					"content": spec.Content,
					"source":  spec.Source,
					"channel": spec.Channel,
				},
			}, nil); err != nil {
				return out, fmt.Errorf("record result for %s: %w", p.ID, err)
			}
			out = append(out, AttestationRecord{Turn: turn, PredictionRef: p.ID, Hypothesis: hyp, Statement: p.Statement, Status: status, Reason: reason, Content: spec.Content})
		}
	}
	return out, nil
}

// readerFromEnv builds the driver's reader on the run's key. It is independent
// of RECKON_EVAL_JUDGE (which governs grading): a driver that cannot read the
// authored answer cannot answer a manual test at all.
func readerFromEnv(apiKey string) Reader {
	if apiKey == "" {
		return nil
	}
	return NewAnthropicJudge(apiKey, os.Getenv("RECKON_EVAL_JUDGE_MODEL"))
}

// ackProposedHypotheses acknowledges every PROPOSED hypothesis with the human
// token — the driver's scripted Adopt (TurnSpec.AckHypotheses). Returns how
// many it acknowledged.
func ackProposedHypotheses(ctx context.Context, client *agent.Client, backendURL string, human agent.TokenSource, invID string) (int, error) {
	n := 0
	for _, h := range hypothesesSnapshot(ctx, client, invID) {
		if h.Status != "PROPOSED" {
			continue
		}
		if err := postJSON(ctx, human, backendURL+"/api/interpretations", map[string]any{
			"investigation_ref":   invID,
			"interpretation_type": "hypothesis",
			"hypothesis_ref":      h.ID,
			"rationale":           "eval driver: adopted (the scripted analyst's Acknowledge)",
		}, nil); err != nil {
			return n, fmt.Errorf("acknowledge %s: %w", h.ID, err)
		}
		n++
	}
	return n, nil
}

// postJSON is the harness's minimal authenticated POST — for the two
// investigation-provisioning calls the agent Client deliberately does not
// carry (they are surface acts, not loop acts).
func postJSON(ctx context.Context, src agent.TokenSource, url string, in, out any) error {
	token, err := src.Token(ctx)
	if err != nil {
		return err
	}
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("%s: %d %s", url, resp.StatusCode, string(raw))
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// judgeFromEnv builds the run's judge (adr/0059): the BYOK key the run already
// holds, the model from RECKON_EVAL_JUDGE_MODEL (DefaultJudgeModel when unset).
// RECKON_EVAL_JUDGE=off grades with the deterministic anchors alone. Returns a
// typed nil-free Judge or nil.
func judgeFromEnv(apiKey string, logf func(string, ...any)) Judge {
	if apiKey == "" || strings.EqualFold(os.Getenv("RECKON_EVAL_JUDGE"), "off") {
		return nil
	}
	j := NewAnthropicJudge(apiKey, os.Getenv("RECKON_EVAL_JUDGE_MODEL"))
	if logf != nil {
		logf("eval: judge %s consulted where E3/R3 anchors miss (RECKON_EVAL_JUDGE=off to disable)", j.Model())
	}
	return j
}

// stampJudge records the judge's attribution and spend on the report. A judge
// that decided nothing leaves the attribution empty: every verdict was
// deterministic, whatever was available.
func stampJudge(report *Report, judge Judge) {
	lj, ok := judge.(*LLMJudge)
	if !ok || lj == nil {
		return
	}
	usage, calls := lj.Usage()
	if calls == 0 {
		return
	}
	report.Attribution.JudgeModel = lj.Model()
	report.Attribution.JudgeRubric = JudgeRubricVersion
	report.JudgeUsage = usage
	report.JudgeCalls = calls
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// envInt reads an integer env var, falling back to def when unset or
// unparseable.
func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// envDuration reads a Go duration env var (e.g. "5s"), falling back to def
// when unset or unparseable.
func envDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}
