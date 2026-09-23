package schemas

import (
	"encoding/json"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/llm/wire/anthropic"
)

type ToolCost struct {
	Name   string
	Bytes  int
	Tokens int
}

type Whole struct {
	Tools      []ToolCost
	SumBytes   int
	SumTokens  int
	WireBytes  int
	WireTokens int
}

type wireTool struct {
	Name   string `json:"name"`
	Desc   string `json:"description,omitempty"`
	Schema any    `json:"input_schema"`
}

func toWire(defs []llm.Tool) []wireTool {
	out := make([]wireTool, len(defs))
	for i, d := range defs {
		out[i] = wireTool{Name: d.Name, Desc: d.Description, Schema: d.Parameters}
	}
	return out
}

func Measure(defs []llm.Tool) Whole {
	wired := toWire(defs)
	whole := Whole{Tools: make([]ToolCost, len(defs))}
	for i, w := range wired {
		encoded, _ := json.Marshal(w)
		bytes := len(encoded)
		whole.Tools[i] = ToolCost{Name: w.Name, Bytes: bytes, Tokens: bytes / konst.SearchBytesPerToken}
		whole.SumBytes += bytes
		whole.SumTokens += bytes / konst.SearchBytesPerToken
	}
	whole.WireBytes = wireDiffBytes(defs)
	whole.WireTokens = whole.WireBytes / konst.SearchBytesPerToken
	return whole
}

func wireDiffBytes(defs []llm.Tool) int {
	baseline := anthropic.Request{
		Model:    "claude-opus-5",
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
	}
	withTools := baseline
	withTools.Tools = defs

	encodedBaseline, err := baseline.Encode(true)
	if err != nil {
		return -1
	}
	encodedWithTools, err := withTools.Encode(true)
	if err != nil {
		return -1
	}
	return len(encodedWithTools) - len(encodedBaseline)
}
