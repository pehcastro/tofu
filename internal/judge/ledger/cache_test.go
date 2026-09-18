package ledger

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type askerThatFailsTheTestWhenCalled struct {
	t     *testing.T
	calls int
}

func (a *askerThatFailsTheTestWhenCalled) Ask(ctx context.Context, req Request) (Entry, error) {
	a.calls++
	a.t.Errorf("a cache hit reached the network: Ask called with questions %q and model %q", req.Questions, req.Model)
	return Entry{}, errors.New("this call must never happen")
}

type askerRecordingOneCall struct {
	entry Entry
	calls int
}

func (a *askerRecordingOneCall) Ask(ctx context.Context, req Request) (Entry, error) {
	a.calls++
	return a.entry, nil
}

func recordedAnswers() []Answer {
	return []Answer{
		{
			Question: "risk",
			Wording:  4,
			Kind:     AnswerChoice,
			Choice:   "force-push to a shared branch",
			Dist: []Slice{
				{Option: "harmless", P: 0.01},
				{Option: "reversible", P: 0.04},
				{Option: "costly", P: 0.11},
				{Option: "force-push to a shared branch", P: 0.84},
			},
		},
		{
			Question: "user_requested",
			Wording:  4,
			Kind:     AnswerChoice,
			Choice:   "no",
			Dist: []Slice{
				{Option: "no", P: 0.93},
				{Option: "yes", P: 0.07},
			},
		},
	}
}

func recordedEntry() Entry {
	return Entry{RowID: "2026-09-18-abc123", Build: "typesafe/jev-1.13-20260917", RequestID: "gen-dec-1", Answers: recordedAnswers()}
}

func TestACacheHitReturnsTheSameEntryAndNeverAsks(t *testing.T) {
	dir := t.TempDir()
	cache := NewCache(dir)
	req := Request{
		State:     map[string]any{"command": "git push --force", "branch": "main", "user_asked": false},
		Questions: "tool_gate",
		Model:     "~typesafe/jev-latest",
		Version:   4,
	}

	first := &askerRecordingOneCall{entry: recordedEntry()}
	fresh, hit, err := cache.Resolve(context.Background(), req, first)
	if err != nil {
		t.Fatalf("first Resolve: %v", err)
	}
	if hit {
		t.Fatal("an empty cache cannot hit")
	}
	if first.calls != 1 {
		t.Fatalf("the first request must reach the asker once, it made %d calls", first.calls)
	}

	refuser := &askerThatFailsTheTestWhenCalled{t: t}
	cached, hit, err := cache.Resolve(context.Background(), req, refuser)
	if err != nil {
		t.Fatalf("second Resolve: %v", err)
	}
	if !hit {
		t.Fatal("the same request must hit the cache")
	}
	if refuser.calls != 0 {
		t.Fatalf("a hit made %d calls, it must make none", refuser.calls)
	}

	freshBytes, err := Canonical(fresh)
	if err != nil {
		t.Fatalf("Canonical: %v", err)
	}
	cachedBytes, err := Canonical(cached)
	if err != nil {
		t.Fatalf("Canonical: %v", err)
	}
	if string(freshBytes) != string(cachedBytes) {
		t.Fatalf("the cache returned a different entry\nfresh  %s\ncached %s", freshBytes, cachedBytes)
	}
	if cached.RowID != recordedEntry().RowID || cached.Build != recordedEntry().Build || cached.RequestID != recordedEntry().RequestID {
		t.Fatalf("the cached entry lost the row id, build or request id: %+v", cached)
	}
	t.Logf("hit with no call, row %s: %s", cached.RowID, cachedBytes)
}

func TestACacheMissOnADifferentStateReachesTheAsker(t *testing.T) {
	cache := NewCache(t.TempDir())
	base := Request{State: map[string]any{"command": "git status"}, Questions: "tool_gate", Model: "jev-latest", Version: 4}
	first := &askerRecordingOneCall{entry: recordedEntry()}
	if _, _, err := cache.Resolve(context.Background(), base, first); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	changed := base
	changed.State = map[string]any{"command": "git push --force"}
	if _, hit, err := cache.Resolve(context.Background(), changed, first); err != nil || hit {
		t.Fatalf("a different state must miss, hit=%v err=%v", hit, err)
	}
	if first.calls != 2 {
		t.Fatalf("two different states make two calls, got %d", first.calls)
	}
}

func TestLoadOfAnUnknownKeyIsAMissAndNotAnError(t *testing.T) {
	cache := NewCache(t.TempDir())
	entry, hit, err := cache.Load("v4-0000")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if hit || entry.Answers != nil {
		t.Fatalf("an unknown key is a miss, got hit=%v entry=%v", hit, entry)
	}
}

func TestACacheEntryIsCanonicalOnDisk(t *testing.T) {
	dir := t.TempDir()
	cache := NewCache(dir)
	req := Request{State: map[string]any{"b": 1, "a": 2}, Questions: "tool_gate", Model: "jev-latest", Version: 4}
	key, err := cache.Key(req)
	if err != nil {
		t.Fatalf("Key: %v", err)
	}
	if err := cache.Store(key, req, recordedEntry()); err != nil {
		t.Fatalf("Store: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, key+".json"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	want := `{"answers":`
	if len(raw) < len(want) || string(raw[:len(want)]) != want {
		t.Fatalf("a cache entry starts with its sorted first key, got %s", raw)
	}
}
