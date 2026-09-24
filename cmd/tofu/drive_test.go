package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"tofu/interface/tui/progress"
	"tofu/internal/konst"
	"tofu/internal/llm"
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
	dir := drivenProject(t)
	deck := written(t, dir, "read.cassette", readingCassette)
	script := written(t, dir, "read.drive", strings.Join([]string{
		"wait type a task and press enter",
		"type read note.txt",
		"key enter",
		"wait cooked for",
		"screen",
	}, "\n"))
	var out, errOut bytes.Buffer
	code := driveVerb([]string{script, "--cassette", deck, "--plain", "--timeout", "30s"}, strings.NewReader(""), &out, &errOut)
	if code != exitOK {
		t.Fatalf("tofu drive exited %d: %s", code, errOut.String())
	}
	for _, want := range []string{"read note.txt", "note.txt holds one line", "cooked for"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the printed screen never says %q:\n%s", want, out.String())
		}
	}
}

const sleepingCassette = `{"text":"waiting on the shell","tools":[{"name":"bash","args":{"command":"sleep 6"}}]}
{"text":"THE ANSWER AFTER THE CANCEL"}
`

func TestTwoInterruptsEndADrivenTurnAndTheCassetteIsNeverAskedAgain(t *testing.T) {
	dir := drivenProject(t)
	deck := written(t, dir, "slow.cassette", sleepingCassette)
	script := written(t, dir, "stop.drive", strings.Join([]string{
		"wait type a task and press enter",
		"type wait for me",
		"key enter",
		"wait working   bash",
		"key ctrl+c",
		"key ctrl+c",
		"wait cooked for",
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
		"wait type a task and press enter",
		"type why does the loop read the policy first?",
		"key enter",
		"wait because a locked",
		"key ctrl+c",
		"wait characters were written and kept in work",
		"key alt+2",
		"screen",
	}, "\n"))
	var out, errOut bytes.Buffer
	code := driveVerb([]string{script, "--cassette", deck, "--plain", "--timeout", "30s"}, strings.NewReader(""), &out, &errOut)
	if code != exitOK {
		t.Fatalf("tofu drive exited %d: %s", code, errOut.String())
	}
	for _, want := range []string{"answer, interrupted", "because a locked"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("work never says %q, so the answer arrived as one sealed event:\n%s", want, out.String())
		}
	}
}

func TestAWaitForAStateThatNeverArrivesFailsSayingWhatItWaitedFor(t *testing.T) {
	dir := drivenProject(t)
	deck := written(t, dir, "read.cassette", readingCassette)
	script := written(t, dir, "never.drive", strings.Join([]string{
		"wait type a task and press enter",
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
		"wait type a task and press enter",
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
		"wait type a task and press enter",
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
	code := driveVerb([]string{"-", "--plain", "--timeout", "30s"}, strings.NewReader("wait type a task and press enter\nscreen\n"), &out, &errOut)
	if code != exitOK {
		t.Fatalf("tofu drive exited %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "type a task and press enter") {
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
	script := written(t, dir, "look.drive", "wait type a task and press enter\n")
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

func TestDriveHelpPrintsItsOwnUsage(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := driveVerb([]string{"--help"}, strings.NewReader(""), &out, &errOut); code != exitOK {
		t.Fatal("tofu drive --help did not exit 0")
	}
	for _, want := range []string{"usage: tofu drive", "wait TEXT", "environment", cassetteVariable} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the usage never says %q:\n%s", want, out.String())
		}
	}
}
