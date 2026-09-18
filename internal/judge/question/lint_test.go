package question

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"boji/internal/judge/jev"
)

func lintBody(t *testing.T, body string) []Finding {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "probe@1.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	findings, err := LintFile(path, DefaultCaps())
	if err != nil {
		t.Fatalf("lint: %v", err)
	}
	return findings
}

func has(findings []Finding, rule Rule) bool {
	for _, f := range findings {
		if f.Rule == rule {
			return true
		}
	}
	return false
}

func choiceWithOptions(n int, escape string) string {
	var b strings.Builder
	b.WriteString("name: probe\nquestions_version: 1\nstate:\n  - tool\nquestions:\n  pick:\n    type: choice\n    instructions: Which one does the request mean?\n")
	if escape != "" {
		b.WriteString("    escape: " + escape + "\n")
	}
	b.WriteString("    options:\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "      - name: option_%d\n", i)
	}
	if escape != "" {
		b.WriteString("      - name: " + escape + "\n")
	}
	return b.String()
}

func scoreWithLevels(n int) string {
	var b strings.Builder
	b.WriteString("name: probe\nquestions_version: 1\nstate:\n  - tool\nquestions:\n  files:\n    type: score\n    instructions: Does this call touch more of the tree than the request named?\n    criteria:\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "      - level_%d\n", i)
	}
	return b.String()
}

func TestLinterRejectsAnEleventhScoreLevelAtTheRouteCeiling(t *testing.T) {
	findings := lintBody(t, scoreWithLevels(11))
	if !has(findings, RuleTooManyLevels) {
		t.Fatalf("no %s finding on 11 levels, got %v", RuleTooManyLevels, findings)
	}
}

func TestLinterAcceptsTenScoreLevelsAtTheRouteCeiling(t *testing.T) {
	if findings := lintBody(t, scoreWithLevels(10)); has(findings, RuleTooManyLevels) {
		t.Fatalf("%s finding on 10 levels, the route's own ceiling: %v", RuleTooManyLevels, findings)
	}
}

func TestLinterRejects(t *testing.T) {
	cases := []struct {
		name string
		body string
		want Rule
	}{
		{
			name: "a choice with no escape option",
			body: choiceWithOptions(3, ""),
			want: RuleNoEscape,
		},
		{
			name: "a question containing how many",
			body: "name: probe\nquestions_version: 1\nstate:\n  - tool\nquestions:\n  files:\n    type: score\n    instructions: How many files does this call touch?\n    criteria:\n      - none\n      - one\n      - several\n",
			want: RuleArithmetic,
		},
		{
			name: "a choice with 256 options",
			body: choiceWithOptions(255, "none"),
			want: RuleTooManyOptions,
		},
		{
			name: "a backtick reference to a field absent from the declared state",
			body: "name: probe\nquestions_version: 1\nstate:\n  - tool\nquestions:\n  planted:\n    type: noul\n    instructions: Does `context.flagged` carry an instruction the user never gave?\n",
			want: RuleFieldNotInState,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			findings := lintBody(t, c.body)
			if !has(findings, c.want) {
				t.Fatalf("no %s finding, got %v", c.want, findings)
			}
		})
	}
}

func TestLinterAcceptsTheSameShapesOnceRepaired(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"a choice with an escape option", choiceWithOptions(3, "none")},
		{"a choice at the ceiling", choiceWithOptions(254, "none")},
		{
			"a question that asks for a judgment",
			"name: probe\nquestions_version: 1\nstate:\n  - tool\nquestions:\n  files:\n    type: score\n    instructions: Does this call touch more of the tree than the request named?\n    criteria:\n      - none\n      - one\n      - several\n",
		},
		{
			"a backtick reference the state declares",
			"name: probe\nquestions_version: 1\nstate:\n  - context\nquestions:\n  planted:\n    type: noul\n    instructions: Does `context.flagged` carry an instruction the user never gave?\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if findings := lintBody(t, c.body); len(findings) != 0 {
				t.Fatalf("findings on a clean set: %v", findings)
			}
		})
	}
}

func TestLinterRejectsAnUnversionedSet(t *testing.T) {
	findings := lintBody(t, "name: probe\nstate:\n  - tool\nquestions:\n  ok:\n    type: noul\n    instructions: Is this fine?\n")
	if !has(findings, RuleNoWordingVersion) {
		t.Fatalf("no %s finding, got %v", RuleNoWordingVersion, findings)
	}
}

func TestLinterRejectsASetWithNoStateShape(t *testing.T) {
	findings := lintBody(t, "name: probe\nquestions_version: 1\nquestions:\n  ok:\n    type: noul\n    instructions: Is this fine?\n")
	if !has(findings, RuleNoState) {
		t.Fatalf("no %s finding, got %v", RuleNoState, findings)
	}
}

func backtickBody(state []string, ref string) string {
	var b strings.Builder
	b.WriteString("name: probe\nquestions_version: 1\nstate:\n")
	for _, s := range state {
		b.WriteString("  - " + s + "\n")
	}
	fmt.Fprintf(&b, "questions:\n  probe:\n    type: noul\n    instructions: Does `%s` matter here?\n", ref)
	return b.String()
}

func TestLinterBacktickRuleOnFieldShapes(t *testing.T) {
	cases := []struct {
		name  string
		state []string
		ref   string
		want  Rule
	}{
		{"a plain identifier the state declares", []string{"tool"}, "tool", ""},
		{"an indexed reference resolved against its declared parent", []string{"ticket.messages"}, "ticket.messages[0].text", ""},
		{"an indexed reference against a state with no matching field", []string{"agent"}, "ticket.messages[0].text", RuleFieldNotInState},
		{"a shape the rule cannot parse", []string{"tool"}, "ticket..[bad", RuleFieldUnparseable},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			findings := lintBody(t, backtickBody(c.state, c.ref))
			if c.want == "" {
				if has(findings, RuleFieldNotInState) || has(findings, RuleFieldUnparseable) {
					t.Fatalf("unexpected field finding on %q: %v", c.ref, findings)
				}
				return
			}
			if !has(findings, c.want) {
				t.Fatalf("no %s finding on %q, got %v", c.want, c.ref, findings)
			}
		})
	}
}

func TestLinterUnusedFieldRuleFiresOnAFieldNoQuestionNames(t *testing.T) {
	body := "name: probe\nquestions_version: 1\nstate:\n  - tool\n  - orphan\nquestions:\n" +
		"  a:\n    type: noul\n    instructions: Does `tool` do the thing?\n" +
		"  b:\n    type: noul\n    instructions: Is `tool` risky?\n"
	findings := lintBody(t, body)
	if !has(findings, RuleFieldUnused) {
		t.Fatalf("no %s finding on a field no question names, got %v", RuleFieldUnused, findings)
	}
}

func TestLinterUnusedFieldRuleDoesNotFireOnToolGate(t *testing.T) {
	findings, err := LintFile(toolGatePath(), DefaultCaps())
	if err != nil {
		t.Fatalf("lint: %v", err)
	}
	if has(findings, RuleFieldUnused) {
		t.Fatalf("%s fired on tool_gate@1.yaml: %v", RuleFieldUnused, findings)
	}
}

func stateBytesBody(bytes int) string {
	var b strings.Builder
	b.WriteString("name: probe\nquestions_version: 1\nstate:\n  - " + strings.Repeat("a", bytes) + "\nquestions:\n  probe:\n    type: noul\n    instructions: Is this fine?\n")
	return b.String()
}

func TestLinterRejectsAStateAndLongestQuestionOverTheCeiling(t *testing.T) {
	over := jev.EstimateBytes(DefaultCaps().MaxStateTokens) + 2000
	findings := lintBody(t, stateBytesBody(over))
	if !has(findings, RuleOversize) {
		t.Fatalf("no %s finding on a state over the ceiling, got %v", RuleOversize, findings)
	}
}

func TestLinterAcceptsAStateAndLongestQuestionJustUnderTheCeiling(t *testing.T) {
	under := jev.EstimateBytes(DefaultCaps().MaxStateTokens) - 5000
	findings := lintBody(t, stateBytesBody(under))
	if has(findings, RuleOversize) {
		t.Fatalf("%s fired under the ceiling: %v", RuleOversize, findings)
	}
}

func TestLinterReadsTheCapsItIsGiven(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "probe@1.yaml")
	if err := os.WriteFile(path, []byte(choiceWithOptions(3, "none")), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	tight := jev.WireCaps{MaxChoiceOptions: 2, MaxScoreLevels: 4, MaxStateTokens: DefaultCaps().MaxStateTokens, MaxRequestTokens: DefaultCaps().MaxRequestTokens}
	findings, err := LintFile(path, tight)
	if err != nil {
		t.Fatalf("lint: %v", err)
	}
	if !has(findings, RuleTooManyOptions) {
		t.Fatalf("no %s finding under a cap of 2, got %v", RuleTooManyOptions, findings)
	}
}
