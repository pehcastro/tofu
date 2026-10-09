package turn

import (
	"cmp"
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"time"

	"tofu/internal/llm"
	"tofu/internal/session"
	"tofu/internal/subagent"
	"tofu/internal/sys"
)

type heldSubAgent struct {
	agent       subagent.SubAgent
	definition  subagent.Definition
	effort      llm.Effort
	askedEffort llm.Effort
	system      string
	environment string
	prefix      Prefix
	boundary    *subagent.Boundary
	scratch     sys.ScratchPlace
	inbox       *Inbox
	trace       spawnTrace
	history     []llm.Message
	reload      func() ([]llm.Message, error)
	kept        sync.Mutex
	restored    bool
	running     bool
	log         *session.Log
	check       *checkIn
	cancel      context.CancelFunc
	answer      chan bool
	askedPlace  string
	allowedHere []string
	forking     sync.Mutex
	forked      []Row
	calls       sync.Mutex
	open        []*openCall
	stopping    bool
}

type openCall struct {
	tool   string
	target string
	since  time.Time
}

func (h *heldSubAgent) started(cancel context.CancelFunc) {
	h.calls.Lock()
	defer h.calls.Unlock()
	h.running, h.cancel, h.stopping = true, cancel, false
}

func (h *heldSubAgent) stop() {
	h.calls.Lock()
	defer h.calls.Unlock()
	h.stopping = true
	if len(h.open) == 0 {
		h.cancel()
	}
}

func (h *heldSubAgent) opened(tool string, raw json.RawMessage) *openCall {
	var target struct{ Command, Path string }
	_ = json.Unmarshal(raw, &target)
	call := &openCall{tool: tool, target: cmp.Or(target.Command, target.Path, string(raw)), since: time.Now()}
	h.calls.Lock()
	defer h.calls.Unlock()
	h.open = append(h.open, call)
	return call
}

func (h *heldSubAgent) closed(call *openCall) {
	h.calls.Lock()
	defer h.calls.Unlock()
	h.open = slices.DeleteFunc(h.open, func(open *openCall) bool { return open == call })
	if h.stopping && len(h.open) == 0 {
		h.cancel()
	}
}

func (h *heldSubAgent) openCalls() []openCall {
	h.calls.Lock()
	defer h.calls.Unlock()
	var open []openCall
	for _, call := range h.open {
		open = append(open, *call)
	}
	return open
}

func (h *heldSubAgent) conversation() ([]llm.Message, error) {
	h.kept.Lock()
	defer h.kept.Unlock()
	if h.reload == nil {
		return h.history, nil
	}
	history, err := h.reload()
	if err == nil {
		h.history, h.reload = history, nil
	}
	return h.history, err
}

type watchedTool struct {
	tool Tool
	held *heldSubAgent
}

func (w watchedTool) Name() string { return w.tool.Name() }

func (w watchedTool) Definition() llm.Tool { return w.tool.Definition() }

func (w watchedTool) refusal(raw json.RawMessage) error {
	if checked, checks := w.tool.(bounded); checks {
		return checked.refusal(raw)
	}
	return nil
}

func (w watchedTool) Run(ctx context.Context, raw json.RawMessage) (Result, error) {
	call := w.held.opened(w.tool.Name(), raw)
	defer w.held.closed(call)
	return w.tool.Run(ctx, raw)
}

func (h *heldSubAgent) runsAs() string {
	if h.agent.Model == "" {
		return ""
	}
	runs := " as " + cmp.Or(h.agent.Agent, "the unnamed sub-agent") + " on " + h.agent.Model
	if h.effort != "" {
		runs += " at effort " + string(h.effort)
	}
	return runs
}

func (h *heldSubAgent) runningWords() string {
	said := h.agent.ID + " is running in the background" + h.runsAs()
	if len(h.agent.Owns) > 0 {
		said += ", holding " + strings.Join(h.agent.Owns, ", ")
	}
	if h.scratch.Root != "" {
		said += ", with its scratch folder " + h.scratch.Dir()
	}
	return said + ". its report comes to you as a message naming it when it ends, and message reaches it at its next step while it runs."
}

func (h *heldSubAgent) forkedSoFar() []Row {
	h.forking.Lock()
	defer h.forking.Unlock()
	return slices.Clone(h.forked)
}

func (h *heldSubAgent) remember(round Row) {
	if len(round.Conversation) == 0 {
		return
	}
	h.kept.Lock()
	defer h.kept.Unlock()
	h.history = Sendable(round.Conversation)
}
