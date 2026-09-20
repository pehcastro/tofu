package turn

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"boji/internal/konst"
	"boji/internal/recall"
)

const testBytesCap = 4096

func bigBody() string {
	var b strings.Builder
	for line := 0; b.Len() < 20000; line++ {
		fmt.Fprintf(&b, "line %04d: the quick brown fox jumps over the lazy dog\n", line)
	}
	return b.String()
}

func TestAResultUnderTheCapIsPassedThroughUntouched(t *testing.T) {
	artifacts, err := NewArtifacts(t.TempDir(), true)
	if err != nil {
		t.Fatalf("building the artifact store: %v", err)
	}
	rendered, handle, err := artifacts.Render("short result", testBytesCap)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if rendered != "short result" || handle != "" {
		t.Fatalf("rendered %q handle %q, wanted the body back with no handle", rendered, handle)
	}
}

func TestTheOffArmCutsTheMiddleAndHandsBackNoHandle(t *testing.T) {
	artifacts, err := NewArtifacts(t.TempDir(), false)
	if err != nil {
		t.Fatalf("building the artifact store: %v", err)
	}
	body := bigBody()
	rendered, handle, err := artifacts.Render(body, testBytesCap)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if handle != "" {
		t.Fatalf("the off arm produced a handle %q", handle)
	}
	if !strings.Contains(rendered, truncationMarker) {
		t.Fatal("the off arm did not cut the middle")
	}
	if len(rendered) != testBytesCap+len(truncationMarker) {
		t.Fatalf("the off arm rendered %d bytes, wanted %d", len(rendered), testBytesCap+len(truncationMarker))
	}
}

func TestTheHandleArmKeepsTheResultOnDiskByteForByte(t *testing.T) {
	dir := t.TempDir()
	artifacts, err := NewArtifacts(dir, true)
	if err != nil {
		t.Fatalf("building the artifact store: %v", err)
	}
	body := bigBody()
	rendered, handle, err := artifacts.Render(body, testBytesCap)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if handle == "" {
		t.Fatal("a result over the cap got no handle")
	}
	if !strings.Contains(rendered, handle) {
		t.Fatalf("the rendered result does not name its own handle %s: %q", handle, rendered)
	}
	if len(rendered) >= len(body) {
		t.Fatalf("the rendered result is %d bytes against a body of %d", len(rendered), len(body))
	}

	stored, err := recall.NewStore(dir).Fetch(handle)
	if err != nil {
		t.Fatalf("a store opened fresh after the turn cannot read handle %s: %v", handle, err)
	}
	if string(stored) != body {
		t.Fatalf("the stored result differs: %d bytes on disk against %d rendered from", len(stored), len(body))
	}
}

func TestFetchReturnsTheExactRangeAskedFor(t *testing.T) {
	dir := t.TempDir()
	artifacts, err := NewArtifacts(dir, true)
	if err != nil {
		t.Fatalf("building the artifact store: %v", err)
	}
	body := bigBody()
	_, handle, err := artifacts.Render(body, testBytesCap)
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	result, err := artifacts.FetchTool().Run(context.Background(),
		json.RawMessage(fmt.Sprintf(`{"handle":%q,"offset":5000,"length":200}`, handle)))
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if result.Content != body[5000:5200] {
		t.Fatalf("fetch returned %q, wanted %q", result.Content, body[5000:5200])
	}
	if !strings.Contains(result.Command, handle) {
		t.Fatalf("the command line does not name the handle: %q", result.Command)
	}
}

func TestFetchRefusesARangePastTheEndInsteadOfReturningNothing(t *testing.T) {
	dir := t.TempDir()
	artifacts, err := NewArtifacts(dir, true)
	if err != nil {
		t.Fatalf("building the artifact store: %v", err)
	}
	body := bigBody()
	_, handle, err := artifacts.Render(body, testBytesCap)
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	beyond := len(body) + 1
	result, err := artifacts.FetchTool().Run(context.Background(),
		json.RawMessage(fmt.Sprintf(`{"handle":%q,"offset":%d,"length":10}`, handle, beyond)))
	if err == nil {
		t.Fatalf("a fetch past the end returned %q instead of an error", result.Content)
	}
	if !strings.Contains(err.Error(), fmt.Sprint(len(body))) {
		t.Fatalf("the refusal does not say how big the artifact is: %v", err)
	}
}

func TestFetchClampsALengthThatRunsPastTheEnd(t *testing.T) {
	dir := t.TempDir()
	artifacts, err := NewArtifacts(dir, true)
	if err != nil {
		t.Fatalf("building the artifact store: %v", err)
	}
	body := bigBody()
	_, handle, err := artifacts.Render(body, testBytesCap)
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	offset := len(body) - 10
	result, err := artifacts.FetchTool().Run(context.Background(),
		json.RawMessage(fmt.Sprintf(`{"handle":%q,"offset":%d,"length":9999}`, handle, offset)))
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if result.Content != body[offset:] {
		t.Fatalf("fetch returned %q, wanted the last ten bytes %q", result.Content, body[offset:])
	}
}

func TestFetchRefusesAnythingThatIsNotAHandle(t *testing.T) {
	artifacts, err := NewArtifacts(t.TempDir(), true)
	if err != nil {
		t.Fatalf("building the artifact store: %v", err)
	}
	for _, handle := range []string{"", "../../../.env", "beef", strings.Repeat("g", 32)} {
		_, err := artifacts.FetchTool().Run(context.Background(),
			json.RawMessage(fmt.Sprintf(`{"handle":%q,"offset":0,"length":10}`, handle)))
		if err == nil {
			t.Fatalf("handle %q was accepted", handle)
		}
		if !strings.Contains(err.Error(), "is not an artifact handle") {
			t.Fatalf("handle %q was refused for the wrong reason: %v", handle, err)
		}
	}
}

func TestFetchRefusesAWellFormedHandleNothingStored(t *testing.T) {
	artifacts, err := NewArtifacts(t.TempDir(), true)
	if err != nil {
		t.Fatalf("building the artifact store: %v", err)
	}
	_, err = artifacts.FetchTool().Run(context.Background(),
		json.RawMessage(`{"handle":"0123456789abcdef0123456789abcdef","offset":0,"length":10}`))
	if err == nil {
		t.Fatal("a handle nothing was stored under was accepted")
	}
}

const capBeforeBOJI174 = 8192

func TestASourceFileOfThisRepositoryIsReadWholeAtTheShippedCap(t *testing.T) {
	body, err := os.ReadFile("spawn.go")
	if err != nil {
		t.Fatal(err)
	}
	artifacts, err := NewArtifacts(t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := recall.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}

	whole, handle, err := artifacts.Render(string(body), konst.TurnResultBytesCap)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("spawn.go is %d lines and %d bytes, %d tokens by the elide estimator, against a cap of %d bytes",
		strings.Count(string(body), "\n"), len(body), preview.Tokens(string(body)), konst.TurnResultBytesCap)
	if handle != "" || whole != string(body) {
		t.Fatalf("a %d byte source file came back as %d bytes with handle %q", len(body), len(whole), handle)
	}

	elided, oldHandle, err := artifacts.Render(string(body), capBeforeBOJI174)
	if err != nil {
		t.Fatal(err)
	}
	if oldHandle == "" {
		t.Fatalf("the old %d byte cap did not elide this file, so it is the wrong file to measure with", capBeforeBOJI174)
	}
	unseen := len(body) - preview.HeadBytes - preview.TailBytes
	t.Logf("at the old %d byte cap the model saw %d bytes and had to fetch %d more, which is %d artifact_fetch round trips",
		capBeforeBOJI174, len(elided), unseen, (unseen+capBeforeBOJI174-1)/capBeforeBOJI174)
	t.Logf("reading it whole costs %d tokens more than the elided render, and removes those round trips",
		preview.Tokens(string(body))-preview.Tokens(elided))
}

func TestBothArmsRenderTheSameResultAndTheSizesAreTheFinding(t *testing.T) {
	body := bigBody()
	off, err := NewArtifacts(t.TempDir(), false)
	if err != nil {
		t.Fatalf("building the off arm: %v", err)
	}
	on, err := NewArtifacts(t.TempDir(), true)
	if err != nil {
		t.Fatalf("building the handle arm: %v", err)
	}
	truncated, _, err := off.Render(body, testBytesCap)
	if err != nil {
		t.Fatalf("off arm render: %v", err)
	}
	withHandle, handle, err := on.Render(body, testBytesCap)
	if err != nil {
		t.Fatalf("handle arm render: %v", err)
	}
	t.Logf("body %d bytes, off arm %d bytes, handle arm %d bytes, handle %s",
		len(body), len(truncated), len(withHandle), handle)
	if len(withHandle) >= len(truncated) {
		t.Fatalf("the handle arm rendered %d bytes against the off arm's %d, so it costs more than it saves",
			len(withHandle), len(truncated))
	}
}
