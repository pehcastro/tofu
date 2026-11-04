package loop

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"tofu/internal/agent/subagents"
	"tofu/internal/models/llm/core"
	"tofu/internal/store/session"
	"tofu/internal/tools/contract"
)

func claimNotes(t *testing.T, store *session.Store, owns ...string) func() error {
	t.Helper()
	name := "notes-chat"
	release, err := store.Claim(session.Header{ID: "side-1", Name: &name, Owns: owns})
	if err != nil {
		t.Fatal(err)
	}
	return release
}

func TestASideChatsHeldPathsHoldAgainstTheLeadsBash(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	store := session.OpenAt(t.TempDir())
	bash, err := NewBashTool(root)
	if err != nil {
		t.Fatal(err)
	}
	ran := func(owner, command string) (contract.Result, error) {
		args, _ := json.Marshal(bashArgs{Command: command})
		return claimedTools(NewRegistry(bash), store, owner).byName[bashToolName].Run(t.Context(), args)
	}
	if _, err := ran("lead", "echo a > plan.md"); err != nil {
		t.Fatalf("with no claim the lead's redirect is refused: %v", err)
	}
	release := claimNotes(t, store, "plan.md", "docs/**")
	for _, command := range []string{
		"echo x > plan.md",
		"echo x > ./docs/../plan.md",
		"echo x > " + filepath.ToSlash(filepath.Join(root, "plan.md")),
		"echo x >> docs/flag.md",
		"echo x | tee plan.md",
		"sed -i s/a/b/ plan.md",
		"rm plan.md",
		"touch docs/new.md",
		"mkdir -p docs/more",
		"cd docs && echo x > flag.md",
		"git init -q sub > plan.md",
	} {
		if _, err := ran("lead", command); err == nil || !strings.Contains(err.Error(), "side chat notes-chat") {
			t.Errorf("while notes-chat holds plan.md and docs/**, the lead's %q came back %v, want a refusal naming the side chat", command, err)
		}
	}
	for command, noted := range map[string]bool{"ls": false, "echo x > main.go": false, "git init -q sub": true, "echo x > $OUT": true} {
		result, err := ran("lead", command)
		if err != nil {
			t.Errorf("the lead's %q writes nothing held and was refused: %v", command, err)
		}
		if said := strings.Contains(result.Content, "side chat notes-chat holds plan.md, docs/**"); said != noted {
			t.Errorf("the lead's %q came back noting the side chat %v, want %v:\n%s", command, said, noted, result.Content)
		}
	}
	if _, err := ran("side-1", "echo s > plan.md"); err != nil {
		t.Errorf("the side chat's own redirect into its own claim was refused: %v", err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	if _, err := ran("lead", "echo z > plan.md"); err != nil {
		t.Fatalf("after the side turn the lead's redirect is refused: %v", err)
	}
	if body, _ := os.ReadFile(filepath.Join(root, "plan.md")); strings.TrimSpace(string(body)) != "z" {
		t.Errorf("plan.md holds %q after the claim ended, want z", body)
	}
}

func TestASideChatsHeldPathsHoldAgainstSpawnGrantAndARunningSubAgent(t *testing.T) {
	const writer = "write the plan"
	model := newCrew(map[string][]core.Decision{
		leadKey: {
			spawnCall("spawn-tree", writer, "**"),
			spawnCall("spawn-notes", "write notes", "**/*.md"),
			messageDoing("grant-plan", "sub-1", "grant", "*.md"),
			claimDecision("both refused"),
			claimDecision("sub-1 was refused too"),
		},
		writer: {
			writeCall("write-plan", "plan.md"),
			called(bashToolName, map[string]any{"command": "echo x > plan.md"}),
			claimDecision("plan.md is held"),
		},
	})
	release := model.hold(writer, 1)
	defer release()
	root := t.TempDir()
	t.Chdir(root)
	write, err := NewWriteTool(root)
	if err != nil {
		t.Fatal(err)
	}
	bash, err := NewBashTool(root)
	if err != nil {
		t.Fatal(err)
	}
	store := session.OpenAt(t.TempDir())
	base := Config{Model: model, Spend: SpendAPIKey, Tools: NewRegistry(write, bash), Caps: Caps{MaxSteps: 20}, ResultBytesCap: 4096,
		ArtifactDir: filepath.Join(root, "artifacts"), NewID: func() string { return "turn-lead" }, Sessions: store, Session: "lead"}
	spawn := NewSpawnTool("turn-lead", base, &subagents.Roster{})
	lead := base
	lead.Task, lead.Tools, lead.Inbox, lead.Sessions = "hand the work to sub-agents", NewRegistry(write, spawn), spawn.Inbox, nil
	typed := make(chan string, 1)
	led := startLead(t.Context(), lead, typed)
	waitFor(t, "sub-1's first request", func() bool { return len(model.requests(writer)) >= 1 })
	unclaim := claimNotes(t, store, "*.md", "**/*.md")
	t.Cleanup(func() { _ = unclaim() })
	typed <- "try the notes while sub-1 works"
	waitFor(t, "the lead's typed turn", func() bool { return model.answered(leadKey) >= 4 })
	release()
	led.wait(t)

	if _, err := os.Stat(filepath.Join(root, "plan.md")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("plan.md was written while notes-chat held it: %v", err)
	}
	for _, call := range []string{"spawn-notes", "grant-plan"} {
		if got := answerTo(t, model, call); !strings.Contains(got, "side chat notes-chat") {
			t.Errorf("the lead's %s meeting *.md while notes-chat holds it came back without the side chat's name:\n%s", call, got)
		}
	}
	answered := map[string]string{}
	for _, request := range model.requests(writer) {
		for _, message := range request.Messages {
			if message.Role == core.RoleTool {
				answered[message.ToolCallID] = message.Content
			}
		}
	}
	for _, call := range []string{"write-plan", bashToolName} {
		if !strings.Contains(answered[call], "side chat notes-chat") {
			t.Errorf("sub-1's %s into plan.md came back without the side chat's name: %q", call, answered[call])
		}
	}
}

func TestASideChatSendsItsParentsSystemBytesAndItsOwnInstructionsAfterTheHead(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	read, err := NewReadTool(root)
	if err != nil {
		t.Fatal(err)
	}
	write, err := NewWriteTool(root)
	if err != nil {
		t.Fatal(err)
	}
	bash, err := NewBashTool(root)
	if err != nil {
		t.Fatal(err)
	}
	model := newCrew(map[string][]core.Decision{leadKey: {claimDecision("parent"), spawnCall("side-spawn", "do the work", "**"), claimDecision("side"),
		claimDecision("side again"), claimDecision("lone side")}})
	store := session.OpenAt(t.TempDir())
	base := Config{Model: model, Spend: SpendAPIKey, Caps: Caps{MaxSteps: 4}, ResultBytesCap: 4096, ArtifactDir: filepath.Join(root, "artifacts")}
	spawn := NewSpawnTool("turn-lead", base, &subagents.Roster{})
	opened := func(id string) string {
		t.Helper()
		log, err := store.Open(session.Header{ID: id})
		if err != nil {
			t.Fatal(err)
		}
		if err := log.Close(); err != nil {
			t.Fatal(err)
		}
		return id
	}
	ran := func(id string) {
		t.Helper()
		prefix, err := HeldPrefix(store, id, "")
		if err != nil {
			t.Fatal(err)
		}
		composed, err := Compose(ComposeSpec{Task: "say hello", Environment: "cwd: " + root, ToolGuidance: "read before you answer"})
		if err != nil {
			t.Fatal(err)
		}
		config := base
		config.Tools, config.Inbox, config.System, config.Parts, config.Instructions, config.Task, config.Sessions, config.Session, config.Prefix =
			NewRegistry(read, write, bash, spawn), spawn.Inbox, composed.Head(), composed.Parts, "the project's rules", "say hello", store, id, prefix
		if _, err := Run(t.Context(), config); err != nil {
			t.Fatal(err)
		}
	}
	side := func(parent string) string {
		t.Helper()
		header, err := store.Header(parent)
		if err != nil {
			t.Fatal(err)
		}
		branched, err := BranchSide(store, header, SideBranch{Seed: SeedNone}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		return branched.Header.ID
	}
	system := func(request core.Request) string { return request.Messages[0].Content }
	afterTheHead := func(request core.Request) string {
		if at := slices.IndexFunc(request.Messages, promptChange); at > 0 {
			return request.Messages[at].Content
		}
		return ""
	}

	ran(opened("parent-1"))
	sideID := side("parent-1")
	ran(sideID)
	ran(sideID)
	asked := model.requests(leadKey)
	parent, first, again := asked[0], asked[1], asked[3]
	if system(first) != system(parent) || system(again) != system(parent) {
		t.Errorf("the side chat's system differs from its parent's, so it reads none of the parent's cache:\nparent %q\nfirst  %q\nagain  %q", system(parent), system(first), system(again))
	}
	tools := func(request core.Request) string {
		sent, err := json.Marshal(request.Tools)
		if err != nil {
			t.Fatal(err)
		}
		return string(sent)
	}
	if tools(first) != tools(parent) || tools(again) != tools(parent) {
		t.Errorf("the side chat's tool list differs from its parent's, so the cache stops before the system:\nparent %s\nfirst  %s", tools(parent), tools(first))
	}
	said := afterTheHead(first)
	if !strings.Contains(said, "[instructions]") || !strings.Contains(said, "This is a side chat beside session parent-1") || !strings.Contains(said, "so a command runs only when every step reads") {
		t.Errorf("the side chat's first request carries no side instructions and bash rules after the cached head: %q", said)
	}
	if strings.Contains(said, aToolIsGone) || strings.Contains(said, aToolChanged) {
		t.Errorf("the side chat announces a tool change its parent never made: %q", said)
	}
	if got := answerTo(t, model, "side-spawn"); !strings.Contains(got, "spawn refused") || !strings.Contains(got, "orchestrator's") {
		t.Errorf("the side chat's spawn came back without a refusal saying why: %q", got)
	}

	ran(side(opened("parent-2")))
	if lone := model.requests(leadKey)[4]; !strings.Contains(system(lone), "This is a side chat beside session parent-2") || afterTheHead(lone) != "" {
		t.Errorf("a side chat whose parent never sent a prompt composes its instructions into its own system, as before: %q", system(lone))
	}
}
