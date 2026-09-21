package session

import (
	"encoding/json"
	"fmt"
	"strconv"
)

type ReasoningSource string

const (
	ReasoningFromAssistantText ReasoningSource = "assistant_text"
	ReasoningNoneSent          ReasoningSource = "none, the step carried no assistant text and no wire here sends reasoning blocks"
	ReasoningNotRecorded       ReasoningSource = "not recorded, the setting is off"
)

const (
	readTool          = "read"
	artifactFetchTool = "artifact_fetch"
)

type Read struct {
	Step            int             `json:"step"`
	Tool            string          `json:"tool"`
	Source          string          `json:"source"`
	Span            string          `json:"span,omitempty"`
	Bytes           int             `json:"bytes"`
	Artifact        string          `json:"artifact,omitempty"`
	Hash            string          `json:"hash,omitempty"`
	Error           string          `json:"error,omitempty"`
	Reasoning       string          `json:"reasoning,omitempty"`
	ReasoningSource ReasoningSource `json:"reasoning_source"`
}

type Reads struct {
	Session    string `json:"session"`
	Reads      []Read `json:"reads"`
	Unrecorded int    `json:"unrecorded"`
}

type stepCall struct {
	Tool         string          `json:"tool"`
	Args         json.RawMessage `json:"args"`
	ResultBytes  int             `json:"result_bytes"`
	ResultHash   string          `json:"result_hash"`
	ResultHandle string          `json:"result_handle"`
	Error        string          `json:"error"`
}

type stepRow struct {
	Index         int        `json:"index"`
	AssistantText string     `json:"assistant_text"`
	ToolCalls     []stepCall `json:"tool_calls"`
}

type readArgs struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Handle    string `json:"handle"`
	Offset    int    `json:"offset"`
	Length    int    `json:"length"`
}

func readsInStep(step stepRow, reasoning bool) []Read {
	var reads []Read
	for _, call := range step.ToolCalls {
		if call.Tool != readTool && call.Tool != artifactFetchTool {
			continue
		}
		var args readArgs
		if err := json.Unmarshal(call.Args, &args); err != nil {
			args = readArgs{}
		}
		source, span := args.Path, lineSpan(args.StartLine, args.EndLine)
		if call.Tool == artifactFetchTool {
			source, span = args.Handle, byteSpan(args.Offset, args.Length)
		}
		read := Read{
			Step:            step.Index,
			Tool:            call.Tool,
			Source:          source,
			Span:            span,
			Bytes:           call.ResultBytes,
			Artifact:        call.ResultHandle,
			Hash:            call.ResultHash,
			Error:           call.Error,
			ReasoningSource: ReasoningNotRecorded,
		}
		if reasoning {
			read.Reasoning = step.AssistantText
			read.ReasoningSource = ReasoningFromAssistantText
			if step.AssistantText == "" {
				read.ReasoningSource = ReasoningNoneSent
			}
		}
		reads = append(reads, read)
	}
	return reads
}

func lineSpan(start, end int) string {
	if start <= 0 && end <= 0 {
		return ""
	}
	last := "end"
	if end > 0 {
		last = strconv.Itoa(end)
	}
	return "lines " + strconv.Itoa(max(start, 1)) + "-" + last
}

func byteSpan(offset, length int) string {
	if length <= 0 {
		return ""
	}
	return fmt.Sprintf("bytes %d-%d", offset, offset+length-1)
}

func (s Settings) withReads(events []Event) ([]Event, error) {
	if !s.RecordReads {
		return events, nil
	}
	recorded := make([]Event, 0, len(events))
	for _, event := range events {
		recorded = append(recorded, event)
		if event.Kind != EventStep {
			continue
		}
		var step stepRow
		if err := json.Unmarshal(event.Body, &step); err != nil {
			return nil, fmt.Errorf("session: a step being recorded does not read back: %w", err)
		}
		for _, read := range readsInStep(step, s.RecordReasoning) {
			body, err := json.Marshal(read)
			if err != nil {
				return nil, err
			}
			recorded = append(recorded, Event{ID: NewEventID(), Parent: event.ID, Author: event.Author, Attempt: event.Attempt, Kind: EventRead, Body: body})
		}
	}
	return recorded, nil
}

func (s *Store) ReadsOf(id string) (Reads, error) {
	events, err := s.Body(id)
	if err != nil {
		return Reads{}, err
	}
	reads := Reads{Session: id}
	happened := 0
	for _, event := range events {
		switch event.Kind {
		case EventRead:
			var read Read
			if err := json.Unmarshal(event.Body, &read); err != nil {
				return Reads{}, fmt.Errorf("session: a read of %s does not parse: %w", id, err)
			}
			reads.Reads = append(reads.Reads, read)
		case EventStep:
			var step stepRow
			if err := json.Unmarshal(event.Body, &step); err != nil {
				return Reads{}, fmt.Errorf("session: a step of %s does not parse: %w", id, err)
			}
			happened += len(readsInStep(step, false))
		}
	}
	reads.Unrecorded = max(happened-len(reads.Reads), 0)
	return reads, nil
}
