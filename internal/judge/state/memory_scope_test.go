package state

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"tofu/internal/judge/question"
	"tofu/internal/memory"
	"tofu/library/questions"
)

func memoryScopeSet(t *testing.T) question.Set {
	t.Helper()
	set, _, err := question.Resolve(MemoryScopeRef, []question.Layer{{Name: "library", Origin: "library/questions", FS: questions.Files()}})
	if err != nil {
		t.Fatalf("resolve %s: %v", MemoryScopeRef, err)
	}
	return set
}

func TestMemoryScopeStateSaysEveryFieldTheSetReadsEvenWhenEmpty(t *testing.T) {
	body, version, err := BuildMemoryScope(MemoryScopeState{Candidate: "never use em dashes"})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		t.Fatal(err)
	}
	set := memoryScopeSet(t)
	for _, name := range set.State {
		if _, present := fields[name]; !present {
			t.Errorf("the state the set reads as %q is missing from %s", name, body)
		}
	}
	if len(fields) != len(set.State) {
		t.Errorf("the builder sends %d fields and the set reads %d: %s against %v", len(fields), len(set.State), body, set.State)
	}
	if !strings.HasPrefix(version, MemoryScopePoint+".") {
		t.Errorf("builder version %q does not name the point", version)
	}
}

func TestMemoryScopeOffersEveryScopeAndEscapesToNone(t *testing.T) {
	set := memoryScopeSet(t)
	for _, finding := range question.Lint(set, question.DefaultCaps()) {
		t.Errorf("lint: %s", finding)
	}
	scope, ok := set.Question(MemoryScopeQuestion)
	if !ok || scope.Kind != question.KindChoice || scope.Escape != MemoryScopeNone {
		t.Fatalf("scope is %+v, want a choice escaping to %s", scope, MemoryScopeNone)
	}
	var offered []memory.Scope
	for _, option := range scope.Options {
		if option.Name == MemoryScopeNone {
			continue
		}
		parsed, err := memory.ParseScope(MemoryScopeOf(option.Name))
		if err != nil {
			t.Errorf("option %s is no memory scope: %v", option.Name, err)
		}
		offered = append(offered, parsed)
	}
	for _, want := range []memory.Scope{memory.Global, memory.UserLocal, memory.Project, memory.ProjectLocal} {
		if !slices.Contains(offered, want) {
			t.Errorf("the set never offers %s, offers %v", want, offered)
		}
	}
	for _, name := range []string{MemoryNamesPrivateQuestion, "durable", "stated"} {
		if q, ok := set.Question(name); !ok || q.Kind != question.KindNoul {
			t.Errorf("%s is %+v, want a noul", name, q)
		}
	}
}

func TestProjectLocalIsRefusedUnlessJevSaysNothingPrivate(t *testing.T) {
	for _, c := range []struct {
		name         string
		namesPrivate float64
		answered     bool
		refused      bool
	}{
		{"jev says it names a home path", 0.93, true, true},
		{"jev says it names nothing private", 0.04, true, false},
		{"jev cannot tell", 0.5, true, true},
		{"jev did not answer", 0, false, true},
	} {
		if got := RefusesProjectLocal(c.namesPrivate, c.answered); got != c.refused {
			t.Errorf("%s: refused %v, want %v", c.name, got, c.refused)
		}
	}
}
