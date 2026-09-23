package main

import (
	"slices"
	"strings"
	"testing"

	"tofu/internal/rule"
	"tofu/internal/turn"
)

func systemFor(t *testing.T, task string) string {
	t.Helper()
	opts, err := parseRunArgs([]string{"--dir", t.TempDir(), task})
	if err != nil {
		t.Fatal(err)
	}
	built, _, err := buildRunTools(opts.dir, opts.toolSet)
	if err != nil {
		t.Fatal(err)
	}
	config, _ := mustConfig(t, opts, built, runtime{spend: turn.SpendSubscription})
	return config.System
}

func TestTheSystemPromptCarriesTheTextOfEveryRuleThatFires(t *testing.T) {
	system := systemFor(t, "rename one symbol in internal/turn/loop.go")
	rules, _, err := loadRules("")
	if err != nil {
		t.Fatal(err)
	}
	fired := 0
	for _, one := range rules {
		if !one.Trigger.AlwaysOn() && one.ID != "comments" {
			continue
		}
		fired++
		if one.Text == "" {
			t.Fatalf("rule %s fires for a task naming a go file and declares no text, so nothing of it can reach the prompt", one.ID)
		}
		if !strings.Contains(system, one.Text) {
			t.Fatalf("rule %s fires for a task naming a go file and its text is not in the system prompt:\nwanted: %s\ngot:\n%s", one.ID, one.Text, system)
		}
	}
	if fired == 0 {
		t.Fatal("no rule on disk fires for a task naming a go file, so the assertion proves nothing")
	}
}

func TestATaskNamingNoPathAndNoVerbCarriesEveryAlwaysOnConcernAndEveryUnconditionalRule(t *testing.T) {
	opts, err := parseRunArgs([]string{"--dir", t.TempDir(), "rename one symbol"})
	if err != nil {
		t.Fatal(err)
	}
	environment, _ := runEnvironment(opts)
	composed, err := composePrompt(opts, environment)
	if err != nil {
		t.Fatal(err)
	}
	if len(composed.Task.Paths) != 0 || composed.Task.Verb != rule.VerbNone {
		t.Fatalf("the task was read as paths %v and verb %q, so the assertion is not about a bare task", composed.Task.Paths, composed.Task.Verb)
	}
	for _, always := range []rule.Concern{rule.ConcernEnvironment, rule.ConcernToolGuidance, rule.ConcernSafety, rule.ConcernOutputShape, rule.ConcernFormatContract} {
		if !slices.ContainsFunc(composed.Parts, func(p turn.PromptPart) bool { return p.Concern == always }) {
			t.Fatalf("the composed prompt carries nothing for %s, which is never conditional", always)
		}
	}

	rules, _, err := loadRules("")
	if err != nil {
		t.Fatal(err)
	}
	unconditional := 0
	for _, one := range rules {
		if one.Trigger.AlwaysOn() {
			unconditional++
		}
	}
	fired := 0
	for _, part := range composed.Parts {
		if part.RuleID != "" {
			fired++
		}
	}
	if fired != unconditional {
		t.Fatalf("%d rules are unconditional on disk and %d reached the prompt for a task naming no path and no verb", unconditional, fired)
	}
}

func TestShowPromptPrintsEveryPartItsRuleAndTheBytesComposingAdds(t *testing.T) {
	var out, errOut strings.Builder
	dir := t.TempDir()
	if code := runVerb([]string{"--dir", dir, "--show-prompt", "rename one symbol in internal/turn/loop.go"}, &out, &errOut); code != exitOK {
		t.Fatalf("tofu run --show-prompt exited %d: %s", code, errOut.String())
	}
	printed := out.String()
	for _, wanted := range []string{
		"paths the task names: internal/turn/loop.go",
		"from the rule comments in",
		"held ",
		"system message,",
		"first user message,",
		"composed system prompt",
	} {
		if !strings.Contains(printed, wanted) {
			t.Fatalf("tofu run --show-prompt never printed %q:\n%s", wanted, printed)
		}
	}
}

func TestARuleHeldBackByItsTriggerIsNotInTheSystemPrompt(t *testing.T) {
	system := systemFor(t, "rename one symbol in src/service/main.py")
	rules, _, err := loadRules("")
	if err != nil {
		t.Fatal(err)
	}
	for _, one := range rules {
		if one.ID != "comments" {
			continue
		}
		if one.Text == "" {
			t.Fatalf("rule %s declares no text, so its absence from the prompt proves nothing", one.ID)
		}
		if strings.Contains(system, one.Text) {
			t.Fatalf("rule %s is held back by its go trigger for a python task and its text is in the system prompt:\n%s", one.ID, system)
		}
		return
	}
	t.Fatal("the comments rule is not on disk, so the assertion proves nothing")
}
