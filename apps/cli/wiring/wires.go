package wiring

import (
	"tofu/internal/models/llm/catalog"
	"tofu/internal/models/llm/wire/openai"
)

const (
	WireSubscription = "anthropic"
	WireCodex        = "codex"
	WireKey          = openai.Name
	WireMeta         = string(catalog.Meta)
)

func RunWires() []string { return []string{WireSubscription, WireCodex, WireKey} }
