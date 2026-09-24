package openrouter

import "tofu/internal/judge/jev/wire"

const (
	Name     = "openrouter"
	Endpoint = "https://openrouter.ai/api/alpha/decisions"
	Alias    = "~typesafe/jev-latest"
)

type (
	Config = wire.Config
	Wire   = wire.Wire
)

func New(config Config) (*Wire, error) {
	return wire.New(wire.Spec{Name: Name, Endpoint: Endpoint, Alias: Alias, FailOp: "openrouter.New"}, config)
}
