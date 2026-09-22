package rule

import (
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

const probeHead = "id: probe\ndomain: dev\nkind: structural\nchecker: comments\n"

func parseProbe(t *testing.T, body string) Rule {
	t.Helper()
	r, err := parseRule([]byte(probeHead+body), "library/dev/rules/probe@1.yaml")
	if err != nil {
		t.Fatalf("parseRule(%q): %v", body, err)
	}
	return r
}

func TestParseRuleRefusesAConcernThatIsNotOneOfTheTen(t *testing.T) {
	_, err := parseRule([]byte(probeHead+"concern: vibes\n"), "library/dev/rules/probe@1.yaml")
	if err == nil {
		t.Fatal("parseRule accepted a concern that is not one of the ten")
	}
	for _, want := range []string{"probe", "vibes", "code_rules", "task_shaping"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal does not name %q: %v", want, err)
		}
	}
}

func TestParseRuleRefusesARuleThatDeclaresNoConcern(t *testing.T) {
	_, err := parseRule([]byte(probeHead), "library/dev/rules/probe@1.yaml")
	if err == nil {
		t.Fatal("parseRule accepted a rule that declares no concern")
	}
	for _, want := range []string{"library/dev/rules/probe@1.yaml", "probe", "no concern", "output_shape", "identity"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal does not name %q: %v", want, err)
		}
	}
}

func TestTheFiveNeverConditionalConcernsRefuseEveryTrigger(t *testing.T) {
	five := []Concern{ConcernOutputShape, ConcernSafety, ConcernEnvironment, ConcernToolGuidance, ConcernFormatContract}
	triggers := []string{"scope: internal/**\n", "language: go\n", "task: write\n", "condition: (?i)release\n"}
	for _, concern := range five {
		for _, declared := range triggers {
			_, err := parseRule([]byte(probeHead+"concern: "+string(concern)+"\n"+declared), "library/dev/rules/probe@1.yaml")
			if err == nil {
				t.Fatalf("a %s rule declaring %q loaded, and %s is never conditional", concern, strings.TrimSpace(declared), concern)
			}
			if !strings.Contains(err.Error(), "probe") || !strings.Contains(err.Error(), string(concern)) {
				t.Fatalf("the refusal names neither the rule nor the concern: %v", err)
			}
		}
		if got := parseProbe(t, "concern: "+string(concern)+"\n"); !got.Trigger.AlwaysOn() {
			t.Fatalf("a %s rule with no trigger is not always on", concern)
		}
	}
	for _, concern := range []Concern{ConcernCodeRules, ConcernProcessDiscipline, ConcernDomainKnowledge, ConcernTaskShaping, ConcernIdentity} {
		if got := parseProbe(t, "concern: "+string(concern)+"\nscope: internal/**\n"); got.Trigger.AlwaysOn() {
			t.Fatalf("a %s rule declaring a scope read as always on", concern)
		}
	}
}

func TestTheLanguageTriggerFiresOnAGoPathAndHoldsBackAPythonRule(t *testing.T) {
	goRule := parseProbe(t, "concern: code_rules\nlanguage: go\n")
	pyRule := parseProbe(t, "concern: code_rules\nlanguage: python\n")
	goTask := Task{Paths: []string{"internal/rule/load.go"}}

	fires, why := goRule.Trigger.firesFor(goTask)
	if !fires || !strings.Contains(why, "internal/rule/load.go") {
		t.Fatalf("a go rule did not fire for a go path: %v %q", fires, why)
	}
	fires, why = pyRule.Trigger.firesFor(goTask)
	if fires {
		t.Fatalf("a python rule fired for a go path: %q", why)
	}
	if !strings.Contains(why, "python") {
		t.Fatalf("why = %q, want the language that reached nothing", why)
	}
	if fires, _ := pyRule.Trigger.firesFor(Task{Paths: []string{"tools/sift.py"}}); !fires {
		t.Fatal("a python rule did not fire for a python path")
	}
	if fires, why := goRule.Trigger.firesFor(Task{Paths: []string{"README.md"}}); fires {
		t.Fatalf("a go rule fired for a task naming no go file: %q", why)
	}
}

func TestTheScopeTriggerFiresInsideADirectoryAndHoldsBackOutsideIt(t *testing.T) {
	scoped := parseProbe(t, "concern: domain_knowledge\nscope: internal/judge/**\n")
	if fires, why := scoped.Trigger.firesFor(Task{Paths: []string{"internal/judge/jev/wire.go"}}); !fires {
		t.Fatalf("a scoped rule did not fire inside its directory: %q", why)
	}
	fires, why := scoped.Trigger.firesFor(Task{Paths: []string{"internal/turn/loop.go"}})
	if fires {
		t.Fatalf("a scoped rule fired outside its directory: %q", why)
	}
	if !strings.Contains(why, "internal/judge/**") {
		t.Fatalf("why = %q, want the scope that reached nothing", why)
	}
}

func TestTheTaskTriggerHoldsBackAWritingRuleOnADebugRequest(t *testing.T) {
	writing := parseProbe(t, "concern: process_discipline\ntask: write\n")
	fires, why := writing.Trigger.firesFor(Task{Verb: VerbDebug, Text: "find why the loop hangs"})
	if fires {
		t.Fatalf("a rule for writing fired on a debug request: %q", why)
	}
	if !strings.Contains(why, "debug") || !strings.Contains(why, "write") {
		t.Fatalf("why = %q, want both the request verb and the rule verb", why)
	}
	if fires, why := writing.Trigger.firesFor(Task{Verb: VerbWrite}); !fires {
		t.Fatalf("a rule for writing did not fire on a write request: %q", why)
	}
	if fires, why := writing.Trigger.firesFor(Task{}); fires {
		t.Fatalf("a rule for writing fired on a request naming no verb: %q", why)
	}
}

func TestParseRuleRefusesALanguageAndATaskItDoesNotKnow(t *testing.T) {
	_, err := parseRule([]byte(probeHead+"concern: code_rules\nlanguage: cobol\n"), "library/dev/rules/probe@1.yaml")
	if err == nil || !strings.Contains(err.Error(), "cobol") || !strings.Contains(err.Error(), "probe") {
		t.Fatalf("parseRule did not refuse an unknown language by name: %v", err)
	}
	_, err = parseRule([]byte(probeHead+"concern: process_discipline\ntask: refactor\n"), "library/dev/rules/probe@1.yaml")
	if err == nil || !strings.Contains(err.Error(), "refactor") || !strings.Contains(err.Error(), "explore") {
		t.Fatalf("parseRule did not refuse an unknown task verb by name: %v", err)
	}
}

func TestARequestNamingNoPathAndNoVerbGetsEveryUnconditionalRule(t *testing.T) {
	rules, err := LoadDir(filepath.Join("..", "..", "library"))
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	unconditional := map[string]bool{}
	for _, r := range rules {
		if r.Trigger.AlwaysOn() {
			unconditional[r.ID] = true
		}
	}
	if len(unconditional) == 0 {
		t.Fatal("no shipped rule is unconditional, so this test proves nothing")
	}
	fired := map[string]bool{}
	for _, m := range Index(rules, Task{}) {
		if m.Fires {
			fired[m.RuleID] = true
		}
	}
	if len(fired) != len(unconditional) {
		t.Fatalf("%d rules fired for a task naming nothing, want the %d unconditional rules on disk", len(fired), len(unconditional))
	}
	for id := range unconditional {
		if !fired[id] {
			t.Fatalf("the unconditional rule %q did not fire for a task naming nothing", id)
		}
	}
}

func TestLoadFSCarriesTheConcernThroughToTheIndex(t *testing.T) {
	rules, err := LoadFS(fstest.MapFS{
		"dev/rules/brevity@1.yaml":  {Data: []byte("id: brevity\ndomain: dev\nkind: structural\nchecker: em_dash\nconcern: output_shape\n")},
		"dev/rules/go_idiom@1.yaml": {Data: []byte("id: go_idiom\ndomain: dev\nkind: structural\nchecker: comments\nconcern: code_rules\nlanguage: go\n")},
	}, "library")
	if err != nil {
		t.Fatalf("LoadFS: %v", err)
	}
	for _, r := range rules {
		if r.ID == "brevity" && r.Concern != ConcernOutputShape {
			t.Fatalf("brevity carries the concern %q", r.Concern)
		}
		if r.ID == "go_idiom" && r.Concern != ConcernCodeRules {
			t.Fatalf("go_idiom carries the concern %q", r.Concern)
		}
	}
	for _, m := range Index(rules, Task{Paths: []string{"docs/plan.md"}}) {
		if m.RuleID == "brevity" && !m.Fires {
			t.Fatal("an output shape rule did not fire")
		}
		if m.RuleID == "go_idiom" && m.Fires {
			t.Fatal("a go rule fired for a task naming only markdown")
		}
	}
}
