package main

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"tofu/internal/sys"
)

const (
	doctorBodyWithNoKeyAndNoProjectRule = `
  claude-sub  no subscription is signed in, so no model can answer
              run tofu login claude-sub
  jev         there is no openrouter key, so jev judges no tool call
              run tofu login openrouter

  wires       anthropic and codex spend subscription quota, openrouter
                spends money

  library     the one in the binary, 9 points, nothing overrides it in
                library
  rules       8 points, all shadow, thresholds from the rule
              tool_gate@3 is the point the gate decides through
              shell_sift@1 shadow, no lock file for shell_sift@1
  calibration none, no lock in
                %s
  ledger      empty
`

	doctorBodyWithAKeyAndAProjectRule = `
  claude-sub  no subscription is signed in, so no model can answer
              run tofu login claude-sub

  jev         key from .env
  wires       anthropic and codex spend subscription quota, openrouter
                spends money

  library     the project's own, 1 of 9 points
  rules       7 points, all shadow, thresholds from the rule
              shell_sift@1 shadow, no lock file for shell_sift@1
              tool_gate@3 shadow, no lock file for tool_gate@3
  calibration none, no lock in
                %s
  ledger      empty
`

	projectToolGateRule = `name: tool_gate
domain: general
kind: threshold
rule_version: 3
questions: tool_gate
questions_version: 3
mode: enforced
sample_floor: 300

risk_question: risk
approval_question: approval
user_requested_question: user_requested
from_untrusted_question: from_untrusted

thresholds:
  risk_ask_at: 1.25
  risk_deny_at: 2.5
  user_requested_relax_at: 0.85
  approval_relax_at: 0.15
  from_untrusted_block_at: 0.5
`
)

func doctorParts(t *testing.T, printed string) (string, string, string) {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(printed, "\n"), "\n")
	if len(lines) < 3 {
		t.Fatalf("tofu doctor printed %d lines, too few to be a report:\n%s", len(lines), printed)
	}
	return lines[0], strings.Join(lines[1:len(lines)-1], "\n"), lines[len(lines)-1]
}

func TestE2EDoctorReadsTheKeyAndTheRuleOfTheProjectItRunsIn(t *testing.T) {
	calibration := func(p project) string {
		return filepath.Join(p.home, sys.StateDirName, sys.ProjectsDirName, sys.ProjectKey(p.root(t)), "calibration")
	}
	host := reportIndent + sys.GoVersion() + ", " + sys.OS() + "/" + sys.Arch() + ", in "

	headlineOf := regexp.MustCompile(`^tofu \d+\.\d+\.\d+\S* +not ready$`)

	bare := newProject(t, "bare")
	headline, body, trailer := doctorParts(t, bare.run(t, exitVerdict, "doctor"))
	if !headlineOf.MatchString(headline) {
		t.Fatalf("the headline of a project with nothing signed in is %q", headline)
	}
	sameText(t, "a project with no key and no rule of its own", body,
		fmt.Sprintf(doctorBodyWithNoKeyAndNoProjectRule, calibration(bare)))
	sameText(t, "the host line", trailer, host+bare.root(t))

	own := newProject(t, "own")
	writeFile(t, own.dir, ".env", "OPENROUTER_KEY=sk-or-v1-thistestwroteit\n")
	writeFile(t, own.dir, "library/general/rules/tool_gate@3.yaml", projectToolGateRule)
	headline, body, trailer = doctorParts(t, own.run(t, exitVerdict, "doctor"))
	if !headlineOf.MatchString(headline) {
		t.Fatalf("the headline of a project carrying its own key and rule is %q", headline)
	}
	sameText(t, "a project carrying its own key and its own tool_gate@3", body,
		fmt.Sprintf(doctorBodyWithAKeyAndAProjectRule, calibration(own)))
	sameText(t, "the host line", trailer, host+own.root(t))
}
