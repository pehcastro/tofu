package turn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"time"

	"tofu/internal/hook"
	"tofu/internal/llm"
	"tofu/internal/sys"
)

const (
	RuleOverrideToolName = "rule_override"
	RememberToolName     = "remember"
	AskPersonToolName    = "ask_person"
)

func gateExempt(tool string) bool {
	return tool == "browser_tabs" || tool == "browser_read" || tool == "browser_observe" || tool == "artifact_fetch" || tool == referenceToolName ||
		tool == RuleOverrideToolName || tool == RememberToolName || tool == AskPersonToolName || tool == LookupToolName || tool == messageTool{}.Name()
}

const theToolSucceededAndPrintedNothing = "the tool ran, succeeded and printed nothing."

const theToolFailedAndPrintedNothing = "the tool ran, failed and printed nothing."

const theToolWasAbortedAndPrintedNothing = "this call was cancelled elsewhere in the turn before it produced output. " +
	"it is not a failure of the command itself and does not need to be retried the same way."

const aRepeatRunsAgainNothing = "cached: the same call earlier in this turn, and nothing written since, so it was not run again\n"

func pointAtCopy(answer llm.Message, held ...[]llm.Message) string {
	for _, messages := range held {
		for _, message := range messages {
			if message.Role == llm.RoleTool && (message.Content == answer.Content || message.Content == aRepeatRunsAgainNothing+answer.Content) {
				return "unchanged: the same call as " + message.ToolCallID + " earlier in this turn, with nothing written since, so its result above is this result word for word"
			}
		}
	}
	return aRepeatRunsAgainNothing + answer.Content
}

type gatedCall struct {
	call    llm.ToolCall
	asked   llm.ToolCall
	proxy   *ProxyRow
	id      string
	parent  string
	author  string
	task    string
	sift    *ShellSift
	thrift  *ThriftSift
	redact  sys.KeyRedactor
	site    spawnSite
	model   Model
	verdict GateDecision
	gateErr string
	shadow  <-chan shadowVerdict
	refusal string
	fire    func(hook.Input) hook.Verdict
	hooks   []HookRun
}

type shadowVerdict struct {
	decision GateDecision
	err      string
}

func (g gatedCall) run(ctx context.Context, tools Registry, resultBytesCap int, artifacts Artifacts, batch int) (ToolCallRow, llm.Message, bool) {
	row, answer := rejectedCall(g.call, time.Now(), g.refusal, g.id, g.parent, g.author)
	repeat := false
	if g.refusal == "" {
		row, answer, repeat = g.execute(ctx, tools, resultBytesCap, artifacts)
	}
	verdict, gateErr := g.verdict, g.gateErr
	select {
	case judged := <-g.shadow:
		verdict, gateErr = judged.decision, judged.err
	default:
	}
	row.GateDecisionID, row.GateVerdict, row.GateError, row.GateReason = verdict.ID, string(verdict.Verdict), gateErr, verdict.Reason
	row.Refused, row.Hooks = g.refusal != "", append(g.hooks, row.Hooks...)
	row.ParallelBatch = batch
	row.Proxy = g.proxy
	row.Command = g.redact.Redact(row.Command)
	if len(row.Args) > 0 {
		row.Args = json.RawMessage(g.redact.Redact(string(row.Args)))
	}
	return row, answer, repeat
}

func (g gatedCall) execute(ctx context.Context, tools Registry, resultBytesCap int, artifacts Artifacts) (ToolCallRow, llm.Message, bool) {
	call := g.call
	started := time.Now()
	ctx = context.WithValue(context.WithValue(context.WithValue(ctx, shellOwnerKey{}, g.author), spawnSiteKey{}, g.site), runningModelKey{}, g.model)
	tool, ok := tools.byName[call.Name]
	if !ok {
		row, answer := rejectedCall(call, started, "unknown tool "+strconv.Quote(call.Name), g.id, g.parent, g.author)
		return row, answer, false
	}

	result, err := tool.Run(ctx, call.Arguments)
	if err == nil && g.proxy != nil && g.proxy.Ran != "" && proxyPanicked(result.Content) {
		g.proxy.ProxyBytes = len(result.Content)
		g.proxy.Note = g.proxy.Proxy + " panicked instead of filtering the output, so the command ran as it was asked for"
		call = g.asked
		result, err = tool.Run(ctx, call.Arguments)
	}
	if err != nil {
		row, answer := rejectedCall(call, started, g.redact.Redact(err.Error()), g.id, g.parent, g.author)
		return row, answer, false
	}
	result.Content, result.FailureText = g.redact.Redact(result.Content), g.redact.Redact(result.FailureText)
	var post hook.Verdict
	if !result.Repeat {
		post = g.fire(hook.Input{Event: hook.PostToolUse, Tool: call.Name, Args: call.Arguments, CallID: call.ID, Response: result.Content, Failed: result.FailureText != ""})
	}

	cut := g.cutShellResult(ctx, call.Name, result)
	thriftCut := g.cutThriftResult(ctx, call.Name, result)
	saved := cut.Saved
	text := cut.Text
	if thriftCut.Saved > 0 {
		text = thriftCut.Text
		saved += thriftCut.Saved
	}
	rendered, handle, storeErr := artifacts.Render(call.Name, call.Arguments, text, resultBytesCap)
	sum := sha256.Sum256([]byte(result.Content))
	row := ToolCallRow{
		ID:             g.id,
		Parent:         g.parent,
		Author:         g.author,
		Call:           call.ID,
		Tool:           call.Name,
		Args:           call.Arguments,
		Command:        result.Command,
		ExitCode:       result.ExitCode,
		ResultBytes:    len(result.Content),
		RenderedBytes:  len(rendered),
		ResultHash:     hex.EncodeToString(sum[:]),
		ResultHandle:   handle,
		SiftSavedBytes: saved,
		DurationMS:     time.Since(started).Milliseconds(),
		Error:          result.FailureText,
		SubAgentID:     result.SubAgent,
		Hooks:          hookRunOf(hook.PostToolUse, post),
	}
	if storeErr != nil {
		row.ResultHandleError = storeErr.Error()
	}
	outcome := row.Outcome()
	if result.Outcome == ResultAborted {
		outcome = llm.ToolOutcomeAborted
	}
	body := rendered
	if body == "" {
		switch outcome {
		case llm.ToolOutcomeAborted:
			body = theToolWasAbortedAndPrintedNothing
		case llm.ToolOutcomeFailed:
			body = theToolFailedAndPrintedNothing
		default:
			body = theToolSucceededAndPrintedNothing
		}
	}
	if post.Block != "" {
		body += "\n\na PostToolUse hook says: " + post.Block
	}
	if post.Context != "" {
		body += "\n\n" + post.Context
	}
	return row, llm.Message{
		Role:            llm.RoleTool,
		ToolCallID:      call.ID,
		Content:         body,
		ToolOutcome:     outcome,
		ToolResultBytes: row.ResultBytes,
		ToolExitCode:    row.ExitCode,
		Images:          result.Images,
	}, result.Repeat
}

func rejectedCall(call llm.ToolCall, started time.Time, reason, id, parent, author string) (ToolCallRow, llm.Message) {
	content := "error: " + reason
	row := ToolCallRow{ID: id, Parent: parent, Author: author, Call: call.ID, Tool: call.Name, Args: call.Arguments, Error: reason, DurationMS: time.Since(started).Milliseconds()}
	return row, llm.Message{
		Role:            llm.RoleTool,
		ToolCallID:      call.ID,
		Content:         content,
		ToolOutcome:     row.Outcome(),
		ToolResultBytes: len(content),
	}
}
