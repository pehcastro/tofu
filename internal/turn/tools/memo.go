package tools

import (
	"context"
	"encoding/json"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"tofu/internal/llm"
	"tofu/internal/turn"
)

func SideEffectFree(name string) bool {
	switch name {
	case "read", "glob", "search", "symbols", "project_report", "artifact_fetch", "fetch", "web_search", "github_pr_diff":
		return true
	}
	return false
}

var pullRequestURL = regexp.MustCompile(`^https://github\.com/[^/]+/[^/]+/pull/(\d+)/?$`)

func pullRequestKey(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if match := pullRequestURL.FindStringSubmatch(trimmed); match != nil {
		return match[1]
	}
	return trimmed
}

func CallKey(name string, raw json.RawMessage) (string, bool) {
	args := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", false
		}
	}
	for key, value := range args {
		switch shape := value.(type) {
		case nil:
			delete(args, key)
		case bool:
			if !shape {
				delete(args, key)
			}
		case float64:
			if shape == 0 {
				delete(args, key)
			}
		case string:
			switch {
			case shape == "":
				delete(args, key)
			case strings.Contains(key, "path"):
				args[key] = path.Clean(filepath.ToSlash(shape))
			case name == "github_pr_diff" && key == "pr":
				args[key] = pullRequestKey(shape)
			}
		}
	}
	canonical, err := json.Marshal(args)
	if err != nil {
		return "", false
	}
	return name + " " + string(canonical), true
}

type Memo struct {
	mutex   sync.Mutex
	answers map[string]turn.Result
}

func NewMemo() *Memo {
	return &Memo{answers: map[string]turn.Result{}}
}

func (m *Memo) Wrap(list []turn.Tool) []turn.Tool {
	wrapped := make([]turn.Tool, len(list))
	for i, tool := range list {
		wrapped[i] = memoTool{memo: m, tool: tool}
	}
	return wrapped
}

func (m *Memo) recall(key string) (turn.Result, bool) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	held, ok := m.answers[key]
	return held, ok
}

func (m *Memo) keep(key string, result turn.Result) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.answers[key] = result
}

func (m *Memo) forget() {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	clear(m.answers)
}

type memoTool struct {
	memo *Memo
	tool turn.Tool
}

func (t memoTool) Name() string { return t.tool.Name() }

func (t memoTool) Definition() llm.Tool { return t.tool.Definition() }

func (t memoTool) Run(ctx context.Context, raw json.RawMessage) (turn.Result, error) {
	if !SideEffectFree(t.tool.Name()) {
		result, err := t.tool.Run(ctx, raw)
		t.memo.forget()
		return result, err
	}
	key, keyed := CallKey(t.tool.Name(), raw)
	if !keyed {
		return t.tool.Run(ctx, raw)
	}
	if held, ok := t.memo.recall(key); ok {
		held.Content = "cached: the same call earlier in this turn, and nothing written since, so it was not run again\n" + held.Content
		return held, nil
	}
	result, err := t.tool.Run(ctx, raw)
	if err == nil {
		t.memo.keep(key, result)
	}
	return result, err
}
