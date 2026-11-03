package turn

import (
	"unicode/utf8"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/tools/contract"
)

type ResultOutcome = contract.ResultOutcome

const (
	ResultUnset     = contract.ResultUnset
	ResultSucceeded = contract.ResultSucceeded
	ResultFailed    = contract.ResultFailed
	ResultAborted   = contract.ResultAborted
)

type Result = contract.Result

type Tool = contract.Tool

type Root = contract.Root

func NewRoot(dir string) (Root, error) {
	return contract.NewRoot(dir)
}

func within(base, path string) (string, bool) {
	return contract.Within(base, path)
}

func runeSafeHead(s string, n int) string {
	if n >= len(s) {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func runeSafeTail(s string, n int) string {
	if n >= len(s) {
		return s
	}
	start := len(s) - n
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	return s[start:]
}

type Registry struct {
	tools  []Tool
	byName map[string]Tool
}

func NewRegistry(tools ...Tool) Registry {
	byName := make(map[string]Tool, len(tools))
	for _, tool := range tools {
		byName[tool.Name()] = tool
	}
	return Registry{tools: tools, byName: byName}
}

func readOnly(name string) bool {
	switch name {
	case "read", "glob", "search", "symbols", "project_report", "artifact_fetch", "fetch", "web_search", "github_pr_diff", "typecheck", "test", "subagents", ScratchPathToolName, ScratchListToolName, ScratchReadToolName:
		return true
	}
	return false
}

func (r Registry) parallelPrefix(calls []llm.ToolCall) int {
	if spawner, spawning := r.byName["spawn"].(*SpawnTool); spawning && calls[0].Name == "spawn" {
		return spawner.disjointPrefix(calls)
	}
	width := min(len(calls), konst.TurnParallelToolCalls)
	for i, call := range calls[:width] {
		if _, known := r.byName[call.Name]; !known || !readOnly(call.Name) {
			return i
		}
	}
	return width
}

func (r Registry) Definitions() []llm.Tool {
	defs := make([]llm.Tool, len(r.tools))
	for i, tool := range r.tools {
		defs[i] = tool.Definition()
	}
	return defs
}
