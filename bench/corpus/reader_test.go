package corpus

import (
	"encoding/json"
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

func TestWalkSessionsOverTheRealTreeReportsTheFiveNumbers(t *testing.T) {
	if _, err := os.Stat(sessionsDir); os.IsNotExist(err) {
		t.Skip("no .tofu/sessions on this machine")
	}
	walked, err := WalkSessions(sessionsDir)
	if err != nil {
		t.Fatalf("walking %s: %v", sessionsDir, err)
	}
	bySchema := map[Schema]int{}
	withToolCall, withWrite := 0, 0
	for _, turn := range walked.Turns {
		bySchema[turn.Schema]++
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
	t.Logf("entries %d, parsed %d (single file %d, header+jsonl %d), skipped %d, carrying a tool call %d, carrying a write %d, walk elapsed %s",
		walked.EntryCount, len(walked.Turns), bySchema[SchemaSingleFile], bySchema[SchemaHeaderJSONL], len(walked.Skipped), withToolCall, withWrite, walked.WalkElapsed)
	for _, skipped := range walked.Skipped {
		t.Logf("skipped %s: %s", skipped.Path, skipped.Reason)
	}
}
