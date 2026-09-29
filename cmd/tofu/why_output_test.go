package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"tofu/internal/golden"
	"tofu/internal/judge/ledger"
	"tofu/internal/konst"
	"tofu/internal/sys"
)

const (
	whyAllowed  = "2026-09-28-allowed"
	whyHeld     = "2026-09-28-held"
	whyDropped  = "2026-09-28-dropped"
	whyReplayed = "2026-09-28-replayed"
)

var whyNow = time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)

type whyCase struct {
	name string
	args []string
	code int
}

func whyCases() []whyCase {
	return []whyCase{
		{"why-id", []string{"why", whyAllowed}, exitOK},
		{"why-held", []string{"why", whyHeld}, exitOK},
		{"why-last", []string{"why", "--last", "2"}, exitOK},
		{"why-missing", []string{"why", "2026-09-28-nothing"}, exitVerdict},
		{"why-usage", []string{"why", "--nope"}, exitUsage},
		{"replay-set", []string{"replay", "--point", "tool_gate", "--set", "risk_ask_at=-1", "--verbose"}, exitOK},
		{"replay-same", []string{"replay", "--point", "tool_gate"}, exitOK},
		{"replay-no-decider", []string{"replay", "--point", "ask"}, exitOK},
		{"replay-usage", []string{"replay"}, exitUsage},
		{"label-id", []string{"label", whyAllowed, "deny"}, exitOK},
		{"label-refused", []string{"label", whyHeld, "ask"}, exitVerdict},
		{"label-usage", []string{"label", "maybe"}, exitUsage},
	}
}

func gateAnswers(risk, approval, requested, untrusted float64) []ledger.Answer {
	dist := make([]ledger.Slice, 4)
	for i := range dist {
		dist[i] = ledger.Slice{Option: strconv.Itoa(i)}
		if float64(i) == risk {
			dist[i].P = 1
		}
	}
	return []ledger.Answer{
		{Question: "risk", Wording: 1, Kind: ledger.AnswerScore, Score: risk, Dist: dist},
		{Question: "approval", Wording: 1, Kind: ledger.AnswerNoul, Noul: approval},
		{Question: "user_requested", Wording: 1, Kind: ledger.AnswerNoul, Noul: requested},
		{Question: "from_untrusted", Wording: 1, Kind: ledger.AnswerNoul, Noul: untrusted},
	}
}

func whyLedger(t *testing.T) {
	t.Helper()
	isolateHome(t)
	t.Setenv("NO_COLOR", "")
	t.Setenv("CLICOLOR_FORCE", "")
	project := filepath.Join(os.Getenv("USERPROFILE"), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(project)
	dir, err := sys.LogDir()
	if err != nil {
		t.Fatal(err)
	}
	unlocked := "no calibration lock"
	gate := func(id string, at time.Time, answers []ledger.Answer, verdict ledger.Verdict, reason ledger.Reason) ledger.Row {
		return ledger.Row{ID: id, At: at, Point: "tool_gate", Questions: "tool_gate", Version: 1, Build: "jev-2026-09-01",
			Model: "~typesafe/jev-latest", StateHash: "h-" + id, Fingerprint: "bash.5ee1f0a0", StateBuilder: "tool_state@1",
			State: json.RawMessage(`{"command":"ls"}`), Answers: answers, Verdict: verdict, Policy: "tool_gate", PolicyVersion: 1, Reason: &reason}
	}
	allowed := gate(whyAllowed, whyNow.Add(-2*time.Hour), gateAnswers(0, 0.9, 0.05, 0.02), ledger.VerdictAllow,
		ledger.Reason{Question: "risk", Comparison: "risk_ask_at", Threshold: 1.5, Mode: ledger.ModeEnforced})
	held := gate(whyHeld, whyNow.Add(-3*time.Hour), gateAnswers(2, 0.1, 0.92, 0.71), ledger.VerdictAsk,
		ledger.Reason{Question: "risk", Comparison: "risk_ask_at", Threshold: 1.5, Value: 2, Blocked: true, Mode: ledger.ModeShadow, ModeReason: &unlocked})
	held.State = json.RawMessage(`{"command":"git push --force","log":"` + strings.Repeat("x", 5000) + `"}`)
	dropped := gate(whyDropped, whyNow.Add(-time.Hour), nil, ledger.VerdictAsk,
		ledger.Reason{Question: "risk", Comparison: "unavailable_timeout", Mode: ledger.ModeEnforced})
	dropped.State, dropped.StateBuilder = nil, ""
	replayed := allowed
	replayed.ID, replayed.At, replayed.ReplayOf = whyReplayed, whyNow.Add(-30*time.Minute), whyAllowed
	writer := ledger.NewWriterWithClock(dir, func() time.Time { return whyNow })
	asked := ledger.Row{ID: "2026-09-28-asked", At: whyNow.Add(-4 * time.Hour), Point: "ask", Questions: "ask", Version: 1,
		StateHash: "h-asked", Verdict: ledger.VerdictAsk, Policy: "ask", PolicyVersion: 1}
	for _, row := range []ledger.Row{asked, held, allowed, dropped, replayed} {
		if _, err := writer.Append(row); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Backfill(whyHeld, ledger.Outcome{Kind: outcomeKindHandLabeled, Detail: "deny"}); err != nil {
		t.Fatal(err)
	}
}

func runWhyVerb(args []string) (int, string, string) {
	verbs := map[string]func([]string, io.Writer, io.Writer, func() time.Time) int{"why": whyVerb, "replay": replayVerb, "label": labelVerb}
	var out, errOut bytes.Buffer
	code := verbs[args[0]](args[1:], &out, &errOut, func() time.Time { return whyNow })
	return code, out.String(), errOut.String()
}

func TestWhyReplayAndLabelMatchTheirTextAndJSONGoldens(t *testing.T) {
	stamp := regexp.MustCompile(`(?m)^  "at": "[^"]*"`)
	printed := map[string]string{}
	for _, mode := range []string{"text", "json"} {
		t.Run(mode, func(t *testing.T) {
			whyLedger(t)
			for _, c := range whyCases() {
				asJSON := mode == "json" && c.code != exitUsage
				args := c.args
				if asJSON {
					args = append(append([]string{}, args...), jsonFlag)
				}
				code, out, errOut := runWhyVerb(args)
				if code != c.code {
					t.Errorf("%s exited %d, want %d\nstdout %s\nstderr %s", c.name, code, c.code, out, errOut)
				}
				if asJSON {
					if !json.Valid([]byte(out)) || errOut != "" {
						t.Errorf("%s --json wrote stdout that is not one document, or wrote stderr %q:\n%s", c.name, errOut, out)
					}
					out = strings.ReplaceAll(stamp.ReplaceAllString(out, `  "at": "AT"`), konst.Version, "VERSION")
				}
				printed[c.name+"."+mode+".golden"] = "exit " + strconv.Itoa(code) + "\n--- stdout\n" + out + "--- stderr\n" + errOut
			}
		})
	}
	for name, text := range printed {
		golden.Assert(t, name, text)
	}
}

func TestWhyReplayAndLabelWriteNoEscapeUnderNoColour(t *testing.T) {
	for _, noColour := range []bool{false, true} {
		whyLedger(t)
		t.Setenv("CLICOLOR_FORCE", "1")
		if noColour {
			t.Setenv("NO_COLOR", "1")
		}
		escaped := 0
		for _, c := range whyCases() {
			_, out, errOut := runWhyVerb(c.args)
			if strings.ContainsRune(out+errOut, 0x1b) {
				escaped++
			}
		}
		if noColour && escaped > 0 {
			t.Errorf("with NO_COLOR, %d verbs wrote an ESC byte", escaped)
		}
		if !noColour && escaped != len(whyCases()) {
			t.Errorf("with colour forced, %d of %d verbs wrote an ESC byte, so the NO_COLOR run proves nothing", escaped, len(whyCases()))
		}
	}
}
