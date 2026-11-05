package loop_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"tofu/internal/agent/context/recall"
	"tofu/internal/agent/hooks"
	"tofu/internal/agent/loop"
	"tofu/internal/agent/subagents"
	"tofu/internal/models/llm/core"
	"tofu/internal/store/memory/entries"
	"tofu/internal/store/session"
	"tofu/internal/tools/files/read"
)

type recordingModel struct {
	replies []core.Decision
	asked   []core.Request
}

func (m *recordingModel) Ask(_ context.Context, request core.Request) (core.Decision, error) {
	m.asked = append(m.asked, request)
	if len(m.replies) == 0 {
		return core.Decision{Build: "m1", Outcome: core.OutcomeMessage, Content: "done"}, nil
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
	reader, err := read.NewReadTool(root)
	if err != nil {
		t.Fatal(err)
	}
	view := "remembered for you, by scope.\nuser-global:\n0+1|project: [memory#m1] Mock rule: run the linter before a commit"
	saved := "[memory#m2-0a1f] Mock rule saved mid-session"
	reading, done := calling("read", map[string]any{"path": "big.txt"}), core.Decision{Build: "m1", Outcome: core.OutcomeMessage, Content: "done"}
	model := &recordingModel{}
	store, id := session.NewStore(t.TempDir()), session.NewEventID()
	config := loop.Config{
		Model:          model,
		Spend:          loop.SpendAPIKey,
		Tools:          loop.NewRegistry(reader),
		System:         "you are tofu",
		Memory:         view,
		Prefix:         &loop.Prefix{},
		Caps:           loop.Caps{MaxSteps: 4},
		ResultBytesCap: 1 << 16,
		Budget:         recall.Budget{}.At(12000, "a budget the second turn forks under"),
		ArtifactDir:    filepath.Join(root, "artifacts"),
		Sessions:       store,
		Session:        id,
		NewID:          func() string { return "turn-memory" },
	}
	resumed := "[memory#m3] Mock rule saved between sessions"
	forks := func(request core.Request) int {
		return len(slices.DeleteFunc(slices.Clone(request.Messages), func(message core.Message) bool { return !strings.HasPrefix(message.Content, "this is fork ") }))
	}
	for at, next := range []struct {
		task, memory string
		source       hooks.SessionSource
	}{
		{task: "read big.txt", memory: view},
		{task: "now read big.txt again", memory: view + "\n" + saved},
		{task: "read big.txt once more", memory: view + "\n" + saved + "\n" + resumed, source: hooks.SessionResumed},
	} {
		opened := len(model.asked)
		config.Task, config.Memory, config.SessionSource, model.replies = next.task, next.memory, next.source, []core.Decision{reading, done}
		if _, err := loop.Run(context.Background(), config); err != nil {
			t.Fatalf("running %q: %v", next.task, err)
		}
		events, err := store.Events(id)
		if err != nil {
			t.Fatal(err)
		}
		if config.History, err = loop.ConversationFrom(events); err != nil {
			t.Fatal(err)
		}
		forkedInTheTurn := false
		for i, request := range model.asked[opened:] {
			number := opened + i + 1
			if system := request.Messages[0]; system.Role != core.RoleSystem || system.Content != "you are tofu" {
				t.Errorf("request %d opens on %s %q, want the system message alone and unchanged", number, system.Role, system.Content)
			}
			views := slices.DeleteFunc(slices.Clone(request.Messages), func(message core.Message) bool { return message.Origin.Source != core.SourceMemoryView })
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
	home, project := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	shelves, err := entries.Open(project)
	if err != nil {
		t.Fatal(err)
	}
	lines := map[string]string{}
	var local string
	for _, entry := range []entries.Entry{
		{Scope: entries.Global, Kind: entries.KindPerson, Text: "Replies stay short and plain"},
		{Scope: entries.Project, Kind: entries.KindProject, Text: "The commit hook refuses attribution"},
		{Scope: entries.ProjectLocal, Kind: entries.KindProject, Text: "Tickets come before code here"},
	} {
		added, err := shelves.Add(entry, "")
		if err != nil {
			t.Fatal(err)
		}
		lines[added.ID], local = added.Line(), added.ID
	}
	leadBlock, err := entries.Block(project)
	if err != nil {
		t.Fatal(err)
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
		{name: "local id", brief: "fix the hook: split the ticket per [memory#" + local + "] then [memory#m1]", cited: []string{lines[local], lines["m1"]}},
		{name: "none cited", brief: "fix the hook"},
		{name: "unknown id", brief: "fix the hook per [memory#m9]", refused: "[memory#m9]"},
		{name: "memory off", brief: "fix the hook per [memory#m2]", memory: func() (map[string]string, error) { return nil, nil }, refused: "[memory#m2]"},
		{name: "memory unreadable", brief: "fix the hook per [memory#m2]", memory: func() (map[string]string, error) { return nil, unreadable }, refused: unreadable.Error()},
	} {
		t.Run(row.name, func(t *testing.T) {
			script := &prefetchScript{brief: "fix the hook", lead: []core.Decision{calling("spawn", map[string]any{"task": row.brief})}}
			base := loop.Config{
				Model:          script,
				Spend:          loop.SpendAPIKey,
				Tools:          loop.NewRegistry(),
				Caps:           loop.Caps{MaxSteps: 4},
				ResultBytesCap: 4096,
				ArtifactDir:    filepath.Join(t.TempDir(), "artifacts"),
				NewID:          func() string { return "turn-orchestrator" },
			}
			spawner := loop.NewSpawnTool("turn-orchestrator", base, &subagents.Roster{})
			spawner.Project, spawner.Remembered = project, row.memory
			if spawner.Remembered == nil {
				spawner.Remembered = func() (map[string]string, error) { return lines, nil }
			}
			lead := base
			lead.Task, lead.Tools, lead.Inbox = "hand the hook to a sub-agent", loop.NewRegistry(spawner), spawner.Inbox
			var answered string
			if err := loop.Lead(context.Background(), lead, nil, nil, func(ended loop.Row, _ error) {
				for _, message := range ended.Conversation {
					if message.Role == core.RoleTool && answered == "" {
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
			views := slices.DeleteFunc(slices.Clone(opened), func(message core.Message) bool { return message.Origin.Source != core.SourceMemoryView })
			if len(opened) < 2 || len(views) != 1 || opened[0].Origin.Source != core.SourceMemoryView {
				t.Fatalf("the sub-agent's first request holds %d memory views in %d messages, want one before the brief", len(views), len(opened))
			}
			if opened[0].Content != leadBlock {
				t.Errorf("the sub-agent's memory view is\n%s\nwant the lead's block\n%s", opened[0].Content, leadBlock)
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
