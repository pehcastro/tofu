package stopcheck

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"tofu/bench/corpus"
	"tofu/internal/judge/jev"
	"tofu/internal/judge/state"
)

const repoRoot = "../.."

func liveBattery(t *testing.T) (Battery, []Turn, []Skipped) {
	t.Helper()
	if os.Getenv("TOFU_LIVE") != "1" {
		t.Skip("set TOFU_LIVE=1 to put the question to the real jev route")
	}
	jev.AllowLiveCredential(t)
	key, err := jev.Key(filepath.Join(repoRoot, ".env"))
	if err != nil {
		t.Fatalf("no credential: %v", err)
	}
	battery, err := New(repoRoot, key)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	turns, skipped, err := ReadSessions(corpusDir)
	if err != nil {
		t.Fatalf("ReadSessions: %v", err)
	}
	return battery, turns, skipped
}

func askDirect(t *testing.T, b Battery, built state.StopCheckState) jev.Decision {
	t.Helper()
	raw, _, err := state.BuildStopCheck(built)
	if err != nil {
		t.Fatalf("BuildStopCheck: %v", err)
	}
	decision, err := b.client.Ask(context.Background(), jev.Request{State: json.RawMessage(raw), Questions: b.battery})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	return decision
}

func TestTheSameStateJudgedTwice(t *testing.T) {
	battery, turns, _ := liveBattery(t)
	var turn Turn
	for _, candidate := range turns {
		if candidate.ID == "turn-18d6913a528f4528" {
			turn = candidate
		}
	}
	if len(turn.Steps) < 9 {
		t.Skip("turn-18d6913a528f4528 is no longer in the frozen corpus with a ninth step, so this evidence cannot be rechecked")
	}
	built := StateAt(turn, 8)
	first := askDirect(t, battery, built)
	second := askDirect(t, battery, built)
	spread := 0.0
	for id, a := range first.Answers {
		b, ok := second.Answers[id]
		if !ok {
			continue
		}
		diff := math.Abs(answerValue(a) - answerValue(b))
		if diff > spread {
			spread = diff
		}
		t.Logf("%s: %.2f then %.2f", id, answerValue(a), answerValue(b))
	}
	t.Logf("the same step-9 state asked twice, largest spread across the battery: %.3f", spread)
	t.Logf("call 1: %s $%.6f. call 2: %s $%.6f", first.Latency, first.Usage.Cost, second.Latency, second.Usage.Cost)
}

func TestTheSameStateUnderTwoTasks(t *testing.T) {
	battery, turns, _ := liveBattery(t)
	var turn Turn
	for _, candidate := range turns {
		if candidate.ID == "turn-18d69bfed2bf52a0" {
			turn = candidate
		}
	}
	if len(turn.Steps) < 9 {
		t.Skip("turn-18d69bfed2bf52a0 is no longer in the frozen corpus with a ninth step, so this evidence cannot be rechecked")
	}
	onTask := StateAt(turn, 8)
	offTask := onTask
	offTask.Task = "how do I uninstall this software from a Windows machine"

	original := askDirect(t, battery, onTask)
	shifted := askDirect(t, battery, offTask)
	on := answerValue(original.Answers["stop_pressure"])
	off := answerValue(shifted.Answers["stop_pressure"])
	t.Logf("stop_pressure under the recorded task %q: %.2f (%s $%.6f)", turn.Task, on, original.Latency, original.Usage.Cost)
	t.Logf("stop_pressure under an unrelated task: %.2f (%s $%.6f)", off, shifted.Latency, shifted.Usage.Cost)
	if math.Abs(on-off) < 0.05 {
		t.Logf("stop_pressure barely moved between tasks (%.2f vs %.2f), unlike read_worth's answers_the_task", on, off)
	}
}

func answerValue(a jev.Answer) float64 {
	switch a.Kind {
	case jev.QuestionNoul:
		return a.Noul
	case jev.QuestionScore:
		return a.Score
	default:
		return 0
	}
}

func TestTheTypedOutcomeInTheNextState(t *testing.T) {
	dir := filepath.Join(repoRoot, ".tofu", "sessions")
	if _, err := os.Stat(dir); err != nil {
		t.Skipf(".tofu/sessions is not on this machine: %v", err)
	}
	frozen, _, err := ReadSessions(corpusDir)
	if err != nil {
		t.Fatalf("ReadSessions: %v", err)
	}
	walked, err := corpus.WalkSessions(dir)
	if err != nil {
		t.Fatalf("WalkSessions: %v", err)
	}
	live := make(map[string]corpus.Turn, len(walked.Turns))
	for _, turn := range walked.Turns {
		live[turn.ID] = turn
	}
	found, missing := 0, 0
	for _, turn := range frozen {
		if _, ok := live[turn.ID]; ok {
			found++
			continue
		}
		missing++
		t.Logf("%s: not found in .tofu/sessions, or present but unreadable (see Skipped)", turn.ID)
	}
	for _, skipped := range walked.Skipped {
		t.Logf("skipped by WalkSessions: %s: %s", skipped.Path, skipped.Reason)
	}
	t.Logf("%d of %d frozen-corpus turns are readable through corpus.WalkSessions(.tofu/sessions); %d missing or unreadable", found, len(frozen), missing)
	t.Logf("corpus.RecordedTurn (bench/corpus/reader.go) carries no Outcome field, so even the %d found turns cannot report whether the recorded turn actually ended by stopping, by error or by hitting the decision cap; that distinction exists in the raw session files (\"outcome\": \"stopped\"/\"error\"/\"decision_cap\") and is dropped by the shared reader before bench/stopcheck ever sees it", found)
}

func TestLiveBatteryOverEveryRecordedStep(t *testing.T) {
	battery, turns, skipped := liveBattery(t)
	result, err := battery.Run(context.Background(), turns, skipped)
	if err != nil {
		t.Fatalf("the battery stopped: %v", err)
	}
	cheap, typed := result.Agreements()
	t.Logf("%s", cheap.Line())
	t.Logf("%s", typed.Line())
	if typed.Total < 40 {
		t.Errorf("the battery decided %d labelled steps, BOJI-079 asks for at least forty", typed.Total)
	}

	body := Render(result)
	path := "report-" + result.GeneratedAt.UTC().Format("2006-01-02") + ".md"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	t.Logf("wrote bench/stopcheck/%s", path)
}
