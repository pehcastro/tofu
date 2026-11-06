package loop

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"tofu/internal/agent/hooks"
	"tofu/internal/models/llm/core"
	"tofu/internal/platform/credential"
)

type NotCalledByHandError struct {
	Tool string
	Why  string
}

func (e NotCalledByHandError) Error() string { return e.Tool + " " + e.Why }

func answersOnlyInATurn() []string {
	return []string{SpawnToolName, messageTool{}.Name(), subAgentsTool{}.Name(), askTool{}.Name()}
}

func (c Config) CallByHand(ctx context.Context, call core.ToolCall) (core.Message, error) {
	if c.ResultBytesCap <= 0 {
		return core.Message{}, errors.New("turn: no tool result byte cap")
	}
	places, err := c.ResolvedPlaces()
	if err != nil {
		return core.Message{}, err
	}
	artifacts, err := NewArtifacts(cmp.Or(c.ArtifactDir, filepath.Join(places.State, "artifacts")), !c.TruncateResults)
	if err != nil {
		return core.Message{}, err
	}
	artifacts.preview = artifacts.preview.OnWire(c.Wire)
	references := &loadedReferences{held: map[string]string{}}
	if c.referencesOffered() {
		maps.Copy(references.held, c.References)
	}
	running := runningTools{fetch: artifacts.FetchTool(), history: historyTool{sessions: c.Sessions, current: func() string { return c.Session }}, references: references}
	tools := NewRegistry(c.answerable(c.withLoopTools(c.Tools.tools, running))...)
	if renamed, was := historyRenamed(call.Name); was {
		return core.Message{}, NotCalledByHandError{Tool: call.Name, Why: renamed}
	}
	if _, offered := tools.byName[call.Name]; !offered {
		offeredNames := make([]string, len(tools.tools))
		for i, tool := range tools.tools {
			offeredNames[i] = tool.Name()
		}
		return core.Message{}, NotCalledByHandError{Tool: call.Name, Why: "is not offered to this turn, which is offered " + strings.Join(offeredNames, ", ")}
	}
	var spawning spawnArgs
	estimating := call.Name == SpawnToolName && json.Unmarshal(call.Arguments, &spawning) == nil && spawning.Estimate
	if slices.Contains(answersOnlyInATurn(), call.Name) && !estimating {
		return core.Message{}, NotCalledByHandError{Tool: call.Name, Why: "answers only inside a live turn: it starts, reaches or reads a running sub-agent or its lead, which needs a model and a session that is running"}
	}
	redactor := credential.LoadKeyRedactor()
	if c.Redactor != nil {
		redactor = *c.Redactor
	}
	engine, _ := hookEngine(ctx, c, call.ID)
	fire := func(ctx context.Context, in hooks.Input) hooks.Verdict {
		in.Session, in.AgentType = c.Session, c.AgentType
		return engine.Fire(ctx, in)
	}
	ctx = context.WithValue(context.WithValue(ctx, hookEngineKey{}, engine), hookFireKey{}, fire)
	asked := call
	proxied, proxyRow := c.Proxy.rewrite(ctx, asked)
	call.Arguments = proxied
	pre := fire(ctx, hooks.Input{Event: hooks.PreToolUse, Tool: call.Name, Args: call.Arguments, CallID: call.ID})
	if pre.Args != nil {
		call.Arguments = pre.Args
	}
	gated := gatedCall{call: call, asked: asked, proxy: proxyRow, sift: siftOrNothing(c.Sift), redact: redactor, task: c.Task, fire: fire, hooks: hookRunOf(hooks.PreToolUse, pre), before: pre.Context}
	gated.refusal = cmp.Or(boundaryRefusal(tools, call), hookRefusal(ctx, c, GateRequest{Task: c.Task, Tool: call.Name, Args: call.Arguments, Call: call.ID}, pre))
	_, answer, _ := gated.run(ctx, tools, c.ResultBytesCap, artifacts, 0)
	return answer, nil
}
