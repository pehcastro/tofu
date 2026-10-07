package hook

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"tofu/internal/judge/ledger"
	"tofu/internal/konst"
	"tofu/internal/shell"
	"tofu/internal/sys"
)

type Input struct {
	Event       Event
	Session     string
	Turn        string
	Agent       string
	AgentType   string
	Tool        string
	Args        json.RawMessage
	CallID      string
	Response    string
	Failed      bool
	Prompt      string
	LastMessage string
	StopActive  bool
	Source      string
	Reason      string
	Gate        *GateFacts
	Spawn       *SpawnFacts
}

type Verdict struct {
	Block    string
	Ask      string
	Args     json.RawMessage
	Context  string
	Warnings []string
	Ran      int
	Gate     ledger.Verdict
	GateWhy  string
	Owns     []string
	Narrowed bool
	Runs     []Run
}

type Result struct {
	At         time.Time `json:"at"`
	Exit       int       `json:"exit"`
	DurationMS int64     `json:"duration_ms"`
	Problem    string    `json:"problem,omitempty"`
	Said       string    `json:"said,omitempty"`
	stdout     string
	stderr     string
	reply      *hookOutput
}

type stdinPayload struct {
	Session      string          `json:"session_id"`
	Transcript   string          `json:"transcript_path"`
	Cwd          string          `json:"cwd"`
	Mode         string          `json:"permission_mode"`
	Event        Event           `json:"hook_event_name"`
	Turn         string          `json:"turn_id"`
	Agent        string          `json:"agent_id,omitempty"`
	AgentType    string          `json:"agent_type,omitempty"`
	Tool         string          `json:"tool_name,omitempty"`
	ToolInput    json.RawMessage `json:"tool_input,omitempty"`
	ToolUseID    string          `json:"tool_use_id,omitempty"`
	ToolResponse *toolResponse   `json:"tool_response,omitempty"`
	Prompt       string          `json:"prompt,omitempty"`
	StopActive   *bool           `json:"stop_hook_active,omitempty"`
	LastMessage  string          `json:"last_assistant_message,omitempty"`
	Source       string          `json:"source,omitempty"`
	Reason       string          `json:"reason,omitempty"`
	Gate         *GateFacts      `json:"gate,omitempty"`
	Spawn        *SpawnFacts     `json:"spawn,omitempty"`
}

type toolResponse struct {
	Content string `json:"content"`
	IsError bool   `json:"is_error"`
}

func (e *Engine) Fire(ctx context.Context, in Input) Verdict {
	var subjects []string
	switch in.Event {
	case PreToolUse, PostToolUse, GateVerdict:
		subjects = toolNames(in.Tool)
	case SubagentStop:
		subjects = []string{in.AgentType}
	case SubagentSpawn:
		subjects = []string{in.Spawn.Definition}
	case SessionStart:
		subjects = []string{in.Source}
	case SessionEnd:
		subjects = []string{in.Reason}
	}
	var chosen []Hook
	for _, hook := range e.hooks {
		if hook.Event == in.Event && hook.Skipped == "" && hook.Trust.runs() && matches(hook.Matcher, subjects) {
			chosen = append(chosen, hook)
		}
	}
	if len(chosen) == 0 {
		return Verdict{}
	}
	payload := stdinPayload{Session: in.Session, Cwd: e.project, Mode: "default", Event: in.Event, Turn: in.Turn, Agent: in.Agent, AgentType: in.AgentType,
		Prompt: in.Prompt, LastMessage: in.LastMessage, Source: in.Source, Reason: in.Reason, Gate: in.Gate, Spawn: in.Spawn}
	if in.Tool != "" {
		payload.Tool, payload.ToolInput, payload.ToolUseID = claudeName(in.Tool), e.toClaude(in.Tool, in.Args), in.CallID
	}
	if in.Event == PostToolUse {
		payload.ToolResponse = &toolResponse{Content: in.Response, IsError: in.Failed}
	}
	if in.Event == Stop || in.Event == SubagentStop {
		payload.StopActive = &in.StopActive
	}
	stdin, _ := json.Marshal(payload)
	results := make([]Result, len(chosen))
	var running sync.WaitGroup
	for i, hook := range chosen {
		running.Go(func() { results[i] = e.run(ctx, hook, stdin) })
	}
	running.Wait()
	for i := range results {
		results[i].read(in.Event)
	}
	e.remember(chosen, results)
	var verdict Verdict
	for i, result := range results {
		said, problem := e.apply(in, result, " (a "+string(in.Event)+" hook in "+chosen[i].File+")", &verdict)
		stderr := strings.TrimSpace(result.stderr)
		verdict.Runs = append(verdict.Runs, Run{Event: in.Event, Command: chosen[i].Command, File: chosen[i].File, Level: chosen[i].Level, Exit: result.Exit,
			DurationMS: result.DurationMS, Decision: strings.Join(said, "; "), Problem: cmp.Or(result.Problem, problem), Stderr: stderr[:min(len(stderr), konst.HookSaidBytes)]})
	}
	if len(verdict.Context) > konst.HookContextBytes {
		verdict.Context = verdict.Context[:konst.HookContextBytes]
	}
	return verdict
}

func (r *Result) read(event Event) {
	out := strings.TrimSpace(r.stdout)
	if r.Problem != "" || r.Exit == 2 || !strings.HasPrefix(out, "{") || !strings.HasSuffix(out, "}") {
		return
	}
	reply, err := parseReply(event, out)
	if err != nil {
		r.Problem = "it printed output tofu does not accept: " + err.Error()
		return
	}
	r.reply = &reply
}

func joined(held, more string) string {
	if held == "" {
		return more
	}
	return held + "; " + more
}

func (e *Engine) apply(in Input, result Result, from string, verdict *Verdict) ([]string, string) {
	if result.Problem != "" {
		verdict.Warnings = append(verdict.Warnings, "a hook did not decide: "+result.Problem+from)
		return nil, ""
	}
	verdict.Ran++
	if result.Exit == 2 {
		why := cmp.Or(strings.TrimSpace(result.stderr), "the hook exited 2 and printed no reason")
		if in.Event == GateVerdict {
			said, _ := verdict.gate(in.Gate.Verdict, ledger.VerdictDeny, why, from)
			return []string{said}, ""
		}
		verdict.Block = joined(verdict.Block, why+from)
		return []string{"block: " + why}, ""
	}
	if result.Exit != 0 {
		verdict.Warnings = append(verdict.Warnings, "a hook exited "+strconv.Itoa(result.Exit)+": "+strings.TrimSpace(result.stderr)+from)
	}
	reply := result.reply
	if reply == nil {
		if out := strings.TrimSpace(result.stdout); result.Exit == 0 && out != "" && (in.Event == UserPromptSubmit || in.Event == SessionStart) {
			verdict.Context = joined(verdict.Context, out)
			return []string{"added context"}, ""
		}
		return nil, ""
	}
	var said []string
	if reply.Decision == "block" {
		why := cmp.Or(reply.Reason, "the hook blocked and gave no reason")
		verdict.Block, said = joined(verdict.Block, why+from), append(said, "block: "+why)
	}
	permission, why := ledger.Verdict(reply.Specific.Permission), reply.Specific.PermissionReason
	var problem string
	switch {
	case permission == ledger.VerdictUnset:
	case in.Event == GateVerdict:
		var gated string
		gated, problem = verdict.gate(in.Gate.Verdict, permission, cmp.Or(why, "the hook gave no reason"), from)
		said = append(said, gated)
	case permission == ledger.VerdictDeny:
		verdict.Block, said = joined(verdict.Block, cmp.Or(why, "the hook denied it and gave no reason")+from), append(said, "deny: "+why)
	case permission == ledger.VerdictAsk:
		verdict.Ask, said = joined(verdict.Ask, cmp.Or(why, "the hook asks the person")+from), append(said, "ask: "+why)
	default:
		said = append(said, "allow")
	}
	if len(reply.Specific.UpdatedInput) > 0 {
		args, isObject := e.fromClaude(in.Tool, reply.Specific.UpdatedInput)
		if !isObject {
			problem = "its updatedInput is not a JSON object, so the call runs as it was asked"
			verdict.Warnings = append(verdict.Warnings, "a hook's "+problem+from)
		}
		verdict.Args, said = args, append(said, "rewrote the input")
	}
	if reply.Specific.Context != "" {
		verdict.Context, said = joined(verdict.Context, reply.Specific.Context), append(said, "added context")
	}
	if owns := reply.Specific.Owns; owns != nil {
		said = append(said, "owns ["+strings.Join(*owns, ", ")+"]")
		switch outside := outsideOf(*owns, in.Spawn.Owns); {
		case outside != "":
			verdict.Block = joined(verdict.Block, "it narrowed owns to "+outside+", which is not inside what was asked, ["+strings.Join(in.Spawn.Owns, ", ")+"]"+from)
		case verdict.Narrowed && !slices.Equal(verdict.Owns, *owns):
			verdict.Block = joined(verdict.Block, "two hooks gave different owns, ["+strings.Join(verdict.Owns, ", ")+"] and ["+strings.Join(*owns, ", ")+"], and tofu does not intersect globs"+from)
		default:
			verdict.Owns, verdict.Narrowed = *owns, true
		}
	}
	if reply.SystemMessage != "" {
		verdict.Warnings = append(verdict.Warnings, reply.SystemMessage+from)
	}
	return said, problem
}

func (v *Verdict) gate(jev, to ledger.Verdict, why, from string) (string, string) {
	if jev == ledger.VerdictDeny && to == ledger.VerdictAllow {
		problem := "a GateVerdict hook cannot turn a deny into allow, so the deny stands"
		v.Warnings = append(v.Warnings, problem+from)
		return "allow: " + why, problem
	}
	if rank(to) > rank(v.Gate) {
		v.Gate, v.GateWhy = to, why+from
	}
	return string(to) + ": " + why, ""
}

type cappedOutput struct{ bytes.Buffer }

func (c *cappedOutput) Write(p []byte) (int, error) {
	if room := konst.HookOutputBytes - c.Len(); room > 0 {
		c.Buffer.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

func (e *Engine) run(ctx context.Context, hook Hook, stdin []byte) (result Result) {
	result.At = time.Now()
	defer func() { result.DurationMS = time.Since(result.At).Milliseconds() }()
	choice, refused := e.shellFor(hook)
	if refused != "" {
		result.Problem = refused
		return result
	}
	timeout := time.Duration(min(cmp.Or(hook.Timeout, konst.HookTimeoutSecondsDefault), konst.HookTimeoutSecondsCeiling)) * time.Second
	if hook.Event == SessionEnd {
		timeout = min(timeout, konst.HookSessionEndMillis*time.Millisecond)
	}
	bounded, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	timedOut := func() string {
		if ctx.Err() != nil {
			return "the turn was stopped while it ran"
		}
		return "timed out after " + timeout.String()
	}
	release, err := takeSlot(bounded, filepath.Join(e.state, slotsDir))
	if err != nil {
		result.Problem = "no hook slot came free: " + err.Error()
		if bounded.Err() != nil {
			result.Problem = timedOut() + ", waiting for one of the " + strconv.Itoa(konst.HookProcessesAtOnce) + " hook slots"
		}
		return result
	}
	defer release()
	cmd := choice.Command(bounded, e.project, hook.Command, "CLAUDE_PROJECT_DIR="+e.project)
	var stdout, stderr cappedOutput
	cmd.Stdin, cmd.Stdout, cmd.Stderr = bytes.NewReader(stdin), &stdout, &stderr
	cmd.WaitDelay = konst.HookKillWaitMillis * time.Millisecond
	tracked, err := shell.StartTracked(cmd)
	if err != nil {
		result.Problem = "it did not start: " + err.Error()
		return result
	}
	waitErr := cmd.Wait()
	tracked.Release()
	result.stdout, result.stderr = stdout.String(), stderr.String()
	var exited *exec.ExitError
	switch {
	case bounded.Err() != nil:
		result.Problem = timedOut()
	case waitErr != nil && !errors.As(waitErr, &exited):
		result.Problem = "it did not finish: " + waitErr.Error()
	default:
		result.Exit = cmd.ProcessState.ExitCode()
	}
	said, _, _ := strings.Cut(strings.TrimSpace(cmp.Or(result.stderr, result.stdout)), "\n")
	result.Said = said[:min(len(said), konst.HookSaidBytes)]
	return result
}

func (e *Engine) shellFor(hook Hook) (shell.Choice, string) {
	if hook.Shell == "powershell" {
		for _, name := range []string{"pwsh", "powershell"} {
			if path, err := exec.LookPath(name); err == nil {
				return shell.Choice{Path: path, Label: name}, ""
			}
		}
		return shell.Choice{}, "it asks for powershell, and neither pwsh nor powershell is on PATH"
	}
	e.resolve.Do(func() {
		if e.bash.Path == "" {
			e.bash, e.bashErr = shell.Resolve("")
		}
	})
	if e.bashErr != nil {
		return shell.Choice{}, "it needs bash, and no shell resolved: " + e.bashErr.Error()
	}
	switch name := strings.TrimSuffix(strings.ToLower(filepath.Base(e.bash.Path)), ".exe"); name {
	case "pwsh", "powershell":
		return shell.Choice{}, "it needs bash, and this machine resolved " + name + `: set "shell": "powershell" on the hook, or install Git for Windows`
	}
	return e.bash, ""
}

func (e *Engine) remember(chosen []Hook, results []Result) {
	e.mu.Lock()
	defer e.mu.Unlock()
	last := map[string]*Result{}
	e.readJSON(lastFile, &last)
	for i, hook := range chosen {
		last[hook.Hash] = &results[i]
	}
	if body, err := json.MarshalIndent(last, "", "  "); err == nil {
		_ = sys.WriteFile(filepath.Join(e.state, lastFile), body, 0o644)
	}
}

func takeSlot(ctx context.Context, dir string) (func(), error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	for {
		for slot := range konst.HookProcessesAtOnce {
			file, err := os.OpenFile(filepath.Join(dir, strconv.Itoa(slot)), os.O_CREATE|os.O_RDWR, 0o644)
			if err != nil {
				return nil, err
			}
			if lockFile(file) == nil {
				return func() {
					unlockFile(file)
					_ = file.Close()
				}, nil
			}
			_ = file.Close()
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(konst.HookSlotPollMillis * time.Millisecond):
		}
	}
}
