package sift

import (
	"strings"
	"testing"
	"testing/fstest"
)

func policyFile(body string) fstest.MapFS {
	return fstest.MapFS{"shell_sift@1.yaml": {Data: []byte(body)}}
}

const shadowPolicy = "name: shell_sift\nschema: shell_sift\npolicy_version: 1\nquestions: shell_sift\nquestions_version: 1\nmode: shadow\nthresholds:\n  keep_at: 0.5\n"

func dropEverything(units []Unit) []Mark {
	marks := make([]Mark, len(units))
	for i, unit := range units {
		marks[i] = Mark{Reason: "the test drops every chunk it is allowed to drop"}
		if unit.Held != NotHeld {
			marks[i] = Mark{Keep: true, Reason: string(unit.Held)}
		}
	}
	return marks
}

func noisyShell() Shell {
	var body strings.Builder
	body.WriteString("the first line names the file\n")
	for i := 0; i < 80; i++ {
		body.WriteString("internal/filler/row.go\n")
	}
	body.WriteString("the last line carries the count\n")
	return Shell{
		Command:  "ls internal; echo ---; ls bench",
		Stdout:   body.String(),
		Stderr:   "ls: cannot access 'bench': No such file or directory\n",
		ExitCode: 2,
	}
}

func TestSplittingReproducesTheOutputByteForByte(t *testing.T) {
	result := noisyShell()
	units := SplitShell(result)
	if len(units) < 4 {
		t.Fatalf("the output split into %d units, too few to say anything", len(units))
	}
	if got := JoinUnits(units[:len(units)-2]); got != result.Stdout {
		t.Fatalf("joining the standard output units does not reproduce it:\n%q", got)
	}
	for i, unit := range units {
		if unit.Index != i {
			t.Fatalf("unit %d carries index %d", i, unit.Index)
		}
	}
}

func TestTheChunksNoJudgmentIsAskedAbout(t *testing.T) {
	result := noisyShell()
	units := SplitShell(result)
	stdout := 0
	for _, unit := range units {
		if unit.Stream == StandardOutput {
			stdout++
		}
	}

	cases := []struct {
		name  string
		index int
		want  Held
	}{
		{name: "the first unit of standard output", index: 0, want: HeldFirstUnit},
		{name: "the last unit of standard output", index: stdout - 1, want: HeldLastUnit},
		{name: "everything on standard error", index: stdout, want: HeldStderr},
		{name: "the exit status", index: len(units) - 1, want: HeldExit},
	}
	for _, each := range cases {
		t.Run(each.name, func(t *testing.T) {
			unit := units[each.index]
			if unit.Held != each.want {
				t.Fatalf("unit %d is held as %q, want %q: %q", each.index, unit.Held, each.want, unit.Text)
			}
			if mark := ShellCheap(unit); !mark.Keep {
				t.Fatalf("the free arm dropped it: %s", mark.Reason)
			}
			mark, err := DecideShell(unit, map[string]float64{NeededQuestion: 0.01}, 0.5)
			if err != nil {
				t.Fatalf("DecideShell: %v", err)
			}
			if !mark.Keep {
				t.Fatalf("the judgment dropped it at still_needed 0.01: %s", mark.Reason)
			}
			message := Message(units, dropEverything(units), ModeEnforced)
			if !strings.Contains(message, strings.TrimRight(unit.Text, "\n")) {
				t.Fatalf("an enforced sieve that drops everything it may still lost it:\n%s", message)
			}
		})
	}
}

func TestAnUnjudgedChunkIsRefusedRatherThanDropped(t *testing.T) {
	unit := Unit{Index: 4, Text: "internal/filler/row.go\n"}
	if _, err := DecideShell(unit, map[string]float64{"something_else": 0.9}, 0.5); err == nil {
		t.Fatal("a chunk with no still_needed answer was judged anyway")
	}
}

func TestShadowGivesTheModelTheOutputWhole(t *testing.T) {
	pol, err := LoadShellPolicy(policyFile(shadowPolicy), "shell_sift@1.yaml")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	result := noisyShell()
	units := SplitShell(result)
	marks := dropEverything(units)
	dropped := 0
	for _, mark := range marks {
		if !mark.Keep {
			dropped++
		}
	}
	if dropped == 0 {
		t.Fatal("the test dropped nothing, so shadow proves nothing")
	}

	whole := result.Stdout + result.Stderr + "the command exited 2\n"
	if got := Message(units, marks, pol.Mode); got != whole {
		t.Fatalf("shadow removed %d bytes:\n%q", len(whole)-len(got), got)
	}
	if got := Message(units, marks, ModeEnforced); got == whole {
		t.Fatal("enforced removed nothing, so shadow and enforced are the same thing")
	}
}

func TestTheThresholdIsThePolicysAndNotTheCodes(t *testing.T) {
	pol, err := LoadShellPolicy(policyFile(shadowPolicy), "shell_sift@1.yaml")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	unit := Unit{Index: 3, Text: "internal/filler/row.go\n"}
	answers := map[string]float64{NeededQuestion: 0.49}
	mark, err := DecideShell(unit, answers, pol.KeepAt)
	if err != nil {
		t.Fatalf("DecideShell: %v", err)
	}
	if mark.Keep {
		t.Fatalf("0.49 was kept against the policy's 0.5 cut: %s", mark.Reason)
	}
	if mark, _ := DecideShell(unit, answers, 0.4); !mark.Keep {
		t.Fatal("0.49 was dropped against a 0.4 cut, so the cut is not read from the policy")
	}
}

func TestAMalformedPolicyIsRefusedRatherThanDefaulted(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		refusal string
	}{
		{name: "a threshold outside the noul range", body: "name: shell_sift\nschema: shell_sift\nthresholds:\n  keep_at: 1.5\n", refusal: "keep_at is 1.5"},
		{name: "no threshold at all", body: "name: shell_sift\nschema: shell_sift\n", refusal: "keep_at is 0"},
		{name: "the gate's own policy handed to this loader", body: "name: tool_gate\nschema: gate\nthresholds:\n  risk_ask_at: 0.5\n", refusal: `schema is "gate"`},
		{name: "no schema at all reads as the gate's own schema", body: "name: shell_sift\nthresholds:\n  keep_at: 0.5\n", refusal: `unknown threshold "keep_at"`},
		{name: "a mode that is neither", body: "name: shell_sift\nschema: shell_sift\nmode: on\nthresholds:\n  keep_at: 0.5\n", refusal: "mode is"},
		{name: "a threshold that is not a number", body: "name: shell_sift\nschema: shell_sift\nthresholds:\n  keep_at: banana\n", refusal: `"keep_at" is a number`},
	}
	for _, each := range cases {
		t.Run(each.name, func(t *testing.T) {
			_, err := LoadShellPolicy(policyFile(each.body), "shell_sift@1.yaml")
			if err == nil {
				t.Fatal("the policy was accepted")
			}
			if !strings.Contains(err.Error(), each.refusal) {
				t.Fatalf("the refusal does not say %q: %v", each.refusal, err)
			}
		})
	}
}
