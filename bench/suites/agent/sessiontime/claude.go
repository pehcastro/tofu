package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type claudeLine struct {
	Type      string    `json:"type"`
	UUID      string    `json:"uuid"`
	Timestamp time.Time `json:"timestamp"`
	IsMeta    bool      `json:"isMeta"`
	Message   struct {
		ID      string          `json:"id"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type claudeBlock struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	Name      string `json:"name"`
	ToolUseID string `json:"tool_use_id"`
	Input     struct {
		Command string `json:"command"`
	} `json:"input"`
}

type claudeTool struct {
	name string
	kind bucket
}

func readClaude(root string, w window) (*tally, error) {
	mains, err := filepath.Glob(filepath.Join(root, "*", "*.jsonl"))
	if err != nil {
		return nil, err
	}
	subs, err := filepath.Glob(filepath.Join(root, "*", "*", "subagents", "*.jsonl"))
	if err != nil {
		return nil, err
	}
	t := newTally()
	seenLines, seenRequests := map[string]bool{}, map[string]bool{}
	for _, path := range append(mains, subs...) {
		used, err := t.transcript(path, w, seenLines, seenRequests)
		if err != nil {
			return nil, err
		}
		switch {
		case !used:
		case strings.Contains(filepath.ToSlash(path), "/subagents/"):
			t.subTranscripts++
		default:
			t.sessions++
		}
	}
	return t, nil
}

func (t *tally) transcript(path string, w window, seenLines, seenRequests map[string]bool) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer func() { _ = file.Close() }()
	reader := bufio.NewReader(file)
	tools := map[string]claudeTool{}
	var previous time.Time
	used := false
	for {
		raw, readErr := reader.ReadBytes('\n')
		var line claudeLine
		if json.Unmarshal(raw, &line) == nil && (line.Type == "user" || line.Type == "assistant") && !line.Timestamp.IsZero() {
			kind, tool := lineKind(line, tools)
			gap := line.Timestamp.Sub(previous)
			if !previous.IsZero() && gap > 0 && !seenLines[line.UUID] && w.holds(line.Timestamp) {
				if gap > idleGap {
					kind, tool = bucketIdle, ""
				}
				t.add(kind, tool, gap, line.Timestamp)
				used = true
				if line.Type == "assistant" && !seenRequests[line.Message.ID] {
					seenRequests[line.Message.ID] = true
					t.requests++
				}
				if line.Type == "user" && tool != "" {
					t.calls++
				}
			}
			seenLines[line.UUID] = line.UUID != ""
			previous = line.Timestamp
		}
		if errors.Is(readErr, io.EOF) {
			return used, nil
		}
		if readErr != nil {
			return used, readErr
		}
	}
}

func lineKind(line claudeLine, tools map[string]claudeTool) (bucket, string) {
	var blocks []claudeBlock
	_ = json.Unmarshal(line.Message.Content, &blocks)
	if line.Type == "assistant" {
		for _, block := range blocks {
			if block.Type != "tool_use" {
				continue
			}
			name := block.Name
			if strings.HasPrefix(name, "mcp__") {
				name = "mcp"
			}
			kind := toolBucket(name)
			if name == "Bash" || name == "PowerShell" {
				kind = commandBucket(block.Input.Command)
			}
			tools[block.ID] = claudeTool{name: name, kind: kind}
		}
		return bucketAPI, ""
	}
	for _, block := range blocks {
		if block.Type == "tool_result" {
			used := tools[block.ToolUseID]
			if used.name == "" {
				return bucketOtherTool, "unmatched result"
			}
			return used.kind, used.name
		}
	}
	var text string
	if line.IsMeta || json.Unmarshal(line.Message.Content, &text) == nil && strings.HasPrefix(text, "<task-notification>") {
		return bucketSubAgent, ""
	}
	return bucketPerson, ""
}
