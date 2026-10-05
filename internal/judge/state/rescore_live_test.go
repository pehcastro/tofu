package state

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	benchapi "tofu/bench/api"
	"tofu/internal/judge/gate"
	"tofu/internal/judge/jev"
	"tofu/internal/judge/question"
	"tofu/internal/konst"
	"tofu/internal/sys"
)

const (
	toolGateV5Path = "../../../library/questions/tool_gate@5.yaml"
	toolGateV6Path = "../../../library/questions/tool_gate@6.yaml"
)

var comparedSets = []string{toolGateV5Path, toolGateV6Path}

const (
	recordedAnswersPath = "../../../bench/cost/answers/heldout-2026-09-19.jsonl"
	gatePolicyPath      = "../../../library/general/rules/tool_gate@1.yaml"
	gateLibraryDir      = "../../../library"
	labelProceed        = "proceed"
	labelBlock          = "block"
)

func gateTheBenchReportDecidesThrough(t *testing.T) gate.Rule {
	t.Helper()
	pol, findings, err := gate.LintFile(gatePolicyPath, gateLibraryDir)
	if err != nil {
		t.Fatalf("read the policy the gate runs: %v", err)
	}
	if len(findings) > 0 {
		t.Fatalf("%s fails its own lint: %s", gatePolicyPath, findings[0])
	}
	if pol.Thresholds.RiskDenyAt <= pol.Thresholds.RiskAskAt {
		t.Fatalf("%s loaded deny at %.2f and ask at %.2f, so the thresholds did not come off disk", gatePolicyPath, pol.Thresholds.RiskDenyAt, pol.Thresholds.RiskAskAt)
	}
	return pol
}

type recordedAnswer struct {
	Arm           string  `json:"arm"`
	Case          string  `json:"case"`
	Label         string  `json:"label"`
	Verdict       string  `json:"verdict"`
	Approval      float64 `json:"approval"`
	UserRequested float64 `json:"user_requested"`
	AnswersKnown  bool    `json:"answers_known"`
}

func TestTheRescoreDecidesThroughThePolicyTheGateRuns(t *testing.T) {
	pol := gateTheBenchReportDecidesThrough(t)
	cut := pol.Thresholds
	band := konst.ThresholdDeadBand
	t.Logf("policy read from %s: risk asks at %.2f and denies at %.2f, user_requested relaxes above %.2f, approval relaxes below %.2f, from_untrusted blocks at %.2f",
		gatePolicyPath, cut.RiskAskAt, cut.RiskDenyAt, cut.UserRequestedRelaxAt, cut.ApprovalRelaxAt, cut.FromUntrustedBlockAt)
	approvalHeld := cut.ApprovalRelaxAt + 2*band
	approvalWithdrawn := cut.ApprovalRelaxAt - 2*band
	for _, want := range []struct {
		why     string
		run     scored
		verdict gate.Verdict
	}{
		{"risk clear below the ask cut", scored{risk: cut.RiskAskAt - 2*band, approval: approvalHeld}, gate.VerdictAllow},
		{"risk clear above the deny cut", scored{risk: cut.RiskDenyAt + 2*band, approval: approvalHeld}, gate.VerdictDeny},
		{"the user asked for it, so deny relaxes to ask", scored{risk: cut.RiskDenyAt + 2*band, approval: approvalHeld, userRequested: cut.UserRequestedRelaxAt + 2*band}, gate.VerdictAsk},
		{"untrusted content refuses the relax the user would have earned", scored{risk: cut.RiskDenyAt + 2*band, approval: approvalHeld, userRequested: cut.UserRequestedRelaxAt + 2*band, fromUntrusted: cut.FromUntrustedBlockAt}, gate.VerdictDeny},
		{"approval withdrawn below its own cut relaxes deny to ask on its own", scored{risk: cut.RiskDenyAt + 2*band, approval: approvalWithdrawn}, gate.VerdictAsk},
	} {
		if got := verdictAt(t, want.run, pol); got != want.verdict {
			t.Errorf("risk %.2f approval %.2f user_requested %.2f from_untrusted %.2f decided %q, want %q: %s",
				want.run.risk, want.run.approval, want.run.userRequested, want.run.fromUntrusted, got, want.verdict, want.why)
		}
	}
}

func TestEveryRecordedFalseBlockNowCarriesATargetOrSaysUnknown(t *testing.T) {
	cases := gateCases(t)
	named, unknown := 0, 0
	for _, row := range recordedJevAnswers(t) {
		if row.Label != labelProceed || row.Verdict != labelBlock {
			continue
		}
		one, ok := cases[row.Case]
		if !ok {
			t.Fatalf("%s is not in the gate corpus", row.Case)
		}
		targets := TargetsOf(recordedInput(t, one))
		t.Logf("%s: %s %+v", row.Case, targets.Determination, targets.Targets)
		switch targets.Determination {
		case TargetsResolved:
			if len(targets.Targets) == 0 {
				t.Errorf("%s: resolved and empty", row.Case)
			}
			named++
		case TargetUnknown:
			unknown++
		case NoWriteFound:
			t.Errorf("%s: a recorded write reads as no write at all", row.Case)
		}
	}
	t.Logf("recorded false blocks with answers: %d named, %d explicitly unknown", named, unknown)
}

type scored struct {
	risk          float64
	approval      float64
	userRequested float64
	fromUntrusted float64
}

func askOneCase(t *testing.T, wire jev.Wire, battery []jev.Question, one recordedCase) (scored, float64) {
	t.Helper()
	built, _, err := BuildToolGateV3(recordedInput(t, one))
	if err != nil {
		t.Fatalf("%s: BuildToolGateV3: %v", one.ID, err)
	}
	state := map[string]any{}
	if err := json.Unmarshal(built, &state); err != nil {
		t.Fatalf("%s: decode the built state: %v", one.ID, err)
	}
	call := benchapi.Ask(context.Background(), wire, jev.Request{State: state, Questions: battery})
	if call.Err != nil {
		t.Fatalf("%s: %v", one.ID, call.Err)
	}
	answers := call.Response.Answers
	return scored{
		risk:          answers["risk"].Score,
		approval:      answers["approval"].Noul,
		userRequested: answers["user_requested"].Noul,
		fromUntrusted: answers["from_untrusted"].Noul,
	}, call.Response.Usage.Cost
}

func askEveryCase(t *testing.T, wire jev.Wire, battery []jev.Question, rows []recordedAnswer) (map[string]scored, float64) {
	t.Helper()
	cases := gateCases(t)
	out := map[string]scored{}
	var spend float64
	for _, row := range rows {
		one, ok := cases[row.Case]
		if !ok {
			t.Fatalf("%s is not in the gate corpus", row.Case)
		}
		answers, cost := askOneCase(t, wire, battery, one)
		out[row.Case] = answers
		spend += cost
	}
	return out, spend
}

func batteryOf(t *testing.T, path string) []jev.Question {
	t.Helper()
	set, err := question.Load(path)
	if err != nil {
		t.Fatalf("load %s: %v", path, err)
	}
	battery := make([]jev.Question, 0, len(set.Questions))
	for _, q := range set.Questions {
		battery = append(battery, q.ToJev())
	}
	return battery
}

func setName(path string) string {
	return strings.TrimSuffix(filepath.Base(path), ".yaml")
}

const coinFlip = "unstable"

func verdictAt(t *testing.T, run scored, pol gate.Rule) gate.Verdict {
	t.Helper()
	verdict, _, err := gate.Decide(map[string]jev.Answer{
		pol.RiskQuestion:          {Kind: jev.QuestionScore, Score: run.risk},
		pol.ApprovalQuestion:      {Kind: jev.QuestionNoul, Noul: run.approval},
		pol.UserRequestedQuestion: {Kind: jev.QuestionNoul, Noul: run.userRequested},
		pol.FromUntrustedQuestion: {Kind: jev.QuestionNoul, Noul: run.fromUntrusted},
	}, pol)
	if err != nil {
		t.Fatalf("gate.Decide over %+v: %v", run, err)
	}
	return verdict
}

func steadyVerdict(t *testing.T, pol gate.Rule, first, second scored) string {
	t.Helper()
	one, two := verdictAt(t, first, pol), verdictAt(t, second, pol)
	if one != two {
		return coinFlip
	}
	if one == gate.VerdictAllow {
		return labelProceed
	}
	return labelBlock
}

func onTheCut(pol gate.Rule, runs ...scored) bool {
	for _, run := range runs {
		for _, pair := range [][2]float64{
			{run.risk, pol.Thresholds.RiskAskAt},
			{run.risk, pol.Thresholds.RiskDenyAt},
			{run.approval, pol.Thresholds.ApprovalRelaxAt},
			{run.userRequested, pol.Thresholds.UserRequestedRelaxAt},
			{run.fromUntrusted, pol.Thresholds.FromUntrustedBlockAt},
		} {
			if pair[0]-pair[1] <= konst.ThresholdDeadBand && pair[1]-pair[0] <= konst.ThresholdDeadBand {
				return true
			}
		}
	}
	return false
}

func TestLiveCompareTheTwoNewestQuestionSets(t *testing.T) {
	if os.Getenv("TOFU_LIVE_STATE_RESCORE") != "1" {
		t.Skip("set TOFU_LIVE_STATE_RESCORE=1 to spend about $0.007 on four jev calls per recorded case")
	}
	pol := gateTheBenchReportDecidesThrough(t)
	sys.AllowLiveCredential(t)
	key, err := jev.Key("../../../.env")
	if err != nil {
		t.Fatalf("no credential: %v", err)
	}
	wire, err := benchapi.NewWire(key)
	if err != nil {
		t.Fatalf("wire: %v", err)
	}

	rows := recordedJevAnswers(t)
	paths := comparedSets
	var spend float64
	runs := map[string][]map[string]scored{}
	for _, path := range paths {
		battery := batteryOf(t, path)
		for run := 0; run < 2; run++ {
			got, cost := askEveryCase(t, wire, battery, rows)
			runs[path] = append(runs[path], got)
			spend += cost
		}
	}

	t.Logf("every verdict below is gate.Decide over %s: %+v", gatePolicyPath, pol.Thresholds)
	header, rule := "| case | label |", "|---|---|"
	for _, path := range paths {
		header += fmt.Sprintf(" %s approval | %s user_requested | %s verdict | %s on the cut |", setName(path), setName(path), setName(path), setName(path))
		rule += "---|---|---|---|"
	}
	t.Log(header)
	t.Log(rule)
	improved, regressed, flipping, banded := 0, 0, 0, 0
	for _, row := range rows {
		line := fmt.Sprintf("| %s | %s |", row.Case, row.Label)
		verdicts := map[string]string{}
		bands := map[string]bool{}
		anyBand := false
		for _, path := range paths {
			first, second := runs[path][0][row.Case], runs[path][1][row.Case]
			verdicts[path] = steadyVerdict(t, pol, first, second)
			bands[path] = onTheCut(pol, first, second)
			anyBand = anyBand || bands[path]
			line += fmt.Sprintf(" %.2f %.2f | %.2f %.2f | %s | %t |",
				first.approval, second.approval, first.userRequested, second.userRequested, verdicts[path], bands[path])
		}
		t.Log(line)

		before, after := verdicts[paths[0]], verdicts[paths[1]]
		if anyBand {
			banded++
		}
		if before == coinFlip || after == coinFlip {
			flipping++
		}
		if bands[paths[0]] || bands[paths[1]] || before == coinFlip || after == coinFlip || before == after {
			continue
		}
		if after == row.Label {
			improved++
		}
		if before == row.Label {
			regressed++
		}
	}
	t.Logf("cases %d, %s against %s: improved %d, regressed %d, a coin flip in either %d, inside the dead band in any set %d, spend $%.6f",
		len(rows), setName(paths[1]), setName(paths[0]), improved, regressed, flipping, banded, spend)
}

func TestLiveReadEveryAnswerOnTheDisagreementCase(t *testing.T) {
	if os.Getenv("TOFU_LIVE_STATE_RESCORE") != "1" {
		t.Skip("set TOFU_LIVE_STATE_RESCORE=1 to spend about $0.0005 on one jev call per question set")
	}
	const disagreement = "tx-003"
	one, ok := gateCases(t)[disagreement]
	if !ok {
		t.Fatalf("%s is not in the gate corpus", disagreement)
	}
	key, err := jev.Key("../../../.env")
	if err != nil {
		t.Fatalf("no credential: %v", err)
	}
	wire, err := benchapi.NewWire(key)
	if err != nil {
		t.Fatalf("wire: %v", err)
	}
	pol := gateTheBenchReportDecidesThrough(t)
	targets := TargetsOf(recordedInput(t, one))
	t.Logf("%s labelled %s, tool %s, targets %s %+v", disagreement, one.Label, one.State.Tool, targets.Determination, targets.Targets)
	for _, path := range comparedSets {
		answers, cost := askOneCase(t, wire, batteryOf(t, path), one)
		t.Logf("%s on %s: risk %.2f, approval %.2f, user_requested %.2f, from_untrusted %.2f, verdict %s, $%.6f",
			disagreement, setName(path), answers.risk, answers.approval, answers.userRequested, answers.fromUntrusted, verdictAt(t, answers, pol), cost)
	}
}

func recordedJevAnswers(t *testing.T) []recordedAnswer {
	t.Helper()
	file, err := os.Open(recordedAnswersPath)
	if err != nil {
		t.Fatalf("open the recorded answers: %v", err)
	}
	defer func() { _ = file.Close() }()

	var out []recordedAnswer
	lines := bufio.NewScanner(file)
	lines.Buffer(make([]byte, 0, 1<<16), 1<<20)
	for lines.Scan() {
		var row recordedAnswer
		if err := json.Unmarshal(lines.Bytes(), &row); err != nil {
			t.Fatalf("decode a recorded answer: %v", err)
		}
		if row.Arm == "jev" && row.AnswersKnown {
			out = append(out, row)
		}
	}
	if err := lines.Err(); err != nil {
		t.Fatalf("read the recorded answers: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("no recorded jev answers carry the answers the rule reads")
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Case < out[j].Case })
	return out
}
