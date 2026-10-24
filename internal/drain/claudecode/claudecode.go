package claudecode

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"tofu/internal/drain"
	"tofu/internal/llm"
	"tofu/internal/session"
)

const (
	transcriptExt  = ".jsonl"
	metaExt        = ".meta.json"
	agentPrefix    = "agent-"
	anthropicWire  = "anthropic"
	syntheticModel = "<synthetic>"
)

type origin struct {
	Kind string `json:"kind"`
}

func (from origin) source() llm.Source {
	switch from.Kind {
	case "human":
		return llm.SourceTyped
	case "task-notification":
		return llm.SourceReport
	case "peer", "coordinator":
		return llm.SourceMessage
	}
	return llm.SourceNotice
}

type usage struct {
	Input  int `json:"input_tokens"`
	Output int `json:"output_tokens"`
	Read   int `json:"cache_read_input_tokens"`
	Write  int `json:"cache_creation_input_tokens"`
	Split  struct {
		Write5m int `json:"ephemeral_5m_input_tokens"`
		Write1h int `json:"ephemeral_1h_input_tokens"`
	} `json:"cache_creation"`
}

type block struct {
	Type  string `json:"type"`
	ID    string `json:"id"`
	Name  string `json:"name"`
	Input struct {
		Command  string `json:"command,omitempty"`
		FilePath string `json:"file_path,omitempty"`
	} `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	IsError   bool            `json:"is_error"`
	Content   json.RawMessage `json:"content"`
}

type row struct {
	Type            string    `json:"type"`
	Subtype         string    `json:"subtype"`
	UUID            string    `json:"uuid"`
	Timestamp       time.Time `json:"timestamp"`
	ScheduledFireID string    `json:"scheduledFireId"`
	Origin          origin    `json:"origin"`
	Attachment      struct {
		Type   string `json:"type"`
		Origin origin `json:"origin"`
	} `json:"attachment"`
	CompactMetadata struct {
		PreTokens int `json:"preTokens"`
	} `json:"compactMetadata"`
	Message struct {
		ID      string          `json:"id"`
		Model   string          `json:"model"`
		Content json.RawMessage `json:"content"`
		Usage   usage           `json:"usage"`
	} `json:"message"`
}

type request struct {
	Model      string   `json:"model"`
	Prompt     int      `json:"prompt_tokens"`
	Completion int      `json:"completion_tokens"`
	Read       int      `json:"cache_read_tokens"`
	Write      int      `json:"cache_write_tokens"`
	Write5m    int      `json:"cache_write_5m_tokens"`
	Write1h    int      `json:"cache_write_1h_tokens"`
	ToolCalls  []string `json:"tool_calls,omitempty"`
	eventIndex int
}

type issuer struct{ agent, turn string }

type transcript struct {
	agent    string
	turn     string
	events   []session.Event
	requests map[string]*request
	issued   map[string]issuer
}

type meta struct {
	AgentType string `json:"agentType"`
	ToolUseID string `json:"toolUseId"`
}

func Read(path string, in drain.Input) (drain.Report, error) {
	id := strings.TrimSuffix(filepath.Base(path), transcriptExt)
	subagents, err := filepath.Glob(filepath.Join(strings.TrimSuffix(path, transcriptExt), "subagents", agentPrefix+"*"+transcriptExt))
	if err != nil {
		return drain.Report{}, err
	}
	paths := append([]string{path}, subagents...)
	read, failed := make([]*transcript, len(paths)), make([]error, len(paths))
	next := make(chan int, len(paths))
	for at := range paths {
		next <- at
	}
	close(next)
	var workers sync.WaitGroup
	for range min(runtime.GOMAXPROCS(0), len(paths)) {
		workers.Go(func() {
			for at := range next {
				agent := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(paths[at]), agentPrefix), transcriptExt)
				if at == 0 {
					agent = ""
				}
				read[at], failed[at] = readFile(paths[at], agent, id)
			}
		})
	}
	workers.Wait()
	if err := errors.Join(failed...); err != nil {
		return drain.Report{}, err
	}
	issued := map[string]issuer{}
	for _, file := range read {
		for call, from := range file.issued {
			issued[call] = from
		}
	}
	spawnedBy, events := map[string]string{}, read[0].events
	for at, file := range read[1:] {
		var described meta
		if body, err := os.ReadFile(strings.TrimSuffix(subagents[at], transcriptExt) + metaExt); err == nil {
			_ = json.Unmarshal(body, &described)
		}
		spawnedBy[file.agent] = described.ToolUseID
		body, _ := json.Marshal(session.SpawnBody{Agent: file.agent, Definition: described.AgentType})
		events = append(events, session.Event{Agent: file.agent, Kind: session.EventSpawn, Body: body})
	}
	for _, file := range read[1:] {
		from := issued[spawnedBy[file.agent]]
		for range len(spawnedBy) {
			if from.agent == "" {
				break
			}
			from = issued[spawnedBy[from.agent]]
		}
		for at := range file.events {
			file.events[at].Turn = from.turn
		}
		events = append(events, file.events...)
	}
	slices.SortStableFunc(events, func(a, b session.Event) int { return a.At.Compare(b.At) })
	weights, window := in.Weights, in.LeadWindow
	in.Weights = func(model string) drain.Weights { return weights(anthropicWire + "/" + model) }
	in.LeadWindow = func(model string) int { return window(anthropicWire + "/" + model) }
	return drain.Read(id, events, in), nil
}

func readFile(path, agent, firstTurn string) (*transcript, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	t := &transcript{agent: agent, turn: firstTurn, requests: map[string]*request{}, issued: map[string]issuer{}}
	reader := bufio.NewReader(file)
	for {
		line, err := reader.ReadBytes('\n')
		var r row
		if len(line) > 0 && json.Unmarshal(line, &r) == nil {
			t.row(r)
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	for _, asked := range t.requests {
		t.events[asked.eventIndex].Body, _ = json.Marshal(asked)
	}
	return t, nil
}

func (t *transcript) add(r row, kind session.EventKind, call string, body any) {
	encoded, _ := json.Marshal(body)
	t.events = append(t.events, session.Event{ID: r.UUID, At: r.Timestamp, Turn: t.turn, Agent: t.agent, Call: call, Kind: kind, Body: encoded})
}

func (t *transcript) wake(r row, source llm.Source) {
	t.add(r, session.EventMessage, "", session.MessageBody{Role: session.RoleUser, Content: string(r.Message.Content), From: &session.Origin{Sub: string(source)}})
}

func (t *transcript) row(r row) {
	switch r.Type {
	case "assistant":
		t.assistant(r)
	case "user":
		t.user(r)
	case "attachment":
		if r.Attachment.Type == "queued_command" {
			t.wake(r, r.Attachment.Origin.source())
		}
	case "system":
		switch r.Subtype {
		case "scheduled_task_fire":
			t.wake(r, llm.SourceCronFire)
		case "compact_boundary":
			type before struct {
				Tokens int `json:"tokens_before"`
			}
			t.add(r, session.EventCompaction, "", struct {
				Compaction before `json:"compaction"`
			}{before{r.CompactMetadata.PreTokens}})
		}
	}
}

func (t *transcript) assistant(r row) {
	if r.Message.Model == syntheticModel || r.Message.ID == "" {
		return
	}
	asked := t.requests[r.Message.ID]
	if asked == nil {
		asked = &request{eventIndex: len(t.events)}
		t.requests[r.Message.ID] = asked
		t.events = append(t.events, session.Event{ID: r.Message.ID, At: r.Timestamp, Turn: t.turn, Agent: t.agent, Kind: session.EventRequest})
	}
	spent := r.Message.Usage
	asked.Model, asked.Prompt, asked.Completion, asked.Read, asked.Write, asked.Write5m, asked.Write1h =
		r.Message.Model, spent.Input, spent.Output, spent.Read, spent.Write, spent.Split.Write5m, spent.Split.Write1h
	var blocks []block
	if json.Unmarshal(r.Message.Content, &blocks) != nil {
		return
	}
	for _, used := range blocks {
		if used.Type != "tool_use" {
			continue
		}
		asked.ToolCalls = append(asked.ToolCalls, used.ID)
		t.issued[used.ID] = issuer{agent: t.agent, turn: t.turn}
		args := struct {
			Command string `json:"command,omitempty"`
			Path    string `json:"path,omitempty"`
		}{used.Input.Command, used.Input.FilePath}
		encoded, _ := json.Marshal(args)
		t.add(r, session.EventToolCall, used.ID, session.CallBody{Tool: strings.ToLower(used.Name), Args: encoded})
	}
}

func (t *transcript) user(r row) {
	var blocks []block
	if json.Unmarshal(r.Message.Content, &blocks) == nil && slices.ContainsFunc(blocks, func(b block) bool { return b.Type == "tool_result" }) {
		for _, result := range blocks {
			if result.Type != "tool_result" {
				continue
			}
			outcome := ""
			if result.IsError {
				outcome = session.ToolOutcomeFailed
			}
			t.add(r, session.EventToolResult, result.ToolUseID, struct {
				Outcome string `json:"tool_outcome,omitempty"`
				Bytes   int    `json:"result_bytes"`
			}{outcome, len(result.Content)})
		}
		return
	}
	switch {
	case t.agent != "" && len(t.requests) == 0:
		t.wake(r, llm.SourceBrief)
	case r.ScheduledFireID != "":
		t.wake(r, llm.SourceCronFire)
	case r.Origin.source() == llm.SourceTyped:
		t.turn = r.UUID
		if t.agent == "" {
			t.add(r, session.EventTurnStart, "", session.TurnStart{Wire: anthropicWire})
		}
		fallthrough
	default:
		t.wake(r, r.Origin.source())
	}
}
