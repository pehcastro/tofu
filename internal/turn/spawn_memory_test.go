package turn_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"tofu/internal/llm"
	"tofu/internal/recall"
	"tofu/internal/session"
	"tofu/internal/subagent"
	"tofu/internal/turn"
)

type recordingModel struct {
	replies []llm.Decision
	asked   []llm.Request
}

func (m *recordingModel) Ask(_ context.Context, request llm.Request) (llm.Decision, error) {
	m.asked = append(m.asked, request)
	if len(m.replies) == 0 {
		return llm.Decision{Build: "m1", Outcome: llm.OutcomeMessage, Content: "done"}, nil
	}
	next := m.replies[0]
	m.replies = m.replies[1:]
	return next, nil
}

func TestTheMemoryViewIsOnceInEveryRequestHeldWithinAGenerationAndFreshAtEachForkAndResume(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "big.txt"), []byte(strings.Repeat("a line of the parser\n", 2000)), 0o644); err != nil {
		t.Fatal(err)
	}
	read, err := turn.NewReadTool(root)
	if err != nil {
		t.Fatal(err)
	}
	view := "remembered for you, by scope.\nuser-global:\n0+1|project: [memory#m1] Mock rule: run the linter before a commit"
	saved := "[memory#m2-0a1f] Mock rule saved mid-session"
	reading, done := calling("read", map[string]any{"path": "big.txt"}), llm.Decision{Build: "m1", Outcome: llm.OutcomeMessage, Content: "done"}
	model := &recordingModel{}
	store, id := session.NewStore(t.TempDir()), session.NewEventID()
	config := turn.Config{
		Model:          model,
		Spend:          turn.SpendAPIKey,
		Tools:          turn.NewRegistry(read),
		System:         "you are tofu",
		Memory:         view,
		Prefix:         &turn.Prefix{},
		Caps:           turn.Caps{MaxSteps: 4},
		ResultBytesCap: 1 << 16,
		Budget:         recall.Budget{}.At(12000, "a budget the second turn forks under"),
		ArtifactDir:    filepath.Join(root, "artifacts"),
		Sessions:       store,
		Session:        id,
		NewID:          func() string { return "turn-memory" },
	}
	resumed := "[memory#m3] Mock rule saved between sessions"
	forks := func(request llm.Request) int {
		return len(slices.DeleteFunc(slices.Clone(request.Messages), func(message llm.Message) bool { return !strings.HasPrefix(message.Content, "this is fork ") }))
	}
	for at, next := range []struct{ task, memory, source string }{
		{task: "read big.txt", memory: view},
		{task: "now read big.txt again", memory: view + "\n" + saved},
		{task: "read big.txt once more", memory: view + "\n" + saved + "\n" + resumed, source: "resume"},
	} {
		opened := len(model.asked)
		config.Task, config.Memory, config.SessionSource, model.replies = next.task, next.memory, next.source, []llm.Decision{reading, done}
		if _, err := turn.Run(context.Background(), config); err != nil {
			t.Fatalf("running %q: %v", next.task, err)
		}
		events, err := store.Events(id)
		if err != nil {
			t.Fatal(err)
		}
		if config.History, err = turn.ConversationFrom(events); err != nil {
			t.Fatal(err)
		}
		forkedInTheTurn := false
		for i, request := range model.asked[opened:] {
			number := opened + i + 1
			if system := request.Messages[0]; system.Role != llm.RoleSystem || system.Content != "you are tofu" {
				t.Errorf("request %d opens on %s %q, want the system message alone and unchanged", number, system.Role, system.Content)
			}
			views := slices.DeleteFunc(slices.Clone(request.Messages), func(message llm.Message) bool { return message.Origin.Source != llm.SourceMemoryView })
			if len(views) != 1 {
				t.Fatalf("request %d holds %d memory views, want one", number, len(views))
			}
			forked := forks(request) > forks(model.asked[opened])
			forkedInTheTurn = forkedInTheTurn || forked
			want := next.memory
			if at == 1 && !forked {
				want = view
			}
			if views[0].Content != want {
				t.Errorf("request %d of turn %d, forked %v, carries the memory view\n%s\nwant\n%s", number, at+1, forked, views[0].Content, want)
			}
		}
		if at == 1 && !forkedInTheTurn {
			t.Fatalf("the second turn never forked in %d requests, so no fork refreshed the view", len(model.asked)-opened)
		}
	}
}

func TestASubAgentOpensOnEveryMemoryEntryOnceAndItsBriefCarriesOnlyTheCitedLines(t *testing.T) {
	lines := map[string]string{
		"m1":      "[memory#m1] Replies stay short and plain",
		"m2":      "[memory#m2] The commit hook refuses attribution",
		"m3-0a1f": "[memory#m3-0a1f] Tickets come before code here",
	}
	unreadable := errors.New("entries.jsonl line 3: unexpected end of JSON input")
	for _, row := range []struct {
		name, brief string
		memory      func() (map[string]string, error)
		cited       []string
		refused     string
	}{
		{name: "one cited", brief: "fix the hook, as [memory#m2] says", cited: []string{lines["m2"]}},
		{name: "cited twice", brief: "[memory#m2] and again [memory#m2], fix the hook", cited: []string{lines["m2"]}},
		{name: "local id", brief: "fix the hook: split the ticket per [memory#m3-0a1f] then [memory#m1]", cited: []string{lines["m3-0a1f"], lines["m1"]}},
		{name: "none cited", brief: "fix the hook"},
		{name: "unknown id", brief: "fix the hook per [memory#m9]", refused: "[memory#m9]"},
		{name: "memory off", brief: "fix the hook per [memory#m2]", memory: func() (map[string]string, error) { return nil, nil }, refused: "[memory#m2]"},
		{name: "memory unreadable", brief: "fix the hook per [memory#m2]", memory: func() (map[string]string, error) { return nil, unreadable }, refused: unreadable.Error()},
	} {
		t.Run(row.name, func(t *testing.T) {
			script := &prefetchScript{brief: "fix the hook", lead: []llm.Decision{calling("spawn", map[string]any{"task": row.brief})}}
			base := turn.Config{
				Model:          script,
				Spend:          turn.SpendAPIKey,
				Tools:          turn.NewRegistry(),
				Caps:           turn.Caps{MaxSteps: 4},
				ResultBytesCap: 4096,
				ArtifactDir:    filepath.Join(t.TempDir(), "artifacts"),
				NewID:          func() string { return "turn-orchestrator" },
			}
			spawner := turn.NewSpawnTool("turn-orchestrator", base, &subagent.Roster{})
			spawner.Remembered = row.memory
			if spawner.Remembered == nil {
				spawner.Remembered = func() (map[string]string, error) { return lines, nil }
			}
			lead := base
			lead.Task, lead.Tools, lead.Inbox = "hand the hook to a sub-agent", turn.NewRegistry(spawner), spawner.Inbox
			var answered string
			if err := turn.Lead(context.Background(), lead, nil, nil, func(ended turn.Row, _ error) {
				for _, message := range ended.Conversation {
					if message.Role == llm.RoleTool && answered == "" {
						answered = message.Content
					}
				}
			}); err != nil {
				t.Fatalf("Lead: %v", err)
			}

			if row.refused != "" {
				if len(script.asked) != 0 || !strings.Contains(answered, row.refused) || !strings.Contains(answered, "refused") {
					t.Fatalf("the sub-agent was asked %d times and the lead read %q, want no sub-agent and a refusal naming %s", len(script.asked), answered, row.refused)
				}
				return
			}
			if len(script.asked) == 0 {
				t.Fatalf("the sub-agent was never asked; the lead read %q", answered)
			}
			opened := script.asked[0].Messages
			views := slices.DeleteFunc(slices.Clone(opened), func(message llm.Message) bool { return message.Origin.Source != llm.SourceMemoryView })
			if len(opened) < 2 || len(views) != 1 || opened[0].Origin.Source != llm.SourceMemoryView {
				t.Fatalf("the sub-agent's first request holds %d memory views in %d messages, want one before the brief", len(views), len(opened))
			}
			for id, line := range lines {
				if got := strings.Count(opened[0].Content, line); got != 1 {
					t.Errorf("%s is in the sub-agent's memory view %d times, want once:\n%s", id, got, opened[0].Content)
				}
			}
			first := opened[1].Content
			if len(row.cited) == 0 {
				if strings.Contains(first, "cited") || !strings.HasSuffix(first, row.brief) {
					t.Fatalf("a brief citing nothing changed:\n%s", first)
				}
				return
			}
			block, task, found := strings.Cut(first, "\n\n"+row.brief)
			if !found || task != "" {
				t.Fatalf("the brief does not end the first message whole:\n%s", first)
			}
			for id, line := range lines {
				want := 0
				for _, cited := range row.cited {
					if cited == line {
						want = 1
					}
				}
				if got := strings.Count(block, line); got != want {
					t.Errorf("%s is above the brief %d times, want %d:\n%s", id, got, want, first)
				}
			}
			at := -1
			for _, cited := range row.cited {
				if next := strings.Index(block, cited); next < at {
					t.Errorf("%q is out of the order the brief cites it:\n%s", cited, block)
				} else {
					at = next
				}
			}
		})
	}
}
