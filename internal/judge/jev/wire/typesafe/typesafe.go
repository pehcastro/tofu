package typesafe

import "tofu/internal/judge/jev/wire"

const (
	Name            = "typesafe"
	Endpoint        = "https://api.typesafe.ai/v1/systemone"
	Alias           = "jev-latest"
	RequestIDHeader = "x-typesafe-request-id"

	DollarsPerInputToken = 42.0 / 1e9
)

type (
	Config = wire.Config
	Wire   = wire.Wire
)

func New(config Config) (*Wire, error) {
	return wire.New(wire.Spec{
		Name:            Name,
		Endpoint:        Endpoint,
		Alias:           Alias,
		FailOp:          "typesafe.New",
		RequestIDHeader: RequestIDHeader,
	}, config)
}
