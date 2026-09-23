package schemas

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

type StepUsage struct {
	Index            int
	PromptTokens     int
	CacheReadTokens  int
	CacheWriteTokens int
	CompletionTokens int
	ToolCalls        []string
}

func newStepUsage(index, promptTokens, cacheReadTokens, cacheWriteTokens, completionTokens int, toolCalls []string) StepUsage {
	return StepUsage{
		Index:            index,
		PromptTokens:     promptTokens,
		CacheReadTokens:  cacheReadTokens,
		CacheWriteTokens: cacheWriteTokens,
		CompletionTokens: completionTokens,
		ToolCalls:        toolCalls,
	}
}

type TurnUsage struct {
	ID    string
	Steps []StepUsage
}

func (t TurnUsage) FirstCacheWrite() (int, bool) {
	for _, step := range t.Steps {
		if step.CacheWriteTokens > 0 {
			return step.CacheWriteTokens, true
		}
	}
	return 0, false
}

func (t TurnUsage) SumFreshInput() int { return t.sum(func(s StepUsage) int { return s.PromptTokens }) }

func (t TurnUsage) SumCacheRead() int {
	return t.sum(func(s StepUsage) int { return s.CacheReadTokens })
}

func (t TurnUsage) SumCacheWrite() int {
	return t.sum(func(s StepUsage) int { return s.CacheWriteTokens })
}

func (t TurnUsage) sum(field func(StepUsage) int) int {
	total := 0
	for _, step := range t.Steps {
		total += field(step)
	}
	return total
}

func (t TurnUsage) CalledToolNames() []string {
	seen := map[string]bool{}
	var names []string
	for _, step := range t.Steps {
		for _, name := range step.ToolCalls {
			if !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
	}
	sort.Strings(names)
	return names
}

type SkippedSession struct {
	Path   string
	Reason string
}

func ReadSessions(dir string) (usable []TurnUsage, skipped []SkippedSession, err error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}
	for _, entry := range entries {
		name := entry.Name()
		full := filepath.Join(dir, name)
		if entry.IsDir() {
			turn, reason := readSessionDir(full)
			if reason != "" {
				skipped = append(skipped, SkippedSession{Path: name, Reason: reason})
				continue
			}
			usable = append(usable, turn)
			continue
		}
		if filepath.Ext(name) != ".json" {
			skipped = append(skipped, SkippedSession{Path: name, Reason: "not a session file"})
			continue
		}
		turn, reason := readSessionFile(full)
		if reason != "" {
			skipped = append(skipped, SkippedSession{Path: name, Reason: reason})
			continue
		}
		usable = append(usable, turn)
	}
	sort.Slice(usable, func(i, j int) bool { return usable[i].ID < usable[j].ID })
	return usable, skipped, nil
}

type bodyStep struct {
	Index            int `json:"index"`
	PromptTokens     int `json:"prompt_tokens"`
	CacheReadTokens  int `json:"cache_read_tokens"`
	CacheWriteTokens int `json:"cache_write_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	ToolCalls        []struct {
		Tool string `json:"tool"`
	} `json:"tool_calls"`
}

type bodyLine struct {
	Kind string   `json:"kind"`
	Body bodyStep `json:"body"`
}

func readSessionDir(dir string) (TurnUsage, string) {
	headerPath := filepath.Join(dir, "header.json")
	header, err := os.ReadFile(headerPath)
	if err != nil {
		return TurnUsage{}, "no header.json: " + err.Error()
	}
	var head struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(header, &head); err != nil {
		return TurnUsage{}, "header.json does not parse: " + err.Error()
	}
	bodyPath := filepath.Join(dir, "body.jsonl")
	file, err := os.Open(bodyPath)
	if err != nil {
		return TurnUsage{}, "no body.jsonl: " + err.Error()
	}
	defer func() { _ = file.Close() }()

	turn := TurnUsage{ID: head.ID}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		var line bodyLine
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			continue
		}
		if line.Kind != "step" {
			continue
		}
		names := make([]string, len(line.Body.ToolCalls))
		for i, call := range line.Body.ToolCalls {
			names[i] = call.Tool
		}
		turn.Steps = append(turn.Steps, newStepUsage(line.Body.Index, line.Body.PromptTokens,
			line.Body.CacheReadTokens, line.Body.CacheWriteTokens, line.Body.CompletionTokens, names))
	}
	if len(turn.Steps) == 0 {
		return TurnUsage{}, "body.jsonl carries no step records"
	}
	return turn, ""
}

type singleFileStep struct {
	Index            int `json:"Index"`
	PromptTokens     int `json:"PromptTokens"`
	CacheReadTokens  int `json:"CacheReadTokens"`
	CacheWriteTokens int `json:"CacheWriteTokens"`
	CompletionTokens int `json:"CompletionTokens"`
	ToolCalls        []struct {
		Tool string `json:"Tool"`
	} `json:"ToolCalls"`
}

func readSessionFile(path string) (TurnUsage, string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return TurnUsage{}, err.Error()
	}
	var parsed struct {
		ID    string           `json:"ID"`
		Steps []singleFileStep `json:"Steps"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return TurnUsage{}, "does not parse as a single-file session: " + err.Error()
	}
	if len(parsed.Steps) == 0 {
		return TurnUsage{}, "carries no steps"
	}
	turn := TurnUsage{ID: parsed.ID}
	for _, raw := range parsed.Steps {
		names := make([]string, len(raw.ToolCalls))
		for i, call := range raw.ToolCalls {
			names[i] = call.Tool
		}
		turn.Steps = append(turn.Steps, newStepUsage(raw.Index, raw.PromptTokens,
			raw.CacheReadTokens, raw.CacheWriteTokens, raw.CompletionTokens, names))
	}
	if turn.SumCacheRead()+turn.SumCacheWrite() == 0 {
		return TurnUsage{}, "carries no cache token fields, an older recording predating cache accounting"
	}
	return turn, ""
}
