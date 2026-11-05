package loop

import (
	"cmp"
	"context"
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"tofu/internal/agent/hooks"
	"tofu/internal/models/llm/core"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/shell/process"
)

type hookEngineKey struct{}

type hookFireKey struct{}

func hookMessages(source core.Source, added []hooks.Context, at time.Time) []core.Message {
	messages := make([]core.Message, len(added))
	for i, one := range added {
		messages[i] = core.Message{Role: core.RoleUser, Content: one.Text, Origin: core.Origin{Source: source, Name: one.File, TakenAt: at}}
	}
	return messages
}

func hookMarked(source core.Source, added []hooks.Context) string {
	var marked string
	for _, one := range added {
		marked += "\n\n" + core.Origin{Source: source, Name: one.File}.Mark(one.Text)
	}
	return marked
}

func EndSession(ctx context.Context, project, session, reason string) []string {
	verdict := hooks.Load(project, process.Choice{}).Fire(ctx, hooks.Input{Event: hooks.SessionEnd, Session: session, Reason: reason})
	return verdict.Warnings
}

func hookEngine(ctx context.Context, config Config, turnID string) (*hooks.Engine, []string) {
	if held, inherited := ctx.Value(hookEngineKey{}).(*hooks.Engine); inherited {
		return held, nil
	}
	root, choice := config.Project, process.Choice{}
	if bash, found := underlyingBash(config.Tools.byName[bashToolName]); found {
		root, choice = cmp.Or(root, string(bash.root)), bash.choice
	}
	root, _ = filepath.Abs(cmp.Or(root, "."))
	engine := hooks.Load(root, choice)
	warnings, untrusted := engine.Problems(), engine.Untrusted()
	if len(untrusted) == 0 {
		return engine, warnings
	}
	listed := make([]string, len(untrusted))
	for i, unknown := range untrusted {
		file, _ := filepath.Rel(root, unknown.File)
		listed[i] = string(unknown.Event) + " " + cmp.Or(unknown.Matcher, "*") + ": " + unknown.Command + " (" + string(unknown.Trust) + ", " + file + ")"
	}
	told := config.Notify
	if told == nil {
		told = func(said string) { warnings = append(warnings, said) }
	}
	if config.Person == nil {
		told(strconv.Itoa(len(untrusted)) + " project hooks are not trusted, so they did not run: " + strings.Join(listed, "; ") +
			". run tofu hooks trust in the project to trust them")
		return engine, warnings
	}
	told("this project has hooks tofu has not run, and each runs a command on this machine:\n" + strings.Join(listed, "\n") +
		"\n1 runs them in this turn only, 2 refuses them until they change, 3 trusts them until they change")
	asked, _ := json.Marshal(map[string]string{"command": strings.Join(listed, "\n")})
	answer, err := config.Person(ctx, GateRequest{TurnID: turnID, Task: config.Task, Tool: "hooks", Args: asked}, GateDecision{})
	switch {
	case err != nil:
		return engine, append(warnings, "the person could not be asked about this project's hooks, so they did not run: "+err.Error())
	case answer == PersonAllowedOnce:
		err = engine.Answer(untrusted, hooks.AnswerOnce)
	case answer == PersonAlwaysHere:
		err = engine.Answer(untrusted, hooks.AnswerAlways)
	default:
		err = engine.Answer(untrusted, hooks.AnswerRefuse)
	}
	if err != nil {
		warnings = append(warnings, "the answer about this project's hooks was not saved, so it is asked again next turn: "+err.Error())
	}
	return engine, warnings
}

func hookRefusal(ctx context.Context, config Config, request GateRequest, pre hooks.Verdict) string {
	if pre.Block != "" {
		return "this call did not run: a PreToolUse hook refused it: " + pre.Block
	}
	if pre.Ask != "" {
		if config.Person != nil && config.Notify != nil && contract.SubAgentAsking(ctx) == "" {
			config.Notify("a PreToolUse hook asks you before " + request.Tool + " runs: " + pre.Ask)
		}
		if _, refused := personRefusal(ctx, config.Person, request, GateDecision{HookAsk: pre.Ask}, "this call did not run: a PreToolUse hook asks first: "+pre.Ask); refused != "" {
			return refused
		}
	}
	return personOnlyRefusal(ctx, config, request)
}
