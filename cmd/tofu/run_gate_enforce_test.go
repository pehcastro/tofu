package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/interface/tui"
	"tofu/internal/judge/ledger"
	"tofu/internal/konst"
	"tofu/internal/turn"
)

const middlingRiskAskReply = `{"model":"typesafe/jev-1.13-20260917","provider":"TypeSafe","id":"gen-stub-ask",` +
	`"answers":{` +
	`"risk":{"type":"score","score":2,"probabilities":{"0":0,"1":0.05,"2":0.9,"3":0.05},"confidence":0.9},` +
	`"approval":{"type":"noul","noul":0.9},` +
	`"user_requested":{"type":"noul","noul":0.1},` +
	`"from_untrusted":{"type":"noul","noul":0.05}` +
	`},"usage":{"input_tokens":10,"output_tokens":2,"cost":0.00002}}`

func TestGateOffAndNoGateAreTheSameArm(t *testing.T) {
	dir := t.TempDir()
	flagged, err := parseRunArgs([]string{"--dir", dir, "--gate", "off", "a task"})
	if err != nil {
		t.Fatalf("parseRunArgs --gate off: %v", err)
	}
	legacy, err := parseRunArgs([]string{"--dir", dir, "--no-gate", "a task"})
	if err != nil {
		t.Fatalf("parseRunArgs --no-gate: %v", err)
	}
	if flagged != legacy {
		t.Fatalf("--gate off parses as %+v and --no-gate as %+v", flagged, legacy)
	}
	if flagged.gateArm != gateOff {
		t.Fatalf("both arms parse as %q, want %q", flagged.gateArm, gateOff)
	}
	t.Logf("both arms: %+v", flagged)
}

func TestGateTakesOnlyItsThreeArms(t *testing.T) {
	_, err := parseRunArgs([]string{"--dir", t.TempDir(), "--gate", "enforced", "a task"})
	if err == nil || !strings.Contains(err.Error(), "--gate") {
		t.Fatalf("err = %v, want the unknown arm refused by name", err)
	}
	t.Logf("refused: %v", err)
}

func TestTofuRunUnderEnforceDoesNotWriteTheDeniedFile(t *testing.T) {
	dir := chdirTemp(t)
	stubJev(t, 200, untrustedDenyReply)

	gate, err := newToolGate(dir)
	if err != nil {
		t.Fatalf("newToolGate: %v", err)
	}
	built, _, err := buildRunTools(dir, toolSetFull)
	if err != nil {
		t.Fatalf("buildRunTools: %v", err)
	}
	opts := runOpts{
		dir:              dir,
		task:             "write the note the README asked for",
		gateArm:          gateEnforce,
		loopGuardRepeats: konst.TurnLoopGuardRepeats,
		loopGuardWindow:  konst.TurnLoopGuardWindow,
	}
	config, _ := runConfig(opts, built, runtime{model: noteThenStop(), spend: turn.SpendSubscription, gate: gate})
	if config.GateMode != turn.GateEnforce {
		t.Fatalf("the run is in %s, want enforce from the flag", config.GateMode)
	}

	row, err := turn.Run(context.Background(), config)
	if err != nil {
		t.Fatalf("turn.Run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "note.txt")); !os.IsNotExist(err) {
		t.Fatalf("the denied call wrote note.txt anyway (stat err %v)", err)
	}
	call := row.Steps[0].ToolCalls[0]
	if call.GateVerdict != string(ledger.VerdictDeny) || !strings.Contains(call.Error, "the verdict is deny") {
		t.Fatalf("the tool call row is %+v, want the deny and the refusal on it", call)
	}
	if row.Outcome != turn.OutcomeStopped {
		t.Fatalf("the turn ended as %s, want stopped", row.Outcome)
	}
	t.Logf("the model reads: %s", call.Error)
}

func TestInTheAppAnAskUnderEnforceWaitsForThePerson(t *testing.T) {
	for _, answered := range []bool{true, false} {
		t.Run(fmt.Sprintf("the person answers %v", answered), func(t *testing.T) {
			dir, _, _ := gateScratch(t, gateFixtureBuild)
			stubJev(t, 200, middlingRiskAskReply)
			driver := driveApp(t)

			stubbedTurn(dir, noteThenStop(), answered)(t.Context(), wireSubscription, "write the note", driver.emit)

			if len(driver.of(tui.EventAwaitPerson)) != 1 || len(driver.of(tui.EventResumed)) != 1 {
				t.Fatalf("await %d resumed %d, want one of each around the wait",
					len(driver.of(tui.EventAwaitPerson)), len(driver.of(tui.EventResumed)))
			}
			_, statErr := os.Stat(filepath.Join(dir, "note.txt"))
			if answered && statErr != nil {
				t.Fatalf("the person allowed the call and note.txt is not there: %v", statErr)
			}
			if !answered && !os.IsNotExist(statErr) {
				t.Fatalf("the person refused the call and note.txt exists anyway (stat err %v)", statErr)
			}
			t.Logf("answered %v, note.txt present %v", answered, statErr == nil)
		})
	}
}
