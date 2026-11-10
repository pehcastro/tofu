package main

import (
	"bytes"
	"cmp"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/bench/kit/transcript"
)

const idleCap = 300 * time.Second

var toolKinds = []string{"write", "edit", "read", "test", "build", "shell", "talk", "other"}

type tokens struct {
	FreshIn    int `json:"fresh_in"`
	CacheRead  int `json:"cache_read"`
	CacheWrite int `json:"cache_write"`
	Out        int `json:"out"`
}

type kindTotal struct {
	Kind   string `json:"kind"`
	Calls  int    `json:"calls"`
	MS     int64  `json:"ms"`
	Failed int    `json:"failed"`
}

type writes struct {
	FilesCreated int `json:"files_created"`
	FilesEdited  int `json:"files_edited"`
	LinesAdded   int `json:"lines_added"`
	LinesRemoved int `json:"lines_removed"`
}

type measured struct {
	Session    string      `json:"session"`
	Version    string      `json:"version"`
	Model      string      `json:"model"`
	Models     []string    `json:"models"`
	Credential string      `json:"credential"`
	ActiveMS   int64       `json:"active_ms"`
	ModelMS    int64       `json:"model_ms"`
	Requests   int         `json:"requests"`
	Tokens     tokens      `json:"tokens"`
	Calls      []kindTotal `json:"calls"`
	Writes     writes      `json:"writes"`
}

type stamped struct {
	ms   int64
	text string
}

type streamEvent struct {
	Type         string `json:"type"`
	Subtype      string `json:"subtype"`
	APIKeySource string `json:"apiKeySource"`
	SessionID    string `json:"session_id"`
	Version      string `json:"claude_code_version"`
	Message      struct {
		ID      string          `json:"id"`
		Model   string          `json:"model"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
	ToolUseResult json.RawMessage `json:"tool_use_result"`
	ModelUsage    map[string]struct {
		InputTokens              int `json:"inputTokens"`
		OutputTokens             int `json:"outputTokens"`
		CacheReadInputTokens     int `json:"cacheReadInputTokens"`
		CacheCreationInputTokens int `json:"cacheCreationInputTokens"`
	} `json:"modelUsage"`
}

type contentBlock struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	Name      string `json:"name"`
	ToolUseID string `json:"tool_use_id"`
	IsError   bool   `json:"is_error"`
	Input     struct {
		Command string `json:"command"`
	} `json:"input"`
}

type fileResult struct {
	Type            string `json:"type"`
	FilePath        string `json:"filePath"`
	Content         string `json:"content"`
	StructuredPatch []struct {
		Lines []string `json:"lines"`
	} `json:"structuredPatch"`
}

type toolUse struct {
	kind  string
	begun int64
}

type requestSpan struct {
	begun, ended int64
}

func toolKind(b contentBlock) string {
	switch b.Name {
	case "Write":
		return "write"
	case "Edit", "MultiEdit", "NotebookEdit":
		return "edit"
	case "Read", "Grep", "Glob":
		return "read"
	case "Agent", "SendMessage", "Task":
		return "talk"
	case "Bash", "PowerShell":
		switch transcript.CommandBucket(b.Input.Command) {
		case transcript.Test:
			return "test"
		case transcript.Build:
			return "build"
		}
		return "shell"
	}
	return "other"
}

func measureStream(lines []stamped) measured {
	m := measured{}
	for _, kind := range toolKinds {
		m.Calls = append(m.Calls, kindTotal{Kind: kind})
	}
	uses, requests := map[string]toolUse{}, map[string]*requestSpan{}
	created, edited := map[string]bool{}, map[string]bool{}
	var lastOther int64
	for _, line := range lines {
		var e streamEvent
		if json.Unmarshal([]byte(line.text), &e) != nil {
			continue
		}
		var blocks []contentBlock
		_ = json.Unmarshal(e.Message.Content, &blocks)
		switch e.Type {
		case "system":
			if e.Subtype == "init" {
				m.Session, m.Version, m.Credential = e.SessionID, e.Version, "key, apiKeySource "+e.APIKeySource
				if e.APIKeySource == "none" {
					m.Credential = "subscription, apiKeySource none"
				}
			}
		case "assistant":
			if requests[e.Message.ID] == nil {
				requests[e.Message.ID] = &requestSpan{begun: lastOther}
			}
			requests[e.Message.ID].ended = line.ms
			m.Model = cmp.Or(m.Model, e.Message.Model)
			m.Models = seen(m.Models, e.Message.Model)
			for _, b := range blocks {
				if b.Type == "tool_use" {
					uses[b.ID] = toolUse{kind: toolKind(b), begun: line.ms}
				}
			}
			continue
		case "user":
			for _, b := range blocks {
				use, known := uses[b.ToolUseID]
				if b.Type != "tool_result" || !known {
					continue
				}
				total := &m.Calls[slices.Index(toolKinds, use.kind)]
				total.Calls++
				total.MS += line.ms - use.begun
				if b.IsError {
					total.Failed++
				}
				if use.kind == "write" || use.kind == "edit" {
					countWrite(&m.Writes, e.ToolUseResult, created, edited)
				}
			}
		case "result":
			for model, u := range e.ModelUsage {
				m.Models = seen(m.Models, model)
				m.Tokens.FreshIn += u.InputTokens
				m.Tokens.CacheRead += u.CacheReadInputTokens
				m.Tokens.CacheWrite += u.CacheCreationInputTokens
				m.Tokens.Out += u.OutputTokens
			}
		}
		lastOther = line.ms
	}
	for _, span := range requests {
		m.ModelMS += min(span.ended-span.begun, idleCap.Milliseconds())
	}
	m.Requests = len(requests)
	m.Writes.FilesCreated, m.Writes.FilesEdited = len(created), len(edited)
	slices.Sort(m.Models)
	return m
}

func seen(models []string, model string) []string {
	if model == "" || slices.Contains(models, model) {
		return models
	}
	return append(models, model)
}

func countWrite(w *writes, raw json.RawMessage, created, edited map[string]bool) {
	var result fileResult
	if json.Unmarshal(raw, &result) != nil {
		return
	}
	for _, hunk := range result.StructuredPatch {
		for _, line := range hunk.Lines {
			switch {
			case strings.HasPrefix(line, "+"):
				w.LinesAdded++
			case strings.HasPrefix(line, "-"):
				w.LinesRemoved++
			}
		}
	}
	if result.Type == "create" && len(result.StructuredPatch) == 0 {
		w.LinesAdded += strings.Count(result.Content, "\n") + 1
	}
	if result.FilePath == "" {
		return
	}
	if result.Type == "create" {
		created[result.FilePath] = true
		return
	}
	edited[result.FilePath] = true
}

func activeMS(body []byte) int64 {
	var times []time.Time
	for _, line := range bytes.Split(body, []byte("\n")) {
		var stamp struct {
			Timestamp time.Time `json:"timestamp"`
		}
		if json.Unmarshal(line, &stamp) == nil && !stamp.Timestamp.IsZero() {
			times = append(times, stamp.Timestamp)
		}
	}
	slices.SortFunc(times, time.Time.Compare)
	var active time.Duration
	for i := 1; i < len(times); i++ {
		active += min(times[i].Sub(times[i-1]), idleCap)
	}
	return active.Milliseconds()
}

func measureWork(work string) (measured, error) {
	raw, err := os.ReadFile(filepath.Join(work, "stdout.txt"))
	if err != nil {
		return measured{}, err
	}
	var lines []stamped
	for _, line := range strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n") {
		ms, text, _ := strings.Cut(line, "\t")
		at, _ := strconv.ParseInt(ms, 10, 64)
		lines = append(lines, stamped{ms: at, text: text})
	}
	m := measureStream(lines)
	body, err := os.ReadFile(filepath.Join(work, "transcript.jsonl"))
	if err != nil {
		return measured{}, err
	}
	m.ActiveMS = activeMS(body)
	return m, nil
}
