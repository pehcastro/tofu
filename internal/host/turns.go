package host

import (
	"bufio"
	"cmp"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"tofu/internal/session"
	"tofu/internal/turn"
)

const undoLedgerName = "undo-turns.jsonl"

type SessionTurns struct {
	Session   string       `json:"session"`
	Turns     []TurnDigest `json:"turns"`
	Snapshots int          `json:"snapshots"`
}

type SessionTurnUpdated struct {
	Identity
	Digest TurnDigest `json:"digest"`
}

type TurnDigest struct {
	Turn        string              `json:"turn"`
	At          time.Time           `json:"at"`
	EndedAt     *time.Time          `json:"ended_at,omitempty"`
	WorkedForMs int64               `json:"worked_for_ms"`
	Origin      Origin              `json:"origin"`
	Asked       string              `json:"asked"`
	Summary     string              `json:"summary,omitempty"`
	Status      Status              `json:"status,omitempty"`
	Outcome     TurnOutcome         `json:"outcome,omitempty"`
	LoopGuard   *turn.LoopGuardStop `json:"loop_guard,omitempty"`
	Error       string              `json:"error,omitempty"`
	Held        []TurnHold          `json:"held"`
	Agents      []TurnAgent         `json:"agents"`
	Files       []TurnFile          `json:"files"`
	Calls       int                 `json:"calls"`
	Tokens      TurnTokens          `json:"tokens"`
	CostUSD     float64             `json:"cost_usd"`
}

type TurnOutcome string

func (TurnOutcome) enum() []string {
	var names []string
	for _, outcome := range session.AllOutcomes() {
		names = append(names, outcome.String())
	}
	return names
}

type HeldBy string

const (
	HeldByGate HeldBy = "gate"
	HeldByHook HeldBy = "hook"
)

func (HeldBy) enum() []string { return []string{string(HeldByGate), string(HeldByHook)} }

type TurnHold struct {
	Call    string `json:"call"`
	Agent   string `json:"agent,omitempty"`
	Tool    string `json:"tool"`
	By      HeldBy `json:"by"`
	Verdict string `json:"verdict,omitempty"`
}

type TurnAgent struct {
	Agent      string     `json:"agent"`
	Definition string     `json:"definition,omitempty"`
	Status     string     `json:"status"`
	StartedAt  time.Time  `json:"started_at"`
	EndedAt    *time.Time `json:"ended_at,omitempty"`
	Report     string     `json:"report,omitempty"`
}

type TurnFile struct {
	Path    string `json:"path"`
	Op      EditOp `json:"op"`
	Added   int    `json:"added"`
	Removed int    `json:"removed"`
	Agent   string `json:"agent,omitempty"`
}

type TurnTokens struct {
	Input     int `json:"input"`
	Output    int `json:"output"`
	CacheRead int `json:"cache_read"`
}

func (s *server) turns(p SessionParams) (any, error) {
	store, err := session.OpenIn(s.Host.dir)
	if err != nil {
		return nil, err
	}
	return sessionTurns(store, cmp.Or(p.Session, s.focus().host.ID()))
}

func (s *server) turnEnded(l *lane, id string) {
	store, err := session.OpenIn(s.Host.dir)
	if err != nil {
		return
	}
	answer, err := sessionTurns(store, l.host.ID())
	at := slices.IndexFunc(answer.Turns, func(digest TurnDigest) bool { return digest.Turn == id })
	if err != nil || at < 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.box.push(kept("session.turns.updated", &SessionTurnUpdated{Identity: l.items.identity("", id), Digest: answer.Turns[at]}))
}

func sessionTurns(store *session.Store, handle string) (SessionTurns, error) {
	header, err := store.Header(handle)
	if err != nil {
		return SessionTurns{}, err
	}
	lineage, err := store.Ancestors(header.ID)
	if err != nil {
		return SessionTurns{}, err
	}
	slices.Reverse(lineage)
	lineage = append(lineage, header)
	digests := &turnDigests{byTurn: map[string]*TurnDigest{}, reports: map[string]string{}, calls: map[string]calledWith{}}
	answer := SessionTurns{Session: header.ID, Turns: []TurnDigest{}}
	for _, from := range lineage {
		events, err := store.Events(from.ID)
		if err != nil {
			return SessionTurns{}, err
		}
		for _, event := range events {
			digests.note(event)
		}
		answer.Snapshots += undoSnapshots(store.Dir(from.ID))
	}
	runs := slices.SortedFunc(maps.Values(recordedRuns(store, header.ID)), func(a, b session.AgentRun) int { return a.StartedAt.Compare(b.StartedAt) })
	for _, id := range digests.order {
		digest := digests.byTurn[id]
		for _, run := range runs {
			if run.SpawnTurn == id {
				digest.Agents = append(digest.Agents, TurnAgent{Agent: run.Agent, Definition: run.Definition, Status: run.Status, StartedAt: run.StartedAt, EndedAt: run.EndedAt, Report: digests.reports[run.Agent]})
			}
		}
		answer.Turns = append(answer.Turns, *digest)
	}
	return answer, nil
}

type turnDigests struct {
	order   []string
	byTurn  map[string]*TurnDigest
	reports map[string]string
	calls   map[string]calledWith
}

type calledWith struct {
	tool    string
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (d *turnDigests) note(event session.Event) {
	if event.Kind == session.EventReport {
		var report session.ReportBody
		if json.Unmarshal(event.Body, &report) == nil && report.Text != "" {
			d.reports[event.Agent] = report.Text
		}
		return
	}
	digest := d.byTurn[event.Turn]
	if digest == nil && event.Agent == "" && event.Turn != "" {
		digest = &TurnDigest{Turn: event.Turn, At: event.At, Held: []TurnHold{}, Agents: []TurnAgent{}, Files: []TurnFile{}}
		d.byTurn[event.Turn], d.order = digest, append(d.order, event.Turn)
	}
	if digest == nil {
		return
	}
	switch event.Kind {
	case session.EventTurnStart:
		var start session.TurnStart
		if event.Agent == "" && json.Unmarshal(event.Body, &start) == nil {
			digest.Asked = cmp.Or(digest.Asked, start.Task)
		}
	case session.EventMessage:
		var said session.MessageBody
		if event.Agent != "" || json.Unmarshal(event.Body, &said) != nil {
			return
		}
		if said.Role == session.RoleUser && digest.Origin.Kind == "" {
			digest.Origin = originFrom(said.Origin)
		}
		if text := strings.TrimSpace(said.Content); said.Role == session.RoleAssistant && text != "" {
			digest.Summary = text
		}
	case session.EventRequest:
		var step session.StepBody
		if json.Unmarshal(event.Body, &step) == nil {
			digest.Tokens.Input += step.PromptTokens
			digest.Tokens.Output += step.CompletionTokens
			digest.Tokens.CacheRead += step.CacheReadTokens
		}
	case session.EventToolCall:
		var call session.CallBody
		_ = json.Unmarshal(event.Body, &call)
		args := calledWith{tool: call.Tool}
		_ = json.Unmarshal(call.Args, &args)
		digest.Calls++
		d.calls[event.Call] = args
	case session.EventToolResult:
		d.result(digest, event)
	case session.EventTurnEnd:
		var ended turn.Row
		if event.Agent != "" || json.Unmarshal(event.Body, &ended) != nil {
			return
		}
		_, digest.Status = doneWords(ended.Outcome, ended.Guard)
		if ended.Error != "" {
			digest.Status = StatusFailed
		}
		at := event.At
		digest.EndedAt, digest.Outcome, digest.LoopGuard, digest.Error = &at, TurnOutcome(ended.Outcome.String()), ended.Guard, ended.Error
		digest.WorkedForMs += ended.WallClockMS
		digest.CostUSD += ended.TotalCostUSD
	}
}

func (d *turnDigests) result(digest *TurnDigest, event session.Event) {
	var ran turn.ToolCallRow
	var said session.ResultBody
	if json.Unmarshal(event.Body, &ran) != nil || json.Unmarshal(event.Body, &said) != nil {
		return
	}
	called := d.calls[event.Call]
	if ran.Refused {
		held := TurnHold{Call: event.Call, Agent: event.Agent, Tool: cmp.Or(ran.Tool, called.tool), By: HeldByGate, Verdict: ran.GateVerdict}
		if slices.ContainsFunc(ran.Hooks, func(hook turn.HookRun) bool { return hook.Block != "" }) {
			held.By = HeldByHook
		}
		digest.Held = append(digest.Held, held)
		return
	}
	edited := TurnFile{Path: called.Path, Agent: event.Agent}
	switch {
	case edited.Path == "" || said.ToolOutcome == session.ToolOutcomeFailed:
		return
	case strings.HasPrefix(said.Content, unifiedDiffHeader):
		edited.Op = EditModify
		for _, hunk := range hunksOf(said.Content) {
			for _, line := range hunk.Lines {
				switch line.Kind {
				case LineAdded:
					edited.Added++
				case LineRemoved:
					edited.Removed++
				case LineContext:
				default:
					panic("host: unknown line kind " + string(line.Kind))
				}
			}
		}
	case strings.HasPrefix(said.Content, createdFilePrefix):
		edited.Op, edited.Added = EditCreate, lineCount(called.Content)
	default:
		return
	}
	at := slices.IndexFunc(digest.Files, func(file TurnFile) bool { return file.Path == edited.Path && file.Agent == edited.Agent })
	if at < 0 {
		digest.Files = append(digest.Files, edited)
		return
	}
	digest.Files[at].Added += edited.Added
	digest.Files[at].Removed += edited.Removed
	if edited.Op == EditCreate {
		digest.Files[at].Op = EditCreate
	}
}

func undoSnapshots(dir string) int {
	file, err := os.Open(filepath.Join(dir, undoLedgerName))
	if err != nil {
		return 0
	}
	defer func() { _ = file.Close() }()
	count := 0
	for lines := bufio.NewScanner(file); lines.Scan(); {
		var mark struct {
			Start string `json:"start"`
		}
		if json.Unmarshal(lines.Bytes(), &mark) == nil && mark.Start != "" {
			count++
		}
	}
	return count
}
