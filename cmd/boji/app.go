package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"

	"boji/interface/tui"
	"boji/interface/tui/frame"
	"boji/internal/judge/jev"
	"boji/internal/konst"
	"boji/internal/llm"
	"boji/internal/llm/cred"
	"boji/internal/llm/quota"
	"boji/internal/turn"
)

const (
	noTerminal   = "boji: the app needs a terminal. with input redirected, use boji run --dir <dir> <task>"
	gateOffNote  = "gate off: OPENROUTER_KEY is not set, so no tool call is judged"
	noCredential = "there is no anthropic subscription credential, so no model can answer"
	loginFix     = "press l to run boji login anthropic, which opens the browser and stores the credential"
)

func appVerb(in io.Reader, out, errOut io.Writer) int {
	file, isFile := in.(*os.File)
	if !isFile || !term.IsTerminal(file.Fd()) {
		_, _ = fmt.Fprintln(errOut, noTerminal)
		return exitUsage
	}
	dir, err := os.Getwd()
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "boji: the working directory is unreadable: %v\n", err)
		return exitVerdict
	}
	selected, err := chooseModel(runOpts{wire: wireSubscription})
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "boji: %v\n", err)
		return exitVerdict
	}
	note := ""
	if _, err := jev.Key(filepath.Join(dir, ".env")); err != nil {
		note = gateOffNote
	}
	if err := tui.Run(tui.Options{
		Repo:         filepath.Base(dir),
		Branch:       branchOf(dir),
		Model:        selected.ID,
		Note:         note,
		Requirements: appRequirements(),
		Recheck:      appRequirements,
		Login:        func() *exec.Cmd { return exec.Command(os.Args[0], "login", wireSubscription) },
		Quota:        appQuota,
		Turn:         appTurn(dir),
	}); err != nil {
		_, _ = fmt.Fprintf(errOut, "boji: %v\n", err)
		return exitVerdict
	}
	_, _ = fmt.Fprintln(out, "boji: session ended")
	return exitOK
}

func appRequirements() []tui.Requirement {
	_, _, store, err := subscriptionCredential(cred.Anthropic)
	if store != nil {
		defer func() { _ = store.Close() }()
	}
	if err == nil {
		return nil
	}
	return []tui.Requirement{{What: noCredential, Fix: loginFix}}
}

func branchOf(dir string) string {
	head, err := os.ReadFile(filepath.Join(dir, ".git", "HEAD"))
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.TrimSpace(string(head)), "ref: refs/heads/")
}

func appQuota() frame.Quota {
	results, err := pollCredentials(context.Background(), time.Now)
	if err != nil {
		return frame.Quota{}
	}
	var fullest frame.Quota
	for _, result := range results {
		if result.err != nil || result.report.Provider != quota.Anthropic {
			continue
		}
		fullest.Label = string(result.report.Provider)
		for _, window := range result.report.Windows {
			if !window.Used.Reported || strings.Contains(window.ID, ":") || window.Used.Fraction < fullest.Fraction {
				continue
			}
			fullest = frame.Quota{
				Label:    string(result.report.Provider) + " " + window.ID,
				Fraction: window.Used.Fraction,
				Reported: true,
				ResetsAt: window.ResetsAt,
			}
		}
	}
	return fullest
}

func appTurn(dir string) tui.Turn {
	return func(ctx context.Context, task string, emit func(tui.Event)) {
		fail := func(err error) { emit(tui.Event{Kind: tui.EventFailure, Text: err.Error()}) }
		opts := runOpts{
			dir:          dir,
			task:         task,
			wire:         wireSubscription,
			toolSet:      toolSetFull,
			maxSteps:     konst.TurnMaxSteps,
			maxWallMS:    konst.TurnMaxWallClockMillis,
			maxDecisions: konst.TurnMaxDecisions,
		}
		selected, err := chooseModel(opts)
		if err != nil {
			fail(err)
			return
		}
		registry, err := buildRunTools(dir, opts.toolSet)
		if err != nil {
			fail(err)
			return
		}
		model, spend, store, err := runModel(opts, selected.ID)
		if store != nil {
			defer func() { _ = store.Close() }()
		}
		if err != nil {
			fail(err)
			return
		}
		gate, gateErr := newToolGate(dir)
		if gateErr != nil {
			emit(tui.Event{Kind: tui.EventNote, Text: "the tool gate is off: " + gateErr.Error()})
		}

		watch := &appWatcher{inner: model, gate: gate, emit: emit}
		row, runErr := turn.Run(ctx, runConfig(opts, registry, watch, spend, gate))
		if runErr != nil {
			fail(runErr)
		}
		if writeErr := writeRunRow(row); writeErr != nil {
			fail(writeErr)
		}
		emit(tui.Event{
			Kind: tui.EventDone,
			Text: fmt.Sprintf("turn %s, %d steps, %d ms, quota windows %s",
				row.Outcome, len(row.Steps), row.WallClockMS, selected.WindowText()),
		})
	}
}

type appWatcher struct {
	inner turn.Model
	gate  *toolGate
	emit  func(tui.Event)
	seen  int
	in    int
	out   int
}

func (a *appWatcher) Ask(ctx context.Context, request llm.Request) (llm.Decision, error) {
	for _, message := range request.Messages[min(a.seen, len(request.Messages)):] {
		if message.Role == llm.RoleTool {
			a.emit(tui.Event{Kind: tui.EventToolResult, Text: resultLine(message.Content)})
		}
	}
	a.seen = len(request.Messages)

	decision, err := a.inner.Ask(ctx, request)
	if err != nil {
		return decision, err
	}
	a.in += decision.Usage.InputTokens
	a.out += decision.Usage.OutputTokens
	stats := tui.Event{Kind: tui.EventStats, Model: decision.Build, TokensIn: a.in, TokensOut: a.out}
	if a.gate != nil {
		stats.Decisions = a.gate.decisions
	}
	a.emit(stats)

	if text := strings.TrimSpace(decision.Content); text != "" {
		a.emit(tui.Event{Kind: tui.EventText, Text: text})
	}
	for _, call := range decision.ToolCalls {
		a.emit(tui.Event{Kind: tui.EventToolCall, Tool: call.Name, Text: callSummary(call)})
	}
	return decision, nil
}

func callSummary(call llm.ToolCall) string {
	var fields map[string]any
	if err := json.Unmarshal(call.Arguments, &fields); err != nil {
		return oneLine(string(call.Arguments))
	}
	for _, key := range []string{"command", "path", "pattern", "glob"} {
		if value, ok := fields[key].(string); ok {
			return oneLine(value)
		}
	}
	return oneLine(string(call.Arguments))
}

func resultLine(content string) string {
	head, _, split := strings.Cut(strings.TrimRight(content, "\n"), "\n")
	head = strings.TrimSpace(head)
	if !split {
		return head
	}
	return head + " …  " + strconv.Itoa(len(content)) + " bytes"
}

func oneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}
