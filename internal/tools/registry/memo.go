package registry

import (
	"context"
	"encoding/json"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"tofu/internal/models/llm/core"
	"tofu/internal/tools/contract"
)

func SideEffectFree(name string) bool {
	switch name {
	case "read", "glob", "search", "symbols", "project_report", "artifact_fetch", "fetch", "web_search", "github_pr_diff":
		return true
	}
	return false
}

func readOnlyCall(name string, raw json.RawMessage) bool {
	var args struct {
		Mode   string `json:"mode"`
		Action string `json:"action"`
	}
	if SideEffectFree(name) {
		return true
	}
	if json.Unmarshal(raw, &args) != nil {
		return false
	}
	switch name {
	case "deps":
		return args.Mode == "why" || args.Mode == "outdated"
	case "trail":
		return args.Mode == "" || args.Mode == "find" || args.Mode == "owner_of"
	case "checkpoint":
		return args.Action == "diff"
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
	answers map[string]contract.Result
}

func NewMemo() *Memo {
	return &Memo{answers: map[string]contract.Result{}}
}

func (m *Memo) Wrap(list []contract.Tool) []contract.Tool {
	wrapped := make([]contract.Tool, len(list))
	for i, tool := range list {
		wrapped[i] = memoTool{memo: m, tool: tool}
	}
	return wrapped
}

func (m *Memo) recall(key string) (contract.Result, bool) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	held, ok := m.answers[key]
	return held, ok
}

func (m *Memo) keep(key string, result contract.Result) {
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
	tool contract.Tool
}

func (t memoTool) Name() string { return t.tool.Name() }

func (t memoTool) Definition() core.Tool { return t.tool.Definition() }

func (t memoTool) Uncached() contract.Tool { return t.tool }

func (t memoTool) Run(ctx context.Context, raw json.RawMessage) (contract.Result, error) {
	if t.tool.Name() == "read" {
		return t.tool.Run(ctx, raw)
	}
	if !readOnlyCall(t.tool.Name(), raw) {
		result, err := t.tool.Run(ctx, raw)
		t.memo.forget()
		return result, err
	}
	key, keyed := CallKey(t.tool.Name(), raw)
	if !keyed {
		return t.tool.Run(ctx, raw)
	}
	if held, ok := t.memo.recall(key); ok {
		held.Repeat = true
		return held, nil
	}
	result, err := t.tool.Run(ctx, raw)
	if err == nil && len(result.Images) == 0 {
		t.memo.keep(key, result)
	}
	return result, err
}
