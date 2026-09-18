package ledger

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCanonicalSortsKeysAndDropsWhitespace(t *testing.T) {
	value := map[string]any{"b": 1, "a": 2, "c": map[string]any{"z": true, "y": nil}}
	got, err := Canonical(value)
	if err != nil {
		t.Fatalf("Canonical: %v", err)
	}
	want := `{"a":2,"b":1,"c":{"y":null,"z":true}}`
	if string(got) != want {
		t.Fatalf("canonical json\n got %s\nwant %s", got, want)
	}
}

func TestCanonicalNumbersHaveOneSpelling(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{`{"p":1.50}`, `{"p":1.5}`},
		{`{"p":1.0}`, `{"p":1}`},
		{`{"p":0.10000000000000001}`, `{"p":0.1}`},
		{`{"p":1e2}`, `{"p":100}`},
		{`{"p":9007199254740993}`, `{"p":9007199254740993}`},
	}
	for _, one := range cases {
		var tree any
		decoder := json.NewDecoder(strings.NewReader(one.raw))
		decoder.UseNumber()
		if err := decoder.Decode(&tree); err != nil {
			t.Fatalf("decode %s: %v", one.raw, err)
		}
		got, err := Canonical(tree)
		if err != nil {
			t.Fatalf("Canonical %s: %v", one.raw, err)
		}
		if string(got) != one.want {
			t.Errorf("Canonical(%s) = %s, want %s", one.raw, got, one.want)
		}
	}
}

func TestKeyIsStableAcrossMapInsertionOrderOverAHundredRounds(t *testing.T) {
	forward := []string{"branch", "command", "cwd", "diff_lines", "files", "tool", "user_asked"}
	backward := make([]string, len(forward))
	for i, name := range forward {
		backward[len(forward)-1-i] = name
	}
	values := map[string]any{
		"branch":     "main",
		"command":    "git push --force origin main",
		"cwd":        "/f/localhost/ephem-sh/bob",
		"diff_lines": 412,
		"files":      []any{"internal/judge/ledger/read.go", "internal/judge/ledger/write.go"},
		"tool":       map[string]any{"family": "shell", "name": "bash", "writes": true},
		"user_asked": false,
	}
	buildInOrder := func(order []string) map[string]any {
		state := make(map[string]any, len(order))
		for _, name := range order {
			state[name] = values[name]
		}
		return state
	}

	cache := NewCache(t.TempDir())
	first, err := cache.Key(Request{State: buildInOrder(forward), Questions: "tool_gate", Model: "jev-latest", Version: 4})
	if err != nil {
		t.Fatalf("Key: %v", err)
	}
	const rounds = 100
	for round := 0; round < rounds; round++ {
		fromForward, err := cache.Key(Request{State: buildInOrder(forward), Questions: "tool_gate", Model: "jev-latest", Version: 4})
		if err != nil {
			t.Fatalf("round %d forward: %v", round, err)
		}
		fromBackward, err := cache.Key(Request{State: buildInOrder(backward), Questions: "tool_gate", Model: "jev-latest", Version: 4})
		if err != nil {
			t.Fatalf("round %d backward: %v", round, err)
		}
		if fromForward != first || fromBackward != first {
			t.Fatalf("round %d: keys diverged\nfirst    %s\nforward  %s\nbackward %s", round, first, fromForward, fromBackward)
		}
	}
	t.Logf("%d rounds, both insertion orders, one key: %s", rounds, first)
}

func TestKeyChangesWithTheQuestionsVersion(t *testing.T) {
	cache := NewCache(t.TempDir())
	state := map[string]any{"tool": "bash"}
	four, err := cache.Key(Request{State: state, Questions: "tool_gate", Model: "jev-latest", Version: 4})
	if err != nil {
		t.Fatalf("Key: %v", err)
	}
	five, err := cache.Key(Request{State: state, Questions: "tool_gate", Model: "jev-latest", Version: 5})
	if err != nil {
		t.Fatalf("Key: %v", err)
	}
	if four == five {
		t.Fatalf("a wording change must miss the cache, both keys were %s", four)
	}
	if !strings.HasPrefix(four, "v4-") || !strings.HasPrefix(five, "v5-") {
		t.Fatalf("the version belongs in front of the hash, got %s and %s", four, five)
	}
}
