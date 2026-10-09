package turn

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"tofu/internal/llm"
	"tofu/internal/shell"
	"tofu/internal/subagent"
)

type messageTool struct {
	orchestrator *SpawnTool
}

type messageArgs struct {
	To     string   `json:"to"`
	Text   string   `json:"text"`
	Stop   bool     `json:"stop,omitempty"`
	Answer string   `json:"answer,omitempty"`
	Do     string   `json:"do,omitempty"`
	From   string   `json:"from,omitempty"`
	Owns   []string `json:"owns,omitempty"`
}

const (
	answerAllow     = "allow"
	answerAllowHere = "allow_here"
	answerDeny      = "deny"
)

const (
	doStop    = "stop"
	doKill    = "kill"
	doRelease = "release"
	doBackup  = "backup"
	doGrant   = "grant"
	doRevoke  = "revoke"
	doReplace = "replace"
)

func (messageTool) Name() string { return "message" }

func (messageTool) Definition() llm.Tool {
	text := map[string]any{"type": "string"}
	return llm.Tool{
		Name: "message",
		Description: "sends a sub-agent more work or a correction, from this turn or any earlier one, and returns at once. " +
			"a running sub-agent reads text at its next step; one that has ended or was released resumes in the background with its conversation, taking back its paths if no other sub-agent holds them, " +
			"so use it rather than spawning a new sub-agent for those paths. either way its answer comes to you later as a report, as spawn's does. " +
			"to is the sub-agent's name as its report gives it, such as ts-dev-1. " +
			"answer allow or deny answers a sub-agent's call that waits on you because the gate asked; any text goes to the sub-agent with it. " +
			"allow_here allows it and every later call of the same kind from that sub-agent until its run ends, so it does not ask you again; a revoke or replace of its paths ends that too. " +
			"a turn that only answers asks ends after the message calls, with nothing to write. " +
			"do acts on the sub-agent instead of sending text. stop: a running one ends after the call it is in, keeping its paths and its conversation; one that is not running is released. " +
			"kill: it ends now, every shell it or its own sub-agents started is killed, and its paths are freed. " +
			"release: one that is not running frees its paths with no run. " +
			"backup: a copy of its conversation and of the files it wrote is kept, under a name the result gives. " +
			"grant, revoke and replace change the paths it holds, running or not, to owns added, owns taken away, or owns alone; a path another sub-agent holds is refused, as at spawn. " +
			"a grant tells it and resumes one that ended, so a write it was refused runs again; any text goes with it. a revoke tells it at its next step. " +
			"stopping, killing or releasing your own sub-agents, and changing what they hold, needs no one's leave. " +
			"from, a backup's name, with text, resumes it from that backup's conversation instead of its latest. " +
			"subagents with a name reads or diagnoses one without any of this",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{"to": text, "text": text, "from": text,
				"answer": map[string]any{"type": "string", "enum": []string{answerAllow, answerAllowHere, answerDeny}},
				"do":     map[string]any{"type": "string", "enum": []string{doStop, doKill, doRelease, doBackup, doGrant, doRevoke, doReplace}},
				"owns":   map[string]any{"type": "array", "items": text}},
			"required": []string{"to"},
		},
	}
}

func (m messageTool) Run(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args messageArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("message: arguments are not the expected shape: %w", err)
	}
	t := m.orchestrator
	if args.Stop {
		args.Do = doStop
	}
	switch args.Do {
	case "":
	case doStop:
		if _, stopping := t.Inbox.halt(args.To, (*heldSubAgent).stop); stopping {
			return Result{Content: args.To + " is stopping after the call it is in. its report, saying where it stopped, comes to you as a message.", Command: "stop " + args.To, SubAgent: args.To}, nil
		}
		return t.release(args.To, "was not running")
	case doRelease:
		return t.release(args.To, "is not running")
	case doKill:
		return t.kill(ctx, args.To)
	case doBackup:
		return t.backup(args.To)
	case doGrant, doRevoke, doReplace:
		return t.regrant(ctx, args)
	default:
		return Result{}, fmt.Errorf("message refused: do is %s, %s, %s, %s, %s, %s or %s, not %q", doStop, doKill, doRelease, doBackup, doGrant, doRevoke, doReplace, args.Do)
	}
	if args.Answer != "" {
		var held *heldSubAgent
		var waiting bool
		stands, answered := "", args.To+"'s call is answered "+args.Answer+", and it goes on."
		switch args.Answer {
		case answerAllow, answerDeny:
			held, waiting = t.Inbox.answer(args.To, args.Answer == answerAllow)
		case answerAllowHere:
			held, stands, waiting = t.Inbox.allowHere(args.To)
			answered = args.To + "'s call is allowed once, and it goes on: a hook's ask or a gate that could not answer never stands."
			if stands != "" {
				answered = args.To + "'s call is allowed, and every later call of this kind (" + stands + ") from it runs without asking you until its run ends."
			}
		default:
			return Result{}, fmt.Errorf("message refused: answer is %s, %s or %s, not %q", answerAllow, answerAllowHere, answerDeny, args.Answer)
		}
		switch {
		case held == nil:
			return Result{}, t.unknown(args.To)
		case !waiting:
			return Result{}, fmt.Errorf("message refused: no call of %s waits for an answer now: the ask ended with the run that made it, or with a restart. "+
				"send text to resume it with your answer, or do release to free its paths", args.To)
		}
		if strings.TrimSpace(args.Text) != "" {
			held.inbox.post(args.Text)
		}
		return Result{Content: answered, Command: args.Answer + " " + args.To, SubAgent: args.To}, nil
	}
	if strings.TrimSpace(args.Text) == "" {
		return Result{}, errors.New("message: text is required unless do or answer is given")
	}
	return t.send(ctx, args.To, args.Text, args.From)
}

func answeredAsksOnly(calls []ToolCallRow) bool {
	for _, call := range calls {
		var args messageArgs
		if call.Tool != (messageTool{}).Name() || call.Error != "" || json.Unmarshal(call.Args, &args) != nil || args.Answer == "" {
			return false
		}
	}
	return len(calls) > 0
}

func (t *SpawnTool) send(ctx context.Context, to, text, from string) (Result, error) {
	var backup []llm.Message
	if from != "" {
		var err error
		if backup, err = t.restoreBackup(to, from); err != nil {
			return Result{}, fmt.Errorf("message refused: %w", err)
		}
	}
	runCtx, cancel := context.WithCancel(ctx)
	held, posted, err := t.Inbox.resume(to, text, t.limits().Running, cancel)
	if held == nil || err != nil || posted {
		cancel()
	}
	switch {
	case held == nil:
		return Result{}, t.unknown(to)
	case err != nil:
		return Result{}, fmt.Errorf("message %w", err)
	case posted:
		return Result{Content: to + " is running and reads this at its next step. its report comes to you as a message when it ends.", Command: "message " + to, SubAgent: to}, nil
	}
	if backup != nil {
		held.kept.Lock()
		held.history, held.reload = backup, nil
		held.kept.Unlock()
	}
	err = t.recompose(held)
	var opened SubAgentModel
	if err == nil {
		opened, err = t.open(held.definition, held.askedEffort)
	}
	if err == nil {
		t.tree.mu.Lock()
		err = t.roster.Reclaim(held.agent.ID, resumedWords)
		t.tree.mu.Unlock()
	}
	if err != nil {
		if opened.Close != nil {
			opened.Close()
		}
		t.Inbox.ended(held, "", true, nil)
		cancel()
		return Result{}, fmt.Errorf("message refused: %w", err)
	}
	site, _ := ctx.Value(spawnSiteKey{}).(spawnSite)
	t.Inbox.hold(held, site.log)
	go t.background(runCtx, cancel, held, opened, site, text, nil)
	return Result{Content: to + " resumes in the background with its conversation. its report comes to you as a message when it ends.", Command: "message " + to, SubAgent: to}, nil
}

func (t *SpawnTool) regrant(ctx context.Context, args messageArgs) (Result, error) {
	held, _ := t.Inbox.find(args.To)
	if held == nil {
		return Result{}, t.unknown(args.To)
	}
	if len(args.Owns) == 0 {
		return Result{}, fmt.Errorf("message refused: do %s names its paths in owns, and owns is empty", args.Do)
	}
	before, did := held.boundary.Owns(), "replaced the paths you hold with "
	switch args.Do {
	case doGrant:
		did = "granted you "
	case doRevoke:
		did = "took back "
		for _, glob := range args.Owns {
			if !slices.Contains(before, glob) {
				return Result{}, fmt.Errorf("message refused: %s does not hold %q; it holds %s", args.To, glob, cmp.Or(strings.Join(before, ", "), "nothing"))
			}
		}
	}
	after := ownsAfter(args.Do, before, args.Owns)
	t.tree.mu.Lock()
	err := t.roster.Regrant(args.To, after, subagent.OwnsChange{At: t.clock(), Did: args.Do, Paths: args.Owns})
	t.tree.mu.Unlock()
	var collision subagent.CollisionError
	if errors.As(err, &collision) {
		return Result{}, fmt.Errorf("message refused: %s already holds %q, which overlaps %q. release or kill %s first, or send this work to it", collision.Holder, collision.HolderGlob, collision.Glob, collision.Holder)
	}
	if err != nil {
		return Result{}, fmt.Errorf("message refused: %w", err)
	}
	held.boundary.Regrant(after)
	if args.Do != doGrant {
		t.Inbox.unstand(held)
	}
	held.kept.Lock()
	held.environment = strings.Replace(held.environment, holdingWords(before), holdingWords(after), 1)
	held.kept.Unlock()
	notice := "the orchestrator " + did + strings.Join(args.Owns, ", ") + ": you now hold " + cmp.Or(strings.Join(after, ", "), "no paths") + "."
	if args.Do == doRevoke {
		held.inbox.post(notice + " a write there is refused from now on.")
		return Result{Content: args.To + " now holds " + cmp.Or(strings.Join(after, " "), "no paths") + ". it reads this at its next step, or when it next runs.", Command: args.Do + " " + args.To, SubAgent: args.To}, nil
	}
	sent, err := t.send(ctx, args.To, strings.TrimSpace(notice+" the write that was refused may run now.\n\n"+args.Text), "")
	if err != nil {
		return Result{}, err
	}
	sent.Content = args.To + " now holds " + strings.Join(after, " ") + ". " + sent.Content
	sent.Command = args.Do + " " + args.To
	return sent, nil
}

func ownsAfter(do string, before, owns []string) []string {
	switch do {
	case doGrant:
		return slices.Concat(before, slices.DeleteFunc(slices.Clone(owns), func(glob string) bool { return slices.Contains(before, glob) }))
	case doRevoke:
		return slices.DeleteFunc(slices.Clone(before), func(glob string) bool { return slices.Contains(owns, glob) })
	}
	return slices.Clone(owns)
}

func (t *SpawnTool) unknown(name string) error {
	var names []string
	for _, known := range t.roster.SubAgents() {
		names = append(names, known.ID)
	}
	if len(names) == 0 {
		return fmt.Errorf("refused: no sub-agent is named %q, and none has been spawned", name)
	}
	return fmt.Errorf("refused: no sub-agent is named %q; the sub-agents are %s", name, strings.Join(names, ", "))
}

func freedWords(owns []string) string {
	if len(owns) == 0 {
		return "it held no paths"
	}
	return "its paths " + strings.Join(owns, ", ") + " are free"
}

const resumeWords = " message with text resumes it, taking its paths back if no other sub-agent holds them."

func (t *SpawnTool) release(to, why string) (Result, error) {
	if _, running := t.Inbox.find(to); running {
		return Result{}, fmt.Errorf("message refused: %s is running, and a release would leave it writing to paths it no longer holds: do stop to end it after the call it is in, or do kill to end it now", to)
	}
	agent, found := t.roster.Release(to)
	if !found {
		return Result{}, t.unknown(to)
	}
	return Result{Content: to + " " + why + ", so it is released with no run, and " + freedWords(agent.Owns) + "." + resumeWords, Command: "release " + to, SubAgent: to}, nil
}

func ownedBy(owner string, ids []string) bool {
	return slices.ContainsFunc(ids, func(id string) bool { return owner == id || strings.HasPrefix(owner, id+"-f") })
}

func (t *SpawnTool) kill(ctx context.Context, to string) (Result, error) {
	held, ran := t.Inbox.halt(to, func(held *heldSubAgent) { held.cancel() })
	agent, found := t.roster.Release(to)
	if !found {
		return Result{}, t.unknown(to)
	}
	owners := []string{to}
	if held != nil {
		owners = held.lineage()
	}
	for _, nested := range owners[1:] {
		t.roster.Release(nested)
	}
	var killed []string
	var failed []error
	if registry := ShellRegistryFrom(ctx); registry != nil {
		for _, running := range registry.Own() {
			if !ownedBy(running.Owner, owners) {
				continue
			}
			if err := registry.Kill(running.Name); err != nil && !errors.Is(err, shell.ErrNotRunning) {
				failed = append(failed, err)
				continue
			}
			killed = append(killed, running.Name)
		}
	}
	ended, shells := "was not running", "no shell of its was running"
	if ran {
		ended = "is killed and its run ends now"
	}
	if len(killed) > 0 {
		shells = "its shells " + strings.Join(killed, ", ") + " are killed"
	}
	said := to + " " + ended + ", " + shells + ", and " + freedWords(agent.Owns) + "."
	if ran {
		said += " its report, saying where it stopped, comes to you as a message."
	}
	if err := errors.Join(failed...); err != nil {
		return Result{}, fmt.Errorf("message: %s a shell of its was not killed: %w", said, err)
	}
	return Result{Content: said + resumeWords, Command: "kill " + to, SubAgent: to}, nil
}
