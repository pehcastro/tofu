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

func TestTheMemoryViewOpensTheSessionOnceAfterTheSystemMessageThroughARememberAResumeAndAFork(t *testing.T) {
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
	firstTurn := 0
	for _, task := range []string{"read big.txt", "now read big.txt again"} {
		config.Task, firstTurn, model.replies = task, len(model.asked), []llm.Decision{reading, done}
		if _, err := turn.Run(context.Background(), config); err != nil {
			t.Fatalf("running %q: %v", task, err)
		}
		events, err := store.Events(id)
		if err != nil {
			t.Fatal(err)
		}
		if config.History, err = turn.ConversationFrom(events); err != nil {
			t.Fatal(err)
		}
		config.Memory = view + "\n" + saved
	}

	forkedAfterResume := false
	for i, request := range model.asked {
		if system := request.Messages[0]; system.Role != llm.RoleSystem || system.Content != "you are tofu" {
			t.Errorf("request %d opens on %s %q, want the system message alone and unchanged", i+1, system.Role, system.Content)
		}
		at := slices.IndexFunc(request.Messages, func(message llm.Message) bool { return message.Content == view })
		copies := len(slices.DeleteFunc(slices.Clone(request.Messages), func(message llm.Message) bool { return message.Content != view }))
		if at != 1 || copies != 1 || request.Messages[1].Role != llm.RoleUser {
			t.Errorf("request %d holds the memory view %d times, first at message %d, want once as the user message after the system message", i+1, copies, at)
		}
		for _, message := range request.Messages {
			forkedAfterResume = forkedAfterResume || i >= firstTurn && strings.HasPrefix(message.Content, "this is fork ")
			if strings.Contains(message.Content, saved) {
				t.Errorf("request %d carries the entry saved mid-session, so the opening changed under the cache", i+1)
			}
		}
	}
	if !forkedAfterResume {
		t.Fatalf("%d requests, the second turn's from request %d, and none after the resume forked", len(model.asked), firstTurn+1)
	}
}

func TestABriefCarriesTheMemoryTheLeadCitesVerbatimAndNoOther(t *testing.T) {
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
			first := script.asked[0].Messages[0].Content
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
