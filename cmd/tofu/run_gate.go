package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"tofu/internal/judge/gate"
	"tofu/internal/judge/jev"
	jevwire "tofu/internal/judge/jev/wire/openrouter"
	typesafewire "tofu/internal/judge/jev/wire/typesafe"
	"tofu/internal/judge/ledger"
	"tofu/internal/judge/state"
	"tofu/internal/konst"
	"tofu/internal/llm/models"
	"tofu/internal/subagent"
	"tofu/internal/sys"
	"tofu/internal/transport"
	"tofu/internal/turn"
	"tofu/internal/turn/tools"
)

const oneCallAtATime = 1

const (
	runGatePoint       = "tool_gate@3"
	judgeEndpointEnvar = "TOFU_JUDGE_ENDPOINT"
)

type toolGate struct {
	client    *jev.Client
	set       battery
	cwd       string
	watch     func(ctx context.Context, tool string, decision turn.GateDecision, err error)
	decisions int
	costUSD   float64
}

type unusableRule struct{ err error }

func (u unusableRule) Error() string { return u.err.Error() }

func (u unusableRule) Unwrap() error { return u.err }

func newToolGate(dir string) (*toolGate, error) {
	set, err := resolvePoint(runGatePoint, dir)
	if err != nil {
		return nil, unusableRule{err}
	}
	client, err := newJevClient(oneCallAtATime)
	if err != nil {
		return nil, err
	}
	return &toolGate{client: client, set: set, cwd: dir}, nil
}

func resolvePoint(point, dir string) (battery, error) {
	set, err := resolveLibrary(point, dir)
	if err != nil {
		return battery{}, err
	}
	pol, origin, err := loadRulePoint(point, dir)
	if err != nil {
		return battery{}, err
	}
	if err := thresholdsInRange(pol, set); err != nil {
		return battery{}, err
	}
	resolution, err := resolveRuleMode(pol)
	if err != nil {
		return battery{}, err
	}
	pol = resolution.Rule
	set.Rule, set.Mode = &pol, resolution.Mode
	set.ModeReason = fmt.Sprintf("the rule came from %s as %s", origin, pol.File)
	if resolution.Reason != "" && !strings.Contains(resolution.Reason, pol.File) {
		set.ModeReason += "; " + resolution.Reason
	}
	return set, nil
}

func thresholdsInRange(r gate.Rule, set battery) error {
	topRisk := 0.0
	for _, q := range set.Questions {
		if q.ID == r.RiskQuestion {
			topRisk = float64(len(q.Levels) - 1)
		}
	}
	for _, field := range []struct {
		name  string
		value float64
		top   float64
	}{
		{"risk_ask_at", r.Thresholds.RiskAskAt, topRisk},
		{"risk_deny_at", r.Thresholds.RiskDenyAt, topRisk},
		{"user_requested_relax_at", r.Thresholds.UserRequestedRelaxAt, 1},
		{"approval_relax_at", r.Thresholds.ApprovalRelaxAt, 1},
		{"from_untrusted_block_at", r.Thresholds.FromUntrustedBlockAt, 1},
	} {
		if field.value < 0 || field.value > field.top {
			return fmt.Errorf("%s: thresholds.%s is %g and it reads between 0 and %g", r.File, field.name, field.value, field.top)
		}
	}
	return nil
}

func newJevClient(concurrency int) (*jev.Client, error) {
	key, err := gateKey()
	if err != nil {
		return nil, err
	}
	return jevClientOn(key, concurrency)
}

func boundClassifier() (models.Model, error) {
	dir, err := os.Getwd()
	if err != nil {
		return models.Model{}, err
	}
	loaded, err := modelLibrary(dir)
	var broken *models.BrokenLibrary
	if err != nil && !errors.As(err, &broken) {
		return models.Model{}, err
	}
	stored, err := sys.StoredKeys()
	if err != nil {
		return models.Model{}, err
	}
	return loaded.Classifier(stored)
}

func jevClientOn(key string, concurrency int) (*jev.Client, error) {
	classifier, err := boundClassifier()
	if err != nil {
		return nil, err
	}
	return jevClientFor(classifier.Provider, key, concurrency)
}

func jevClientFor(provider models.Provider, key string, concurrency int) (*jev.Client, error) {
	config := jevwire.Config{
		Key:      key,
		Endpoint: os.Getenv(judgeEndpointEnvar),
		Transport: transport.Config{
			AttemptTimeout: time.Duration(konst.JudgeTimeoutMillis) * time.Millisecond,
			Retries:        konst.JudgeRetries,
			Backoff:        time.Duration(konst.JudgeBackoffMillis) * time.Millisecond,
			Concurrency:    concurrency,
		},
	}
	var wire *jevwire.Wire
	var err error
	switch provider {
	case models.OpenRouter:
		wire, err = jevwire.New(config)
	case models.TypeSafe:
		wire, err = typesafewire.New(config)
	case models.Anthropic, models.OpenAI:
		return nil, fmt.Errorf("the classifier is served by %s, and jev is reached through %s or %s", provider, models.OpenRouter, models.TypeSafe)
	default:
		panic("tofu: unknown provider " + string(provider))
	}
	if err != nil {
		return nil, err
	}
	return jev.NewClient(jev.Config{Wire: wire})
}

func gateKey() (string, error) {
	classifier, err := boundClassifier()
	if err != nil {
		return "", err
	}
	return jev.KeyFor(sys.CredentialFileName, classifier.Provider.KeyName())
}

func (g *toolGate) Decide(ctx context.Context, request turn.GateRequest) (turn.GateDecision, error) {
	row, err := g.ask(ctx, request)
	decision := turn.GateDecision{ID: row.ID, Verdict: row.Verdict, Answers: row.Answers, Reason: row.Reason}
	if err != nil {
		decision = turn.GateDecision{Verdict: ledger.VerdictAsk}
	}
	if decision.Verdict != ledger.VerdictDeny && (request.Tool == (tools.Settings{}).Name() || changesTheHarness(g.cwd, request)) {
		decision.Verdict, decision.PersonOnly = ledger.VerdictAsk, true
	}
	if g.watch != nil {
		g.watch(ctx, request.Tool, decision, err)
	}
	return decision, err
}

func harnessFiles() []string {
	return []string{
		".tofu/settings.json", ".boji/settings.json",
		".tofu/hooks.json", ".boji/hooks.json",
		".tofu/hooks/trusted.json", ".boji/hooks/trusted.json",
		".claude/settings.json", ".claude/settings.local.json",
		".codex/hooks.json",
	}
}

func readOnlyPrograms() []string {
	return []string{"cat", "head", "tail", "less", "more", "grep", "egrep", "fgrep", "rg", "ls", "dir", "stat", "wc", "diff",
		"cd", "pushd", "popd", "echo", "printf", "type", "file", "jq", "sha256sum", "md5sum", "realpath", "readlink", "test",
		"get-content", "gc", "select-string", "sls"}
}

func changesTheHarness(project string, request turn.GateRequest) bool {
	var args struct{ Path, Command string }
	if json.Unmarshal(request.Args, &args) != nil {
		return false
	}
	switch request.Tool {
	case (&turn.WriteTool{}).Name(), (tools.Edit{}).Name():
		return reachesHarnessFile(project, args.Path)
	case (&turn.BashTool{}).Name():
		return shellChangesTheHarness(project, args.Command)
	}
	return false
}

func shellChangesTheHarness(project, command string) bool {
	readsOnly := true
	for _, step := range subagent.ShellSteps(command) {
		if slices.ContainsFunc(step.Changes, func(changed string) bool { return reachesHarnessFile(project, changed) }) {
			return true
		}
		if step.Program == "tofu" && (slices.Contains(step.Args, "settings") && slices.Contains(step.Args, "set") ||
			slices.Contains(step.Args, "hooks") && slices.Contains(step.Args, "trust")) {
			return true
		}
		readsOnly = readsOnly && len(step.Changes) == 0 && slices.Contains(readOnlyPrograms(), step.Program)
	}
	lower := strings.ToLower(command)
	return !readsOnly && slices.ContainsFunc(harnessFiles(), func(file string) bool {
		dir, _, _ := strings.Cut(file, "/")
		return strings.Contains(lower, dir) && strings.Contains(lower, path.Base(file))
	})
}

func reachesHarnessFile(project, written string) bool {
	full := written
	home, _ := os.UserHomeDir()
	for _, spelled := range []string{"~", "$HOME", "${HOME}", "$USERPROFILE", "${USERPROFILE}", "%USERPROFILE%", "$env:USERPROFILE"} {
		if rest, spelt := strings.CutPrefix(strings.ToLower(written), strings.ToLower(spelled)); spelt && (rest == "" || rest[0] == '/' || rest[0] == '\\') {
			full = home + written[len(spelled):]
			break
		}
	}
	if !filepath.IsAbs(full) {
		full = filepath.Join(project, full)
	}
	names := []string{full}
	if real, err := filepath.EvalSymlinks(full); err == nil {
		names = append(names, real)
	}
	if dir, err := filepath.EvalSymlinks(filepath.Dir(full)); err == nil {
		names = append(names, filepath.Join(dir, filepath.Base(full)))
	}
	if target, err := os.Readlink(full); err == nil {
		names = append(names, target)
	}
	return slices.ContainsFunc(names, namesHarnessFile)
}

func namesHarnessFile(name string) bool {
	segments := strings.Split(strings.ToLower(path.Clean(strings.ReplaceAll(name, `\`, "/"))), "/")
	for i, segment := range segments {
		segment, _, _ = strings.Cut(segment, ":")
		segments[i] = strings.TrimRight(segment, ". ")
	}
	named := "/" + strings.Join(segments, "/")
	for _, file := range harnessFiles() {
		parts := strings.Split(file, "/")
		for k := range parts {
			if strings.HasSuffix(named, "/"+strings.Join(parts[:k+1], "/")) {
				return true
			}
		}
	}
	return false
}

func (g *toolGate) ask(ctx context.Context, request turn.GateRequest) (ledger.Row, error) {
	var input map[string]any
	if err := json.Unmarshal(request.Args, &input); err != nil {
		return ledger.Row{}, fmt.Errorf("the %s call carries arguments the gate cannot read: %w", request.Tool, err)
	}
	call := state.ToolGateInput{
		Agent:      "tofu-run",
		Tool:       request.Tool,
		Input:      input,
		Cwd:        g.cwd,
		ProjectDir: g.cwd,
		Context:    state.ToolGateContext{UserRecentMessages: []string{request.Task}},
	}
	built, builder, err := state.BuildToolGateV3(call)
	if err != nil {
		return ledger.Row{}, err
	}
	written := rowInput{turnID: request.TurnID, stateBuilder: builder, fingerprint: state.FingerprintOf(call)}
	builtState := json.RawMessage(built)
	if own := turn.OwnShellsCalled(ctx, request); len(own) > 0 {
		return appendOwnShellRow(builtState, g.set, written, own)
	}
	decision, err := g.client.Ask(ctx, jev.Request{State: builtState, Questions: g.set.Questions})
	g.decisions++
	if err != nil {
		return appendFallbackRow(builtState, g.set, written, err)
	}
	g.costUSD += decision.Usage.Cost
	written.decision = &decision
	written.answers = toLedgerAnswers(g.set.QuestionsVersion, decision.Answers)
	return appendRow(builtState, g.set, written)
}

func appendOwnShellRow(state json.RawMessage, set battery, in rowInput, own []string) (ledger.Row, error) {
	dir, err := sys.LogDir()
	if err != nil {
		return ledger.Row{}, err
	}
	row, err := rowSkeleton(state, set, in)
	if err != nil {
		return ledger.Row{}, err
	}
	sentence := "the call acts on " + strings.Join(own, ", ") + ", which tofu started, so it is allowed without asking jev, and a kill runs as shell stop"
	row.Model, row.Answers, row.Verdict = "", []ledger.Answer{}, ledger.VerdictAllow
	row.Reason = &ledger.Reason{Mode: set.Mode.Ledger(), ModeReason: &sentence}
	if set.Rule != nil {
		row.Policy, row.PolicyVersion = set.Rule.Name, set.Rule.RuleVersion
	}
	return ledger.NewWriter(dir).Append(row)
}
