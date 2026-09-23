package corpus

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func unaccountedKeys(obj map[string]json.RawMessage, level string) []string {
	known, ignored := knownKeys(level), ignoredKeys(level)
	var unaccounted []string
	for key := range obj {
		lower := strings.ToLower(key)
		if !known[lower] && !ignored[lower] {
			unaccounted = append(unaccounted, level+"."+key)
		}
	}
	return unaccounted
}

func rawObject(t *testing.T, data []byte) map[string]json.RawMessage {
	t.Helper()
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		t.Fatalf("not a JSON object: %v", err)
	}
	return obj
}

func uniqueSorted(keys []string) []string {
	seen := map[string]bool{}
	var unique []string
	for _, key := range keys {
		if !seen[key] {
			seen[key] = true
			unique = append(unique, key)
		}
	}
	sort.Strings(unique)
	return unique
}

type corpusVisitor func(schema, level string, obj map[string]json.RawMessage)

func walkNamedCalls(t *testing.T, callsRaw json.RawMessage, schema, level string, visit corpusVisitor) {
	t.Helper()
	if len(callsRaw) == 0 {
		return
	}
	var calls []json.RawMessage
	if err := json.Unmarshal(callsRaw, &calls); err != nil {
		t.Fatalf("tool_calls is not a JSON array: %v", err)
	}
	for _, callRaw := range calls {
		visit(schema, level, rawObject(t, callRaw))
	}
}

func walkSteps(t *testing.T, stepsRaw json.RawMessage, schema string, visit corpusVisitor) {
	t.Helper()
	if len(stepsRaw) == 0 {
		return
	}
	var steps []json.RawMessage
	if err := json.Unmarshal(stepsRaw, &steps); err != nil {
		t.Fatalf("steps is not a JSON array: %v", err)
	}
	for _, stepRaw := range steps {
		step := rawObject(t, stepRaw)
		visit(schema, "step", step)
		walkNamedCalls(t, step["tool_calls"], schema, "call", visit)
	}
}

func walkBody(t *testing.T, body []byte, schema string, visit corpusVisitor) {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		if line == "" {
			continue
		}
		var entry struct {
			Kind string          `json:"kind"`
			Body json.RawMessage `json:"body"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatal(err)
		}
		switch entry.Kind {
		case "outcome":
			visit(schema, "turn", rawObject(t, entry.Body))
		case "step":
			step := rawObject(t, entry.Body)
			visit(schema, "step", step)
			walkNamedCalls(t, step["tool_calls"], schema, "call", visit)
		case "message":
			message := rawObject(t, entry.Body)
			visit(schema, "message", message)
			walkNamedCalls(t, message["tool_calls"], schema, "message_call", visit)
		}
	}
}

func walkRealCorpus(t *testing.T, visit corpusVisitor) (singleFileSeen, dirsSeen int) {
	t.Helper()
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		t.Skipf("no %s on this machine: %v", sessionsDir, err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			dir := filepath.Join(sessionsDir, entry.Name())
			if header, err := os.ReadFile(filepath.Join(dir, "header.json")); err == nil {
				visit("header and jsonl", "turn", rawObject(t, header))
			}
			body, err := os.ReadFile(filepath.Join(dir, "body.jsonl"))
			if err != nil {
				continue
			}
			dirsSeen++
			walkBody(t, body, "header and jsonl", visit)
			continue
		}
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(sessionsDir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		turn := rawObject(t, data)
		if _, ok := turn["schema"]; !ok {
			continue
		}
		singleFileSeen++
		visit("single file", "turn", turn)
		walkSteps(t, turn["steps"], "single file", visit)
	}
	return singleFileSeen, dirsSeen
}

func TestEveryKeyInARealTurnHasAFieldOrAReason(t *testing.T) {
	found := map[string][]string{}
	singleFileSeen, dirsSeen := walkRealCorpus(t, func(schema, level string, obj map[string]json.RawMessage) {
		found[schema] = append(found[schema], unaccountedKeys(obj, level)...)
	})
	cases := []struct {
		schema string
		seen   int
	}{
		{"single file", singleFileSeen},
		{"header and jsonl", dirsSeen},
	}
	for _, one := range cases {
		t.Run(one.schema, func(t *testing.T) {
			if one.seen == 0 {
				t.Skip("no turn of this schema on this machine")
			}
			if keys := found[one.schema]; len(keys) > 0 {
				t.Fatalf("keys with no field and no listed reason: %v", uniqueSorted(keys))
			}
		})
	}
}

func TestTheReportCountsKeysCarriedAndKeysRead(t *testing.T) {
	carried := map[string]map[string]bool{"single file": {}, "header and jsonl": {}}
	walkRealCorpus(t, func(schema, level string, obj map[string]json.RawMessage) {
		for key := range obj {
			carried[schema][level+"."+strings.ToLower(key)] = true
		}
	})
	readCount := len(knownKeys("turn")) + len(knownKeys("step")) + len(knownKeys("call")) + len(knownKeys("message")) + len(knownKeys("message_call"))
	for _, schema := range []string{"single file", "header and jsonl"} {
		if len(carried[schema]) == 0 {
			t.Logf("%s: no example on this machine", schema)
			continue
		}
		t.Logf("%s: %d distinct keys carried across turn, step, call and message; the reader now reads %d of them", schema, len(carried[schema]), readCount)
	}
}
