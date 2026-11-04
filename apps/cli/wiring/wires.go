package wiring

import (
	"tofu/internal/models/llm/catalog"
	"tofu/internal/models/llm/wire/openrouter"
)

const (
	WireSubscription = "anthropic"
	WireCodex        = "codex"
	WireKey          = openrouter.Name
	WireMeta         = string(catalog.Meta)
)

func RunWires() []string { return []string{WireSubscription, WireCodex, WireKey} }
