package main

import (
	"os"
	"path/filepath"
	"regexp"
	goruntime "runtime"
	"strings"
	"testing"

	"tofu/interface/cli"
	"tofu/internal/konst"
)

const (
	doctorBodyWithNoKeyAndNoProjectRule = `
  ✗ claude-sub  no subscription is signed in, so no model can answer
    → tofu login claude-sub
  ✗ jev         there is no openrouter key, so jev judges no tool call
    → tofu login openrouter

access
  ✓ anthropic   subscription
  ✓ codex       subscription
  ✓ openrouter  money

shell
  ⚠ rtk  not on PATH, so bash commands run as asked  → RTK_INSTALL

rules
  ✓ library       binary · 9 points
  ○ 8 points      shadow · thresholds from the rule
    gate tool_gate@3
  ✗ shell_sift@1  unusable · jev.Key: missing_credential: OPENROUTER_KEY is
    not set and .env does not exist

state
  calibration  none
  ledger       empty
`

	doctorBodyWithAKeyAndAProjectRule = `
  ✗ claude-sub  no subscription is signed in, so no model can answer
    → tofu login claude-sub

access
  ✓ jev         key · .env
  ✓ anthropic   subscription
  ✓ codex       subscription
  ✓ openrouter  money

shell
  ⚠ rtk  not on PATH, so bash commands run as asked  → RTK_INSTALL

rules
  ✓ library       project · 1 of 9 points
  ○ 7 points      shadow · thresholds from the rule
  ● shell_sift@1  enforced · thresholds from the rule
  ⚠ tool_gate@3   shadow · no lock file for tool_gate@3

state
  calibration  none
  ledger       empty
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

	versionEnvelope = `{
  "tofu": "VERSION",
  "verb": "version",
  "ok": true,
  "at": "AT",
  "data": {
    "version": "VERSION",
    "commit": "COMMIT",
    "go": "GO"
  },
  "problems": []
}
`
)

var browserRow = regexp.MustCompile(`^  (⚠ .+? +no native host, so tofu cannot reach its tabs +→ tofu browser install|⚠ .+? +the native host names a tofu that is gone +→ tofu browser install|✓ .+? +native host installed)$`)

var envelopeMoment = regexp.MustCompile(`(?m)^  "at": "\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ"`)

func normalEnvelope(printed string, values map[string]string) string {
	printed = envelopeMoment.ReplaceAllString(printed, `  "at": "AT"`)
	printed = strings.Replace(printed, `"tofu": "`+konst.Version+`"`, `"tofu": "VERSION"`, 1)
	for name, value := range values {
		printed = strings.Replace(printed, `"`+name+`": "`+value+`"`, `"`+name+`": "`+strings.ToUpper(name)+`"`, 1)
	}
	return printed
}

func TestE2EDoctorReadsTheKeyAndTheRuleOfTheProjectItRunsIn(t *testing.T) {
	titleOf := regexp.MustCompile(`^Doctor · \d+\.\d+\.\d+\S* · go\S+ · \S+/\S+ +✗ not ready$`)
	for _, step := range []struct {
		name string
		env  string
		rule string
		body string
	}{
		{"a project with no key and no rule of its own", "", "", doctorBodyWithNoKeyAndNoProjectRule},
		{"a project carrying its own key and its own tool_gate@3", "OPENROUTER_KEY=sk-or-v1-thistestwroteit\n", projectToolGateRule, doctorBodyWithAKeyAndAProjectRule},
	} {
		p := newProject(t, "doctor")
		if step.env != "" {
			writeFile(t, p.dir, ".env", step.env)
			writeFile(t, p.dir, "library/general/rules/tool_gate@3.yaml", step.rule)
		}
		title, body, _ := strings.Cut(p.run(t, exitVerdict, "doctor"), "\n")
		if !titleOf.MatchString(title) {
			t.Fatalf("%s: the title is %q", step.name, title)
		}
		before, rest, found := strings.Cut(body, "\nbrowser\n")
		section, after, _ := strings.Cut(rest, "\n\n")
		for _, row := range strings.Split(section, "\n") {
			fresh := goruntime.GOOS == "windows" || strings.Contains(row, "no native host")
			if !found || !browserRow.MatchString(row) || !fresh {
				t.Fatalf("%s: the browser section is\n%s\nwant one row per browser, and in a fresh home on %s no native host", step.name, section, goruntime.GOOS)
			}
		}
		body = before + "\n" + after
		sameText(t, step.name, body, strings.ReplaceAll(step.body, "RTK_INSTALL", rtkInstall()))
	}
}

func TestE2EDoctorAndARunInTheSameHomeAgreeOnWhetherTheShellSiftCuts(t *testing.T) {
	stubRecordedJudgedReplies(t)
	for _, step := range []struct {
		name string
		env  []string
		row  string
		code int
	}{
		{"a fresh home with no key", nil, "  ✗ shell_sift@1  unusable · jev.Key: missing_credential: OPENROUTER_KEY is", exitUsage},
		{"a fresh home with a key and a recorded jev", []string{"OPENROUTER_KEY=stub-key-not-a-real-credential", judgeEndpointEnvar + "=" + os.Getenv(judgeEndpointEnvar)},
			"  ● shell_sift@1  enforced · thresholds from the rule\n", exitOK},
	} {
		p := newProject(t, "sift")
		writeFile(t, p.dir, ".tofu/tools/shell/proxy.yaml", "use: off\n")
		writeFile(t, p.dir, "long.txt", siftableOutput())
		writeFile(t, p.home, "deck.jsonl", `{"text":"reading it","tools":[{"name":"bash","args":{"command":"cat long.txt"}}]}`+"\n"+`{"text":"done"}`+"\n")
		env := append([]string{"USERPROFILE=" + p.home, "HOME=" + p.home, "TEMP=" + p.home, "TMP=" + p.home, "SystemRoot=" + os.Getenv("SystemRoot"), "PATH=" + os.Getenv("PATH")}, step.env...)
		page, _ := runBinary(t, p.dir, env, "doctor")
		var envelope struct{ Data doctorReport }
		printed, _ := runBinary(t, p.dir, env, "doctor", "--json")
		oneEnvelope(t, printed, &envelope)
		var said doctorRule
		for _, rule := range envelope.Data.Rules {
			if rule.Point == shellSiftPoint {
				said = rule
			}
		}
		ran, code := runBinary(t, p.dir, append(env, cassetteVariable+"="+filepath.Join(p.home, "deck.jsonl")), "run", "--dir", ".", "--no-gate", "read long.txt")
		events, _ := filepath.Glob(filepath.Join(p.home, ".tofu", "projects", "*", "sessions", "*", "events.jsonl"))
		session := ""
		for _, path := range events {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			session += string(raw)
		}
		cut := strings.Contains(session, "bytes of output removed, the judged method")
		refused := strings.TrimPrefix(strings.TrimSpace(ran), "tofu run: ")
		agree := (said.Mode == "enforced") == cut && (said.Unusable == "" || said.Unusable == refused)
		if !strings.Contains(page, "\n"+step.row) || code != step.code || !agree {
			t.Fatalf("%s: doctor says\n%s\nwant a row starting %q, and in JSON mode %q unusable %q; the run exited %d, want %d, and cut %v\n%s",
				step.name, page, step.row, said.Mode, said.Unusable, code, step.code, cut, ran)
		}
		t.Logf("%s: doctor mode %q unusable %q; the run exited %d, cut %v, and said %q", step.name, said.Mode, said.Unusable, code, cut, lastLine(ran))
	}
}

func TestE2EDoctorNamesTheRtkVersionOrThatAProjectTurnedItOff(t *testing.T) {
	standInProxyOnPath(t)
	for _, step := range []struct {
		name  string
		sheet string
		line  string
		use   string
	}{
		{"the shipped default with rtk on PATH", "", "  ✓ rtk  9.9.9 · rewrites every bash command  from library", "rtk"},
		{"a project saying use: off", "use: off\n", "  ○ rtk  off  from project", "off"},
	} {
		p := newProject(t, "doctor")
		if step.sheet != "" {
			writeFile(t, p.dir, ".tofu/tools/shell/proxy.yaml", step.sheet)
		}
		env := []string{"USERPROFILE=" + p.home, "HOME=" + p.home, "TEMP=" + p.home, "TMP=" + p.home, "SystemRoot=" + os.Getenv("SystemRoot"), "PATH=" + os.Getenv("PATH")}
		said, code := runBinary(t, p.dir, env, "doctor")
		if code != exitVerdict || !strings.Contains(said, "\nshell\n"+step.line) {
			t.Fatalf("%s: exit %d, want %d and a shell section starting %q\n%s", step.name, code, exitVerdict, step.line, said)
		}
		var envelope struct{ Data doctorReport }
		printed, _ := runBinary(t, p.dir, env, "doctor", "--json")
		oneEnvelope(t, printed, &envelope)
		if envelope.Data.Proxy.Use != step.use {
			t.Fatalf("%s: the JSON says use %q, want %q\n%s", step.name, envelope.Data.Proxy.Use, step.use, printed)
		}
		t.Logf("%s:\n%s", step.name, said)
	}
}

func TestE2EDoctorJSONIsOneEnvelopeThatCarriesTheBlockersAsProblems(t *testing.T) {
	var envelope struct {
		Tofu     string
		Verb     string
		OK       bool
		Data     doctorReport
		Problems []cli.Problem
	}
	oneEnvelope(t, newProject(t, "doctor").run(t, exitVerdict, "doctor", "--json"), &envelope)
	want := []cli.Problem{
		{What: "claude-sub: no subscription is signed in, so no model can answer", Hint: "tofu login claude-sub"},
		{What: "jev: there is no openrouter key, so jev judges no tool call", Hint: "tofu login openrouter"},
	}
	if envelope.Tofu != konst.Version || envelope.Verb != "doctor" || envelope.OK || envelope.Data.Verdict != doctorNotReady ||
		len(envelope.Problems) != len(want) || envelope.Problems[0] != want[0] || envelope.Problems[1] != want[1] || len(envelope.Data.Rules) != 9 {
		t.Fatalf("the envelope is %+v, want doctor, not ok, not ready, 9 rules and the problems %+v", envelope, want)
	}
}

func TestE2EVersionJSONIsOneEnvelope(t *testing.T) {
	printed := newProject(t, "version").run(t, exitOK, "version", "--json")
	var envelope struct{ Data versionReport }
	oneEnvelope(t, printed, &envelope)
	sameText(t, "tofu version --json", normalEnvelope(printed, map[string]string{
		"version": envelope.Data.Version, "commit": envelope.Data.Commit, "go": envelope.Data.Go,
	}), versionEnvelope)
}

func TestE2ENoColourWritesNoEscapeWhereForcedColourWritesOne(t *testing.T) {
	p := newProject(t, "colour")
	base := []string{"USERPROFILE=" + p.home, "HOME=" + p.home, "TEMP=" + p.home, "TMP=" + p.home, "SystemRoot=" + os.Getenv("SystemRoot"), "CLICOLOR_FORCE=1"}
	for _, args := range [][]string{{"doctor"}, {"usage"}, {"version"}, {"doctor", "--nope"}} {
		forced, _ := runBinary(t, p.dir, base, args...)
		plain, _ := runBinary(t, p.dir, append(base, "NO_COLOR=1"), args...)
		if !strings.Contains(forced, "\x1b") || strings.Contains(plain, "\x1b") {
			t.Errorf("tofu %s: forced colour wrote an ESC %v, want true; NO_COLOR wrote one %v, want false",
				strings.Join(args, " "), strings.Contains(forced, "\x1b"), strings.Contains(plain, "\x1b"))
		}
	}
}

func TestAnUnknownArgumentGoesToStderrAndExitsTwo(t *testing.T) {
	for _, verb := range []string{"doctor", "usage", "version"} {
		var out, errOut strings.Builder
		code := run([]string{verb, "--nope"}, strings.NewReader(""), &out, &errOut)
		if code != exitUsage || out.Len() > 0 || !strings.HasPrefix(errOut.String(), "✗ tofu "+verb+": unknown argument \"--nope\"\n  → tofu "+verb+" [") {
			t.Errorf("tofu %s --nope: exit %d, stdout %q, stderr %q", verb, code, out.String(), errOut.String())
		}
	}
}
