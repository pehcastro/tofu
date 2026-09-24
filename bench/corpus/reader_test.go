package corpus

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sessionsDir = "../../.tofu/sessions"

func firstReadableEntry(t *testing.T, wantDir bool) (string, bool) {
	t.Helper()
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		t.Skipf("no %s on this machine: %v", sessionsDir, err)
	}
	for _, entry := range entries {
		if entry.IsDir() != wantDir || (!wantDir && filepath.Ext(entry.Name()) != ".json") {
			continue
		}
		full := filepath.Join(sessionsDir, entry.Name())
		if wantDir {
			if _, err := ReadTurnDir(full); err == nil {
				return entry.Name(), true
			}
			continue
		}
		if _, err := ReadTurn(full); err == nil {
			return entry.Name(), true
		}
	}
	return "", false
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		copyFile(t, filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name()))
	}
}

func assertParsedTurn(t *testing.T, name string, recorded RecordedTurn, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	if recorded.ID == "" || len(recorded.Steps) == 0 || recorded.WallClockMS == 0 {
		t.Fatalf("%s parsed to an empty turn: %+v", name, recorded)
	}
}

func TestTheReaderParsesBothSchemas(t *testing.T) {
	t.Run("single file", func(t *testing.T) {
		name, ok := firstReadableEntry(t, false)
		if !ok {
			t.Skip("no single file turn on this machine")
		}
		recorded, err := ReadTurn(filepath.Join(sessionsDir, name))
		assertParsedTurn(t, name, recorded, err)
	})
	t.Run("header and jsonl", func(t *testing.T) {
		name, ok := firstReadableEntry(t, true)
		if !ok {
			t.Skip("no header and jsonl turn on this machine")
		}
		recorded, err := ReadTurnDir(filepath.Join(sessionsDir, name))
		assertParsedTurn(t, name, recorded, err)
	})
}

const plantedHomePath = "F:/localhost/ephem-sh/bob owned by pehcastro@gmail.com"

func serializedLeaks(t *testing.T, recorded RecordedTurn) []string {
	t.Helper()
	raw, err := json.Marshal(recorded)
	if err != nil {
		t.Fatal(err)
	}
	return LeaksIn(string(raw))
}

func TestAPlantedHomePathDoesNotSurviveTheRead(t *testing.T) {
	name, ok := firstReadableEntry(t, false)
	if !ok {
		t.Skip("no single file turn on this machine")
	}
	scratch := t.TempDir()
	original, err := os.ReadFile(filepath.Join(sessionsDir, name))
	if err != nil {
		t.Fatal(err)
	}
	fields := map[string]any{}
	if err := json.Unmarshal(original, &fields); err != nil {
		t.Fatal(err)
	}
	taskKey := "Task"
	if _, ok := fields["task"]; ok {
		taskKey = "task"
	}
	fields[taskKey] = plantedHomePath
	planted, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	plantedPath := filepath.Join(scratch, name)
	if err := os.WriteFile(plantedPath, planted, 0o644); err != nil {
		t.Fatal(err)
	}
	recorded, err := ReadTurn(plantedPath)
	if err != nil {
		t.Fatalf("reading the planted copy: %v", err)
	}
	if leaks := serializedLeaks(t, recorded); len(leaks) > 0 {
		t.Fatalf("a planted home path survived the read: %q", leaks)
	}
}

func TestAPlantedHomePathDoesNotSurviveTheDirRead(t *testing.T) {
	name, ok := firstReadableEntry(t, true)
	if !ok {
		t.Skip("no header and jsonl turn on this machine")
	}
	scratch := t.TempDir()
	dst := filepath.Join(scratch, name)
	copyDir(t, filepath.Join(sessionsDir, name), dst)
	bodyPath := filepath.Join(dst, "body.jsonl")
	body, err := os.ReadFile(bodyPath)
	if err != nil {
		t.Fatal(err)
	}
	planted := strings.Replace(string(body), `"task":"`, `"task":"`+plantedHomePath+" ", 1)
	if planted == string(body) {
		t.Fatal("the outcome line carries no \"task\" key to plant into, so this test proves nothing")
	}
	if err := os.WriteFile(bodyPath, []byte(planted), 0o644); err != nil {
		t.Fatal(err)
	}
	recorded, err := ReadTurnDir(dst)
	if err != nil {
		t.Fatalf("reading the planted copy: %v", err)
	}
	if leaks := serializedLeaks(t, recorded); len(leaks) > 0 {
		t.Fatalf("a planted home path survived the read: %q", leaks)
	}
}

func hasStepWithText(steps []RecordedStep) bool {
	for _, step := range steps {
		if strings.TrimSpace(step.AssistantText) != "" {
			return true
		}
	}
	return false
}

func TestTheReaderCarriesReplyTextOnBothSchemas(t *testing.T) {
	t.Run("single file", func(t *testing.T) {
		entries, err := os.ReadDir(sessionsDir)
		if err != nil {
			t.Skipf("no %s on this machine: %v", sessionsDir, err)
		}
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
				continue
			}
			recorded, err := ReadTurn(filepath.Join(sessionsDir, entry.Name()))
			if err != nil {
				continue
			}
			if hasStepWithText(recorded.Steps) {
				return
			}
		}
		t.Skip("no single file turn on this machine carries reply text")
	})
	t.Run("header and jsonl", func(t *testing.T) {
		entries, err := os.ReadDir(sessionsDir)
		if err != nil {
			t.Skipf("no %s on this machine: %v", sessionsDir, err)
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			recorded, err := ReadTurnDir(filepath.Join(sessionsDir, entry.Name()))
			if err != nil {
				continue
			}
			if hasStepWithText(recorded.Steps) {
				return
			}
		}
		t.Skip("no header and jsonl turn on this machine carries reply text")
	})
}

func TestAPlantedHomePathInReplyTextDoesNotSurviveTheRead(t *testing.T) {
	name, ok := firstReadableEntry(t, false)
	if !ok {
		t.Skip("no single file turn on this machine")
	}
	scratch := t.TempDir()
	original, err := os.ReadFile(filepath.Join(sessionsDir, name))
	if err != nil {
		t.Fatal(err)
	}
	fields := map[string]any{}
	if err := json.Unmarshal(original, &fields); err != nil {
		t.Fatal(err)
	}
	stepsKey := "Steps"
	if _, ok := fields["steps"]; ok {
		stepsKey = "steps"
	}
	steps, ok := fields[stepsKey].([]any)
	if !ok || len(steps) == 0 {
		t.Skip("this turn carries no step to plant into")
	}
	step, ok := steps[0].(map[string]any)
	if !ok {
		t.Skip("this turn's step is not the expected shape")
	}
	step["AssistantText"] = plantedHomePath
	step["assistant_text"] = plantedHomePath
	steps[0] = step
	fields[stepsKey] = steps
	planted, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	plantedPath := filepath.Join(scratch, name)
	if err := os.WriteFile(plantedPath, planted, 0o644); err != nil {
		t.Fatal(err)
	}
	recorded, err := ReadTurn(plantedPath)
	if err != nil {
		t.Fatalf("reading the planted copy: %v", err)
	}
	if leaks := serializedLeaks(t, recorded); len(leaks) > 0 {
		t.Fatalf("a planted home path in reply text survived the read: %q", leaks)
	}
}

func TestTheReaderCarriesTheTypedOutcomeOnBothSchemas(t *testing.T) {
	t.Run("single file", func(t *testing.T) {
		entries, err := os.ReadDir(sessionsDir)
		if err != nil {
			t.Skipf("no %s on this machine: %v", sessionsDir, err)
		}
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
				continue
			}
			recorded, err := ReadTurn(filepath.Join(sessionsDir, entry.Name()))
			if err != nil || recorded.Outcome == "" {
				continue
			}
			return
		}
		t.Skip("no single file turn on this machine carries an outcome")
	})
	t.Run("header and jsonl", func(t *testing.T) {
		entries, err := os.ReadDir(sessionsDir)
		if err != nil {
			t.Skipf("no %s on this machine: %v", sessionsDir, err)
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			recorded, err := ReadTurnDir(filepath.Join(sessionsDir, entry.Name()))
			if err != nil || recorded.Outcome == "" {
				continue
			}
			return
		}
		t.Skip("no header and jsonl turn on this machine carries an outcome")
	})
}

func TestAPlantedHomePathInOutcomeDoesNotSurviveTheRead(t *testing.T) {
	name, ok := firstReadableEntry(t, false)
	if !ok {
		t.Skip("no single file turn on this machine")
	}
	scratch := t.TempDir()
	original, err := os.ReadFile(filepath.Join(sessionsDir, name))
	if err != nil {
		t.Fatal(err)
	}
	fields := map[string]any{}
	if err := json.Unmarshal(original, &fields); err != nil {
		t.Fatal(err)
	}
	outcomeKey := "Outcome"
	if _, ok := fields["outcome"]; ok {
		outcomeKey = "outcome"
	}
	fields[outcomeKey] = plantedHomePath
	planted, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	plantedPath := filepath.Join(scratch, name)
	if err := os.WriteFile(plantedPath, planted, 0o644); err != nil {
		t.Fatal(err)
	}
	recorded, err := ReadTurn(plantedPath)
	if err != nil {
		t.Fatalf("reading the planted copy: %v", err)
	}
	if leaks := serializedLeaks(t, recorded); len(leaks) > 0 {
		t.Fatalf("a planted home path in outcome survived the read: %q", leaks)
	}
}

func firstDirWith(t *testing.T, has func(RecordedTurn) bool) (string, bool) {
	t.Helper()
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		t.Skipf("no %s on this machine: %v", sessionsDir, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		recorded, err := ReadTurnDir(filepath.Join(sessionsDir, entry.Name()))
		if err != nil || !has(recorded) {
			continue
		}
		return entry.Name(), true
	}
	return "", false
}

func TestAPlantedHomePathInAutoCompactionDoesNotSurviveTheDirRead(t *testing.T) {
	name, ok := firstDirWith(t, func(r RecordedTurn) bool { return r.AutoCompaction != "" })
	if !ok {
		t.Skip("no header and jsonl turn on this machine carries auto_compaction")
	}
	scratch := t.TempDir()
	dst := filepath.Join(scratch, name)
	copyDir(t, filepath.Join(sessionsDir, name), dst)
	headerPath := filepath.Join(dst, "header.json")
	header, err := os.ReadFile(headerPath)
	if err != nil {
		t.Fatal(err)
	}
	fields := map[string]any{}
	if err := json.Unmarshal(header, &fields); err != nil {
		t.Fatal(err)
	}
	fields["auto_compaction"] = fields["auto_compaction"].(string) + " " + plantedHomePath
	planted, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(headerPath, planted, 0o644); err != nil {
		t.Fatal(err)
	}
	recorded, err := ReadTurnDir(dst)
	if err != nil {
		t.Fatalf("reading the planted copy: %v", err)
	}
	if leaks := serializedLeaks(t, recorded); len(leaks) > 0 {
		t.Fatalf("a planted home path in auto_compaction survived the read: %q", leaks)
	}
}

func TestAPlantedHomePathInBudgetSourceDoesNotSurviveTheDirRead(t *testing.T) {
	name, ok := firstDirWith(t, func(r RecordedTurn) bool { return r.Budget.Source != "" })
	if !ok {
		t.Skip("no header and jsonl turn on this machine carries a budget source")
	}
	scratch := t.TempDir()
	dst := filepath.Join(scratch, name)
	copyDir(t, filepath.Join(sessionsDir, name), dst)
	bodyPath := filepath.Join(dst, "body.jsonl")
	body, err := os.ReadFile(bodyPath)
	if err != nil {
		t.Fatal(err)
	}
	planted := strings.Replace(string(body), `"Source":"`, `"Source":"`+plantedHomePath+" ", 1)
	if planted == string(body) {
		t.Fatal("the outcome line carries no budget \"Source\" key to plant into, so this test proves nothing")
	}
	if err := os.WriteFile(bodyPath, []byte(planted), 0o644); err != nil {
		t.Fatal(err)
	}
	recorded, err := ReadTurnDir(dst)
	if err != nil {
		t.Fatalf("reading the planted copy: %v", err)
	}
	if leaks := serializedLeaks(t, recorded); len(leaks) > 0 {
		t.Fatalf("a planted home path in the budget source survived the read: %q", leaks)
	}
}

func firstCallOffDisk(t *testing.T, wanted func(RecordedCall) bool) (where string, found RecordedCall, ok bool) {
	t.Helper()
	if _, err := os.Stat(sessionsDir); err != nil {
		t.Skipf("no %s on this machine: %v", sessionsDir, err)
	}
	walked, err := WalkSessions(sessionsDir)
	if err != nil {
		t.Fatalf("walking %s: %v", sessionsDir, err)
	}
	for _, turn := range walked.Turns {
		for _, step := range turn.Steps {
			for _, call := range step.ToolCalls {
				if wanted(call) {
					return fmt.Sprintf("%s step %d %s", turn.ID, step.Index, call.Tool), call, true
				}
			}
		}
	}
	return "", RecordedCall{}, false
}

func TestARecordedRowCarriesRenderedBytesAndTheArtifactHandle(t *testing.T) {
	where, call, ok := firstCallOffDisk(t, func(call RecordedCall) bool {
		return call.ResultHandle != "" && call.RenderedBytes != 0 && call.RenderedBytes != call.ResultBytes
	})
	if !ok {
		t.Fatal("no row off disk carried a rendered byte count that differs from its result byte count together with an artifact handle, so either the reader drops them or the corpus never recorded one")
	}
	t.Logf("%s: result_bytes %d, rendered_bytes %d, result_handle %s", where, call.ResultBytes, call.RenderedBytes, call.ResultHandle)
}

func TestARecordedRowCarriesTheResultHashARerunIsComparedAgainst(t *testing.T) {
	where, call, ok := firstCallOffDisk(t, func(call RecordedCall) bool { return call.ResultHash != "" })
	if !ok {
		t.Fatal("no row off disk carried a result hash, so either the reader drops it or the corpus never recorded one")
	}
	t.Logf("%s: result_bytes %d, result_hash %s", where, call.ResultBytes, call.ResultHash)
}

func TestATurnWithStepsAndNoOutcomeLineIsReadAndSaysItsWallClockIsMissing(t *testing.T) {
	name, ok := firstReadableEntry(t, true)
	if !ok {
		t.Skip("no header and jsonl turn on this machine")
	}
	scratch := t.TempDir()
	dst := filepath.Join(scratch, name)
	copyDir(t, filepath.Join(sessionsDir, name), dst)
	if err := os.Remove(filepath.Join(dst, "header.json")); err != nil {
		t.Fatal(err)
	}
	bodyPath := filepath.Join(dst, "body.jsonl")
	body, err := os.ReadFile(bodyPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	kept := []string{}
	for _, line := range lines {
		if !strings.Contains(line, `"kind":"outcome"`) {
			kept = append(kept, line)
		}
	}
	if len(kept) == len(lines) {
		t.Fatal("the copy carries no outcome line to remove, so this test proves nothing")
	}
	if err := os.WriteFile(bodyPath, []byte(strings.Join(kept, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	recorded, err := ReadTurnDir(dst)
	if !errors.Is(err, ErrNoWallClock) {
		t.Fatalf("reading a turn with steps and no outcome line returned %v, want an error the caller can match against ErrNoWallClock", err)
	}
	if len(recorded.Steps) == 0 {
		t.Fatal("the turn was refused rather than read: it carries steps and they did not come back")
	}
	if recorded.WallClockMS != 0 {
		t.Fatalf("the reader invented a wall clock of %d ms", recorded.WallClockMS)
	}
	walked, err := WalkSessions(scratch)
	if err != nil {
		t.Fatal(err)
	}
	if len(walked.Turns) != 1 {
		t.Fatalf("the walk read %d turns, want the one planted copy", len(walked.Turns))
	}
	if walked.Turns[0].WallClockRecorded {
		t.Fatal("the walk reports a recorded wall clock on a turn whose outcome line was removed")
	}
	t.Logf("%s read with %d steps and no wall clock, skipped %d", name, len(recorded.Steps), len(walked.Skipped))
}

const olderShapedTurnPath = "../stopcheck/corpus/turn-18d68bcceb3d56e8.json"

func TestAnOlderShapedSessionKeepsItsToolCalls(t *testing.T) {
	data, err := os.ReadFile(olderShapedTurnPath)
	if err != nil {
		t.Fatalf("reading %s: %v", olderShapedTurnPath, err)
	}
	var recorded RecordedTurn
	if err := json.Unmarshal(data, &recorded); err != nil {
		t.Fatalf("decoding %s: %v", olderShapedTurnPath, err)
	}
	if len(recorded.Steps) == 0 {
		t.Fatalf("%s parsed to no steps", olderShapedTurnPath)
	}
	first := recorded.Steps[0]
	if len(first.ToolCalls) != 1 {
		t.Fatalf("%s step 0 carries %d tool calls, want 1", olderShapedTurnPath, len(first.ToolCalls))
	}
	call := first.ToolCalls[0]
	if call.Tool != "write" {
		t.Fatalf("%s step 0's call is tool %q, want write", olderShapedTurnPath, call.Tool)
	}
	if call.ResultBytes != 26 || call.RenderedBytes != 26 {
		t.Fatalf("%s step 0's call carries result_bytes %d, rendered_bytes %d, want 26 and 26", olderShapedTurnPath, call.ResultBytes, call.RenderedBytes)
	}
	if call.ResultHash == "" {
		t.Fatalf("%s step 0's call carries no result hash", olderShapedTurnPath)
	}
}

func TestWalkSessionsOverTheRealTreeReportsTheFiveNumbers(t *testing.T) {
	if _, err := os.Stat(sessionsDir); os.IsNotExist(err) {
		t.Skip("no .tofu/sessions on this machine")
	}
	walked, err := WalkSessions(sessionsDir)
	if err != nil {
		t.Fatalf("walking %s: %v", sessionsDir, err)
	}
	bySchema := map[Schema]int{}
	withToolCall, withWrite, noWallClock := 0, 0, 0
	for _, turn := range walked.Turns {
		bySchema[turn.Schema]++
		if !turn.WallClockRecorded {
			noWallClock++
		}
		hasCall, hasWrite := false, false
		for _, step := range turn.Steps {
			for _, call := range step.ToolCalls {
				hasCall = true
				if call.Tool == "write" || call.Tool == "edit" {
					hasWrite = true
				}
			}
		}
		if hasCall {
			withToolCall++
		}
		if hasWrite {
			withWrite++
		}
	}
	t.Logf("entries %d, parsed %d (single file %d, header+jsonl %d), skipped %d, no wall clock %d, carrying a tool call %d, carrying a write %d, walk elapsed %s",
		walked.EntryCount, len(walked.Turns), bySchema[SchemaSingleFile], bySchema[SchemaHeaderJSONL], len(walked.Skipped), noWallClock, withToolCall, withWrite, walked.WalkElapsed)
	for _, skipped := range walked.Skipped {
		t.Logf("skipped %s: %s", skipped.Path, skipped.Reason)
	}
}
