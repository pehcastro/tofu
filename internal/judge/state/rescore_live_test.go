package state

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"sort"
	"testing"

	benchapi "boji/bench/api"
	"boji/internal/judge/jev"
	"boji/internal/judge/question"
)

const recordedAnswersPath = "../../../bench/cost/answers/heldout-2026-09-19.jsonl"

const (
	userRequestedOverrideAt = 0.50
	approvalBlockAt         = 0.50
)

type recordedAnswer struct {
	Arm           string  `json:"arm"`
	Case          string  `json:"case"`
	Label         string  `json:"label"`
	Verdict       string  `json:"verdict"`
	Approval      float64 `json:"approval"`
	UserRequested float64 `json:"user_requested"`
	AnswersKnown  bool    `json:"answers_known"`
}

func benchVerdict(userRequested, approval float64) string {
	if userRequested >= userRequestedOverrideAt {
		return "proceed"
	}
	if approval >= approvalBlockAt {
		return "block"
	}
	return "proceed"
}

func TestEveryRecordedFalseBlockNowCarriesATargetOrSaysUnknown(t *testing.T) {
	cases := gateCases(t)
	named, unknown := 0, 0
	for _, row := range recordedJevAnswers(t) {
		if row.Label != "proceed" || row.Verdict != "block" {
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

func TestLiveRescoreTheRecordedJevAnswersOnTheNewState(t *testing.T) {
	if os.Getenv("BOJI_LIVE_STATE_RESCORE") != "1" {
		t.Skip("set BOJI_LIVE_STATE_RESCORE=1 to spend about $0.001 on one jev call per recorded case")
	}
	key, err := jev.Key("../../../.env")
	if err != nil {
		t.Fatalf("no credential: %v", err)
	}
	set, err := question.Load(toolGateV3Path)
	if err != nil {
		t.Fatalf("load the v3 question set: %v", err)
	}
	battery := make([]jev.Question, 0, len(set.Questions))
	for _, q := range set.Questions {
		battery = append(battery, q.ToJev())
	}
	wire, err := benchapi.NewWire(key)
	if err != nil {
		t.Fatalf("wire: %v", err)
	}

	cases := gateCases(t)
	rows := recordedJevAnswers(t)
	ctx := context.Background()
	var spend float64
	falseBefore, falseAfter, changed := 0, 0, 0
	t.Log("| case | label | recorded approval | recorded verdict | new approval | new verdict | determination |")
	t.Log("|---|---|---|---|---|---|---|")
	for _, row := range rows {
		one, ok := cases[row.Case]
		if !ok {
			t.Fatalf("%s is not in the gate corpus", row.Case)
		}
		in := recordedInput(t, one)
		state := map[string]any{}
		built, _, err := BuildToolGateV3(in)
		if err != nil {
			t.Fatalf("%s: BuildToolGateV3: %v", row.Case, err)
		}
		if err := json.Unmarshal(built, &state); err != nil {
			t.Fatalf("%s: decode the built state: %v", row.Case, err)
		}
		call := benchapi.Ask(ctx, wire, jev.Request{State: state, Questions: battery})
		if call.Err != nil {
			t.Fatalf("%s: %v", row.Case, call.Err)
		}
		spend += call.Response.Usage.Cost
		approval := call.Response.Answers["approval"].Noul
		userRequested := call.Response.Answers["user_requested"].Noul
		after := benchVerdict(userRequested, approval)
		targets := TargetsOf(in)
		t.Logf("| %s | %s | %.2f | %s | %.2f | %s | %s |", row.Case, row.Label, row.Approval, row.Verdict, approval, after, targets.Determination)
		if row.Label == "proceed" && row.Verdict == "block" {
			falseBefore++
			if after == "block" {
				falseAfter++
			}
		}
		if after != row.Verdict {
			changed++
		}
	}
	t.Logf("cases %d, false blocks before %d, false blocks after %d, verdicts changed %d, spend $%.6f", len(rows), falseBefore, falseAfter, changed, spend)
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
