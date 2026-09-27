package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"tofu/interface/tui/progress"
	"tofu/interface/tui/session"
	"tofu/internal/konst"
	"tofu/internal/llm"
	settingspkg "tofu/internal/settings"
	"tofu/internal/sys"
)

func TestADrivenStepSettlesInsideOneProgressTick(t *testing.T) {
	if konst.ProgressTickMillis*time.Millisecond != progress.TickInterval {
		t.Fatalf("konst calls the progress tick %dms and the app ticks at %s", konst.ProgressTickMillis, progress.TickInterval)
	}
	if konst.DriveSettleMillis*time.Millisecond >= progress.TickInterval {
		t.Fatalf("a driven step waits %dms for quiet and the progress tick repaints every %s, so a running turn never looks quiet and every wait resolves at the timeout instead", konst.DriveSettleMillis, progress.TickInterval)
	}
}

func drivenProject(t *testing.T) string {
	t.Helper()
	dir := scratchProject(t)
	t.Setenv(cassetteVariable, "")
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("the note says a note"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func written(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const readingCassette = `{"text":"reading the note","tools":[{"name":"read","args":{"path":"note.txt"}}]}
{"text":"note.txt holds one line"}
`

func TestADrivenScriptSendsATaskAndPrintsWhatTheScreenShowed(t *testing.T) {
	printed := drivenRead(t, drivenProject(t))
	for _, want := range []string{"read note.txt", "note.txt holds one line", "cooked for"} {
		if !strings.Contains(printed, want) {
			t.Errorf("the printed screen never says %q:\n%s", want, printed)
		}
	}
}

const sleepingCassette = `{"text":"waiting on the shell","tools":[{"name":"bash","args":{"command":"sleep 3"}}]}
{"text":"THE ANSWER AFTER THE CANCEL"}
`

func TestTwoInterruptsEndADrivenTurnAndTheCassetteIsNeverAskedAgain(t *testing.T) {
	dir := drivenProject(t)
	deck := written(t, dir, "slow.cassette", sleepingCassette)
	script := written(t, dir, "stop.drive", strings.Join([]string{
		"wait " + session.Placeholder,
		"type wait for me",
		"key enter",
		"wait working",
		"key ctrl+c",
		"key ctrl+c",
		"wait cancelled at",
		"screen",
	}, "\n"))
	var out, errOut bytes.Buffer
	code := driveVerb([]string{script, "--cassette", deck, "--plain", "--timeout", "30s"}, strings.NewReader(""), &out, &errOut)
	if code != exitOK {
		t.Fatalf("tofu drive exited %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "stopping the turn") {
		t.Errorf("the screen never says the turn was stopped:\n%s", out.String())
	}
	if strings.Contains(out.String(), "THE ANSWER AFTER THE CANCEL") {
		t.Errorf("the cancelled turn was served the next recorded reply:\n%s", out.String())
	}
}

const halfAnswerCassette = `{"text":"the loop reads the policy first, then the wire, because a locked","unfinished":true}
`

func TestADrivenAnswerArrivesInDeltasSoAnInterruptKeepsTheHalfThatWasWritten(t *testing.T) {
	dir := drivenProject(t)
	deck := written(t, dir, "half.cassette", halfAnswerCassette)
	script := written(t, dir, "half.drive", strings.Join([]string{
		"wait " + session.Placeholder,
		"type why does the loop read the policy first?",
		"key enter",
		"wait because a locked",
		"key ctrl+c",
		"wait characters were written and kept in sub-agents",
		"key alt+2",
		"screen",
	}, "\n"))
	var out, errOut bytes.Buffer
	code := driveVerb([]string{script, "--cassette", deck, "--plain", "--timeout", "30s"}, strings.NewReader(""), &out, &errOut)
	if code != exitOK {
		t.Fatalf("tofu drive exited %d: %s", code, errOut.String())
	}
	for _, want := range []string{"answer, interrupted", "locked"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("sub-agents never says %q, so the answer arrived as one sealed event:\n%s", want, out.String())
		}
	}
}

func TestAWaitForAStateThatNeverArrivesFailsSayingWhatItWaitedFor(t *testing.T) {
	dir := drivenProject(t)
	deck := written(t, dir, "read.cassette", readingCassette)
	script := written(t, dir, "never.drive", strings.Join([]string{
		"wait " + session.Placeholder,
		"type read note.txt",
		"key enter",
		"wait the build is green",
	}, "\n"))
	var out, errOut bytes.Buffer
	code := driveVerb([]string{script, "--cassette", deck, "--plain", "--timeout", "2s"}, strings.NewReader(""), &out, &errOut)
	if code != exitVerdict {
		t.Fatalf("a wait that never arrives exited %d, wanted %d: %s", code, exitVerdict, errOut.String())
	}
	if !strings.Contains(errOut.String(), "the build is green") {
		t.Errorf("the failure never names what it waited for:\n%s", errOut.String())
	}
}

func TestADrivenTurnWithNoCassetteOpensNoWireAndSaysSo(t *testing.T) {
	dir := drivenProject(t)
	script := written(t, dir, "bare.drive", strings.Join([]string{
		"wait " + session.Placeholder,
		"type read note.txt",
		"key enter",
		"wait opens no live wire",
		"screen",
	}, "\n"))
	var out, errOut bytes.Buffer
	code := driveVerb([]string{script, "--plain", "--timeout", "30s"}, strings.NewReader(""), &out, &errOut)
	if code != exitOK {
		t.Fatalf("tofu drive exited %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "opens no live wire") {
		t.Errorf("the screen never says the wire stayed shut:\n%s", out.String())
	}
}

func TestTheCassetteCarriesTheEnvironmentBlockTheTurnSent(t *testing.T) {
	dir := drivenProject(t)
	deck := written(t, dir, "read.cassette", readingCassette)
	script := written(t, dir, "env.drive", strings.Join([]string{
		"wait " + session.Placeholder,
		"type read note.txt",
		"key enter",
		"wait cooked for",
		"environment",
	}, "\n"))
	var out, errOut bytes.Buffer
	code := driveVerb([]string{script, "--cassette", deck, "--timeout", "30s"}, strings.NewReader(""), &out, &errOut)
	if code != exitOK {
		t.Fatalf("tofu drive exited %d: %s", code, errOut.String())
	}
	for _, want := range []string{"<env>", "working directory: ", "</env>"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the environment block never says %q:\n%s", want, out.String())
		}
	}
}

func TestDriveReadsItsScriptFromStandardInput(t *testing.T) {
	drivenProject(t)
	var out, errOut bytes.Buffer
	code := driveVerb([]string{"-", "--plain", "--timeout", "30s"}, strings.NewReader("wait "+session.Placeholder+"\nscreen\n"), &out, &errOut)
	if code != exitOK {
		t.Fatalf("tofu drive exited %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), session.Placeholder) {
		t.Errorf("the screen read from standard input is:\n%s", out.String())
	}
}

type watchedInput struct{ read bool }

func (w *watchedInput) Read([]byte) (int, error) {
	w.read = true
	return 0, io.EOF
}

func TestANamedScriptNeverReadsStandardInput(t *testing.T) {
	dir := drivenProject(t)
	script := written(t, dir, "look.drive", "wait "+session.Placeholder+"\n")
	var out, errOut bytes.Buffer
	watched := &watchedInput{}
	if code := driveVerb([]string{script, "--timeout", "30s"}, watched, &out, &errOut); code != exitOK {
		t.Fatalf("tofu drive exited %d: %s", code, errOut.String())
	}
	if watched.read {
		t.Error("a named script read standard input too, so a run from a terminal waits for an end of file that never comes")
	}
}

func TestAnUnknownStepAndAnUnknownFlagAreRefusedByName(t *testing.T) {
	dir := drivenProject(t)
	script := written(t, dir, "bad.drive", "dance a jig\n")
	var out, errOut bytes.Buffer
	if code := driveVerb([]string{script}, strings.NewReader(""), &out, &errOut); code != exitUsage {
		t.Fatalf("an unknown step exited %d, wanted %d", code, exitUsage)
	}
	if !strings.Contains(errOut.String(), `no step is named "dance"`) {
		t.Errorf("the refusal never names the step:\n%s", errOut.String())
	}
	errOut.Reset()
	if code := driveVerb([]string{"--nonsense", "1"}, strings.NewReader(""), &out, &errOut); code != exitUsage {
		t.Fatalf("an unknown flag exited %d, wanted %d", code, exitUsage)
	}
	if !strings.Contains(errOut.String(), "no such flag") {
		t.Errorf("the refusal never says the flag is unknown:\n%s", errOut.String())
	}
}

func TestADrivenMouseStepThatCannotBePlayedFailsNamingItsLine(t *testing.T) {
	dir := drivenProject(t)
	for step, named := range map[string]string{
		"click 3 1 hyper":       "hyper",
		"drag 0 0 4 4 alt meta": "meta",
		"click 3":               "click X Y",
		"click -1 0":            "click X Y",
		"drag a b c d":          "drag X1 Y1 X2 Y2",
		"wheel 1 1 sideways":    "wheel X Y up|down",
		"wheel 1 1 up 0":        "wheel X Y up|down",
		"wheel 1 1 up 2 3":      "wheel X Y up|down",
		"resize 60":             "resize W H",
		"resize 60 20 30":       "resize W H",
		"resize 0 20":           "resize W H",
	} {
		script := written(t, dir, "mouse.drive", "# the step below is line 3\nscreen\n"+step+"\n")
		var out, errOut bytes.Buffer
		if code := driveVerb([]string{script}, strings.NewReader(""), &out, &errOut); code != exitUsage {
			t.Errorf("%q exited %d, wanted %d:\n%s", step, code, exitUsage, errOut.String())
		}
		if !strings.Contains(errOut.String(), "line 3:") || !strings.Contains(errOut.String(), named) {
			t.Errorf("%q failed without naming line 3 and %q:\n%s", step, named, errOut.String())
		}
	}
}

func TestADrivenClickAsTheFirstStepLandsOnTheFrameAPersonWouldSee(t *testing.T) {
	dir := drivenProject(t)
	run := func(script string) string {
		var out, errOut bytes.Buffer
		if code := driveVerb([]string{written(t, dir, "tab.drive", script), "--plain", "--width", "120", "--height", "30"}, strings.NewReader(""), &out, &errOut); code != exitOK {
			t.Fatalf("%q exited %d:\n%s", script, code, errOut.String())
		}
		return out.String()
	}
	label := "file edits"
	column, row, top := -1, -1, -1
	for at, line := range strings.Split(run("screen\n"), "\n") {
		if top < 0 && strings.HasPrefix(line, " tofu") {
			top = at
		}
		if before, _, found := strings.Cut(line, label); found && top >= 0 {
			column, row = len([]rune(before))+1, at-top
			break
		}
	}
	if column < 0 {
		t.Fatalf("no row of the first screen shows %q", label)
	}
	shown := run("click " + strconv.Itoa(column) + " " + strconv.Itoa(row) + "\nscreen\n")
	if !strings.Contains(shown, "no file has changed in this session") {
		t.Errorf("a click on column %d of row %d before any screen left the file edits view closed:\n%s", column, row, shown)
	}
}

type cancelledWhenAwaited struct {
	context.Context
	awaited chan struct{}
	once    sync.Once
}

func (c *cancelledWhenAwaited) Done() <-chan struct{} {
	c.once.Do(func() { close(c.awaited) })
	return c.awaited
}

func (c *cancelledWhenAwaited) Err() error {
	select {
	case <-c.awaited:
		return context.Canceled
	default:
		return nil
	}
}

func TestACassetteGivesACancelledTurnNoDeltaAndNoReply(t *testing.T) {
	deck, err := readCassette(written(t, t.TempDir(), "stray.cassette", `{"text":"THE STRAY ANSWER"}`+"\n"))
	if err != nil {
		t.Fatal(err)
	}
	var drawn []string
	stopped := &cancelledWhenAwaited{Context: context.Background(), awaited: make(chan struct{})}
	decision, err := deck.Ask(stopped, llm.Request{OnDelta: func(text string) { drawn = append(drawn, text) }})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("a turn stopped while its request was in flight was answered %q with error %v", decision.Content, err)
	}
	if len(drawn) != 0 {
		t.Errorf("a turn stopped while its request was in flight was still drawn %q", drawn)
	}
	if decision.Content != "" {
		t.Errorf("a turn stopped while its request was in flight was still handed %q", decision.Content)
	}
}

const addressedCassette = `{"text":"handing it to a sub-agent","tools":[{"name":"spawn","args":{"task":"read note.txt and say what it holds","owns":["note.txt"]}}]}
{"text":"the sub-agent read it and the note says a note"}
{"agent":"c1","text":"reading the note","tools":[{"name":"read","args":{"path":"note.txt"}}]}
{"agent":"c1","text":"the note says a note"}
`

func asked(t *testing.T, deck *cassette, task string) string {
	t.Helper()
	decision, err := deck.Ask(context.Background(), llm.Request{Messages: []llm.Message{
		{Role: llm.RoleSystem, Content: "the system prompt both conversations share"},
		{Role: llm.RoleUser, Content: task},
	}})
	if err != nil {
		t.Fatalf("%q was answered %v", task, err)
	}
	return decision.Content
}

func TestAnOrchestratorAndASubAgentEachTakeTheRepliesAddressedToThem(t *testing.T) {
	deck, err := readCassette(written(t, t.TempDir(), "addressed.cassette", addressedCassette))
	if err != nil {
		t.Fatal(err)
	}
	const orchestratorTask, subAgentBrief = "ask a sub-agent to read the note", "read note.txt and say what it holds"
	for _, want := range []struct{ conversation, reply string }{
		{orchestratorTask, "handing it to a sub-agent"},
		{subAgentBrief, "reading the note"},
		{subAgentBrief, "the note says a note"},
		{orchestratorTask, "the sub-agent read it and the note says a note"},
	} {
		if got := asked(t, deck, want.conversation); got != want.reply {
			t.Errorf("%q was handed %q, which belongs to the other caller, wanted %q", want.conversation, got, want.reply)
		}
	}
}

func TestACassetteWithNoReplyForASubAgentSaysSoNamingTheSubAgent(t *testing.T) {
	deck, err := readCassette(written(t, t.TempDir(), "flat.cassette", readingCassette))
	if err != nil {
		t.Fatal(err)
	}
	asked(t, deck, "the orchestrator task")
	_, err = deck.Ask(context.Background(), llm.Request{Messages: []llm.Message{{Role: llm.RoleUser, Content: "the sub-agent brief"}}})
	if err == nil {
		t.Fatal("a sub-agent was handed a reply from a cassette that addresses none to it")
	}
	if !strings.Contains(err.Error(), "sub-agent c1") {
		t.Errorf("the failure never names the caller that went unanswered: %v", err)
	}
}

func TestAnAgentThatIsNotASubAgentNumberIsRefusedByLine(t *testing.T) {
	_, err := readCassette(written(t, t.TempDir(), "named.cassette", `{"agent":"the sub-agent","text":"hello"}`+"\n"))
	if err == nil || !strings.Contains(err.Error(), "line 1") {
		t.Fatalf("an agent that is not c1, c2 and so on was read as %v", err)
	}
}

func drivenConversation(t *testing.T, deck, script string) string {
	t.Helper()
	var out, errOut bytes.Buffer
	if code := driveVerb([]string{script, "--cassette", deck, "--plain", "--timeout", "60s"}, strings.NewReader(""), &out, &errOut); code != exitOK {
		t.Fatalf("tofu drive exited %d: %s", code, errOut.String())
	}
	var spoken []string
	speaking := false
	for _, line := range strings.Split(out.String(), "\n") {
		said := strings.TrimSpace(strings.TrimRight(strings.TrimSpace(line), "┃│"))
		switch {
		case strings.HasPrefix(said, "[&"):
			speaking = true
			spoken = append(spoken, strings.Fields(said)[0])
		case said == "" || strings.HasPrefix(said, "·"):
			speaking = false
		case speaking && !strings.HasPrefix(said, "[tool#"):
			spoken = append(spoken, said)
		}
	}
	return strings.Join(spoken, "\n")
}

func TestASpawnDrivenTwiceProducesTheSameConversationBothTimes(t *testing.T) {
	dir := drivenProject(t)
	deck := written(t, dir, "spawn.cassette", addressedCassette)
	script := written(t, dir, "spawn.drive", strings.Join([]string{
		"wait " + session.Placeholder,
		"type ask a sub-agent to read the note",
		"key enter",
		"wait cooked for",
		"screen",
	}, "\n"))
	first := drivenConversation(t, deck, script)
	if !strings.Contains(first, "the sub-agent read it and the note says a note") {
		t.Fatalf("the orchestrator never reached the reply addressed to it:\n%s", first)
	}
	if second := drivenConversation(t, deck, script); second != first {
		t.Errorf("the same script ran twice and the two conversations differ:\n%s\n\n%s", first, second)
	}
}

func pollutedHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	state := filepath.Join(home, sys.StateDirName)
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	written(t, state, settingspkg.FileName, `{"chatShowsTools":1}`)
	return home
}

func drivenTask(t *testing.T, dir, name, deck, task string, args ...string) string {
	t.Helper()
	script := written(t, dir, name+".drive", strings.Join([]string{
		"wait " + session.Placeholder,
		"type " + task,
		"key enter",
		"wait cooked for",
		"screen",
	}, "\n"))
	var out, errOut bytes.Buffer
	code := driveVerb(append([]string{script, "--cassette", deck, "--plain", "--timeout", "60s"}, args...), strings.NewReader(""), &out, &errOut)
	if code != exitOK {
		t.Fatalf("tofu drive exited %d: %s", code, errOut.String())
	}
	return out.String()
}

func drivenRead(t *testing.T, dir string, args ...string) string {
	t.Helper()
	return drivenTask(t, dir, "read", written(t, dir, "read.cassette", readingCassette), "read note.txt", args...)
}

func settingsSaid(t *testing.T, printed, key string) string {
	t.Helper()
	for _, line := range strings.Split(printed, "\n") {
		if fields := strings.Fields(line); len(fields) >= 3 && fields[1] == key {
			return strings.Join(fields[2:], " ")
		}
	}
	t.Fatalf("the output never names the setting %s it resolved:\n%s", key, printed)
	return ""
}

const foldedToolRow, fullToolRow = "(1) tools", "⟩ read note.txt"

func TestADrivenRunIgnoresASettingsFilePlantedInTheAmbientHome(t *testing.T) {
	dir := drivenProject(t)
	pollutedHome(t)
	printed := drivenRead(t, dir)
	if strings.Contains(printed, fullToolRow) {
		t.Errorf("the run drew the full tool row, so it took chatShowsTools from a home the script never named:\n%s", printed)
	}
	if !strings.Contains(printed, foldedToolRow) {
		t.Errorf("the run drew neither the folded count nor the full row, so the screen proves nothing:\n%s", printed)
	}
	if said := settingsSaid(t, printed, settingspkg.ChatShowsTools); said != "false default" {
		t.Errorf("the output says chatShowsTools resolved to %q, wanted the declared default", said)
	}
}

func TestADrivenRunReadsTheSettingsOfAHomeTheScriptNamesAndSaysWhere(t *testing.T) {
	dir := drivenProject(t)
	home := pollutedHome(t)
	printed := drivenRead(t, dir, "--home", home)
	if !strings.Contains(printed, fullToolRow) {
		t.Errorf("--home named a home holding chatShowsTools and the run folded the tool row anyway:\n%s", printed)
	}
	said := settingsSaid(t, printed, settingspkg.ChatShowsTools)
	if !strings.HasPrefix(said, "true global ") || !strings.Contains(said, home) {
		t.Errorf("the output says chatShowsTools resolved to %q, wanted true out of a file under %s", said, home)
	}
}

func TestDriveHelpPrintsItsOwnUsage(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := driveVerb([]string{"--help"}, strings.NewReader(""), &out, &errOut); code != exitOK {
		t.Fatal("tofu drive --help did not exit 0")
	}
	for _, want := range []string{"usage: tofu drive", "wait TEXT", "environment", cassetteVariable, "click X Y", "drag X1 Y1 X2 Y2", "wheel X Y up|down", "resize W H", "zero-based"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the usage never says %q:\n%s", want, out.String())
		}
	}
}

const siftCassette = `{"text":"reading the build log","tools":[{"name":"bash","args":{"command":"cat build.log"}}]}
{"text":"the build log is read"}
`

func bulkyLog() string {
	var log strings.Builder
	for block := 1; block <= 4; block++ {
		for line := 1; line <= 20; line++ {
			log.WriteString("block " + strconv.Itoa(block) + " line " + strconv.Itoa(line) + " of the build log, compiling a package and saying so\n")
		}
		log.WriteString("\n")
	}
	return log.String()
}

func drivenSift(t *testing.T, arms ...string) string {
	t.Helper()
	dir := drivenProject(t)
	written(t, dir, "build.log", bulkyLog())
	deck := written(t, dir, "sift.cassette", siftCassette)
	return drivenTask(t, dir, "sift", deck, "read the build log", append([]string{"--home", pollutedHome(t)}, arms...)...)
}

var resultRow = regexp.MustCompile(`(\d+) lines,`)

func linesShown(t *testing.T, screen string) int {
	t.Helper()
	found := resultRow.FindStringSubmatch(screen)
	if found == nil {
		t.Fatalf("no tool result row on the screen says how many lines it holds:\n%s", screen)
	}
	count, err := strconv.Atoi(found[1])
	if err != nil {
		t.Fatal(err)
	}
	return count
}

func TestDriveTakesTheRunArmsThatChangeWhatATurnDoes(t *testing.T) {
	plan, taken := driveArgs([]string{
		"--sift", siftFree, "--gate", gateShadow, "--tools", toolSetThree,
		"--no-subagents", "--no-instructions", "--max-steps", "7", "--context-ceiling", "20000",
	}, io.Discard)
	if !taken {
		t.Fatal("tofu drive refused the arms tofu run takes")
	}
	want := runOpts{siftArm: siftFree, gateArm: gateShadow, toolSet: toolSetThree, noSubAgents: true, noInstructions: true, maxSteps: 7, contextCeiling: 20000}
	if plan.arms != want {
		t.Fatalf("drive parsed %+v, want %+v", plan.arms, want)
	}
	if _, took := driveArgs([]string{"--gate", "bogus"}, io.Discard); took {
		t.Fatal("tofu drive took --gate bogus, which the app would panic on")
	}
}

const subAgentOnCodexCassette = `{"text":"reading the note first","tools":[{"name":"read","args":{"path":"note.txt"}}]}
{"text":"handing it to go-dev","tools":[{"name":"spawn","args":{"agent":"go-dev","task":"wait a moment, then say done","owns":["note.txt"]}}]}
{"text":"go-dev is done"}
{"agent":"c1","text":"waiting","tools":[{"name":"bash","args":{"command":"sleep 8"}}]}
{"agent":"c1","text":"done"}
`

const bothQuotas = `[{"label":"claude-sub 5h","fraction":0.62,"reported":true,"resetsIn":"3h28m"},
{"label":"codex-sub 5h","fraction":0.4,"reported":true,"resetsIn":"1h"}]`

func TestADrivenFooterShowsASubAgentsSubscriptionOnlyWhileTheSubAgentRuns(t *testing.T) {
	dir := drivenProject(t)
	if err := os.MkdirAll(filepath.Join(dir, ".tofu", "agents"), 0o700); err != nil {
		t.Fatal(err)
	}
	written(t, filepath.Join(dir, ".tofu", "agents"), "go-dev.md", "---\nname: go-dev\ndescription: writes go\nmodel: codex-sub/gpt-5.6-sol\n---\n\nYou write Go.\n")
	deck := written(t, dir, "codex.cassette", subAgentOnCodexCassette)
	script := written(t, dir, "footer.drive", strings.Join([]string{
		"wait " + session.Placeholder,
		"type ask go-dev to wait",
		"key enter",
		"wait claude 62%  |  codex 40%",
		"wait cooked for",
		"absent codex 40%",
		"wait claude-sub 5h 62%",
	}, "\n"))
	var out, errOut bytes.Buffer
	args := []string{script, "--cassette", deck, "--plain", "--timeout", "60s", "--width", "140", "--height", "36", "--source", "claude-sub", "--quota", written(t, dir, "q.json", bothQuotas)}
	if code := driveVerb(args, strings.NewReader(""), &out, &errOut); code != exitOK {
		t.Fatalf("tofu drive exited %d: %s", code, errOut.String())
	}
}

func TestDriveForcesTheFreeSiftArmAndTheAppCutsTheResult(t *testing.T) {
	whole := linesShown(t, drivenSift(t))
	cut := linesShown(t, drivenSift(t, "--sift", "free"))
	if cut >= whole {
		t.Fatalf("--sift free showed %d lines and no arm at all showed %d: the arm never reached the app", cut, whole)
	}
}
