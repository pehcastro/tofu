package tools_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/llm"
	"tofu/internal/rule"
	"tofu/internal/turn"
	"tofu/internal/turn/tools"
)

const overrideArgs = `{"rule":"no_unit_test_after_code","reason_from_rule":"made up by the model","why_now":"the person asked for unit tests on the parser","change":"off"}`

type overrideRun struct {
	tool          tools.RuleOverride
	asked         int
	question      string
	project, home string
}

func newOverrideRun(t *testing.T, answer turn.PersonAnswer, askErr error) *overrideRun {
	t.Helper()
	run := &overrideRun{project: t.TempDir(), home: t.TempDir()}
	run.tool = tools.RuleOverride{
		Running: func() ([]rule.Rule, error) {
			return []rule.Rule{{ID: "no_unit_test_after_code", Version: 3, Text: "never a unit test after the code", Notes: "written after, it agrees with every bug"}}, nil
		},
		Project: run.project,
		Global:  run.home,
		Ask: func(_ context.Context, request turn.GateRequest, _ turn.GateDecision) (turn.PersonAnswer, error) {
			run.asked++
			var shown struct{ Question string }
			_ = json.Unmarshal(request.Args, &shown)
			run.question = shown.Question
			return answer, askErr
		},
	}
	return run
}

func (r *overrideRun) written(t *testing.T) map[string]string {
	t.Helper()
	found := map[string]string{}
	for layer, dir := range map[string]string{"project": r.project, "global": r.home} {
		data, err := os.ReadFile(filepath.Join(dir, "no_unit_test_after_code@1.yaml"))
		if err == nil {
			found[layer] = string(data)
		}
	}
	return found
}

func TestRuleOverrideWritesOnlyWhereThePersonSaidYes(t *testing.T) {
	for _, c := range []struct {
		answer turn.PersonAnswer
		layer  string
	}{{turn.PersonAllowedOnce, "project"}, {turn.PersonAlwaysHere, "global"}, {turn.PersonDenied, ""}} {
		run := newOverrideRun(t, c.answer, nil)
		result, err := run.tool.Run(context.Background(), json.RawMessage(overrideArgs))
		if err != nil {
			t.Fatalf("answer %v: %v", c.answer, err)
		}
		if !strings.Contains(run.question, "no_unit_test_after_code") || !strings.Contains(run.question, "written after, it agrees with every bug") || strings.Contains(run.question, "made up by the model") || !strings.Contains(run.question, "unit tests on the parser") {
			t.Errorf("answer %v: the person was asked %q, want the rule, its own note rather than the model's, and why now", c.answer, run.question)
		}
		written := run.written(t)
		if c.layer == "" {
			if len(written) != 0 || !strings.Contains(result.Content, "no") {
				t.Errorf("a no wrote %v, and told the model %q", written, result.Content)
			}
			continue
		}
		file := written[c.layer]
		if len(written) != 1 || !strings.Contains(file, "by: asked") || !strings.Contains(file, "overrides: no_unit_test_after_code@3") || !strings.Contains(file, "reason: the person asked for unit tests on the parser") || !strings.Contains(file, "mode: off") {
			t.Errorf("answer %v wrote %v, want one %s file by asked over @3 carrying why now", c.answer, written, c.layer)
		}
	}
}

func TestRuleOverrideWritesNothingWhenNobodyCanAnswer(t *testing.T) {
	run := newOverrideRun(t, turn.PersonAllowedOnce, errors.New("the app stopped taking answers"))
	if _, err := run.tool.Run(context.Background(), json.RawMessage(overrideArgs)); len(run.written(t)) != 0 {
		t.Errorf("a failed ask wrote %v (err %v)", run.written(t), err)
	}
	unattended := newOverrideRun(t, turn.PersonAllowedOnce, nil)
	unattended.tool.Ask = nil
	result, err := unattended.tool.Run(context.Background(), json.RawMessage(overrideArgs))
	if err != nil || len(unattended.written(t)) != 0 || !strings.Contains(result.Content, "no_unit_test_after_code") {
		t.Errorf("with no person the tool wrote %v, returned %q, %v", unattended.written(t), result.Content, err)
	}
}

func TestRuleOverrideNeverAsksAboutARuleThatDoesNotRunOrAnArgumentItCannotUse(t *testing.T) {
	for _, args := range []string{
		`{"rule":"no_such_rule","why_now":"w","change":"off"}`,
		`{"rule":"no_unit_test_after_code","why_now":"","change":"off"}`,
		`{"rule":"no_unit_test_after_code","why_now":"w","change":""}`,
		`{"rule":"no_unit_test_after_code","why_now":"two\nlines","change":"off"}`,
	} {
		run := newOverrideRun(t, turn.PersonAllowedOnce, nil)
		if _, err := run.tool.Run(context.Background(), json.RawMessage(args)); err == nil || run.asked != 0 || len(run.written(t)) != 0 {
			t.Errorf("%s: err %v, asked %d times, wrote %v", args, err, run.asked, run.written(t))
		}
	}
}

func TestRuleOverrideAsksThePersonOnceUnderAnEnforcedGate(t *testing.T) {
	run := newOverrideRun(t, turn.PersonAllowedOnce, nil)
	_, err := turn.Run(context.Background(), turn.Config{
		Model:    &scriptedModel{calls: []llm.ToolCall{{ID: "c1", Name: "rule_override", Arguments: json.RawMessage(overrideArgs)}}},
		Spend:    turn.SpendSubscription,
		Tools:    turn.NewRegistry(run.tool),
		Gate:     askingGate{},
		GateMode: turn.GateEnforce,
		Person:   run.tool.Ask,
		Task:     "write unit tests for the parser", ResultBytesCap: 4096, ArtifactDir: t.TempDir(),
	})
	if err != nil || run.asked != 1 || len(run.written(t)) != 1 {
		t.Errorf("the person was asked %d times and the tool wrote %v (err %v), want one ask and one file", run.asked, run.written(t), err)
	}
}

func TestRuleOverrideQuestionReadsWhateverWordsTheModelPassesAsWhyNow(t *testing.T) {
	for _, why := range []string{"you asked for unit tests on the parser.", "the person asked for unit tests on the parser", "unit tests on the parser"} {
		run := newOverrideRun(t, turn.PersonDenied, nil)
		args, _ := json.Marshal(map[string]string{"rule": "no_unit_test_after_code", "why_now": why, "change": "off"})
		if _, err := run.tool.Run(context.Background(), args); err != nil {
			t.Fatal(err)
		}
		want := "A rule stops me: no_unit_test_after_code.\nIts reason: written after, it agrees with every bug\nWhy now: " + why + "\nOverride it?"
		if run.question != want {
			t.Errorf("why_now %q asked\n%s\nwant\n%s", why, run.question, want)
		}
	}
}

func TestRuleOverrideNeverWritesOverAFileAlreadyThere(t *testing.T) {
	run := newOverrideRun(t, turn.PersonAllowedOnce, nil)
	existing := filepath.Join(run.project, "no_unit_test_after_code@1.yaml")
	if err := os.WriteFile(existing, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := run.tool.Run(context.Background(), json.RawMessage(overrideArgs))
	if data, _ := os.ReadFile(existing); err == nil || string(data) != "mine" {
		t.Errorf("the tool wrote over a file already there: %q, err %v", data, err)
	}
}

func TestRuleOverrideWritesAReplacementText(t *testing.T) {
	run := newOverrideRun(t, turn.PersonAllowedOnce, nil)
	if _, err := run.tool.Run(context.Background(), json.RawMessage(`{"rule":"no_unit_test_after_code","why_now":"w","change":"unit tests are the contract here"}`)); err != nil {
		t.Fatal(err)
	}
	if file := run.written(t)["project"]; !strings.Contains(file, "text: unit tests are the contract here") || strings.Contains(file, "mode: off") {
		t.Errorf("a replacement wrote %q", file)
	}
}
