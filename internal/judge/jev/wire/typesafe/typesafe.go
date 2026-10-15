package typesafe

import (
	"strings"

	"tofu/internal/judge/jev"
	"tofu/internal/judge/jev/wire"
)

const (
	Name            = "typesafe"
	Endpoint        = "https://api.typesafe.ai/v1/systemone"
	Alias           = "jev-latest"
	RequestIDHeader = "x-typesafe-request-id"

	DollarsPerInputToken = 42.0 / 1e9

	openRouterVendor = "typesafe/"
	patchZero        = ".0"
)

type (
	Config = wire.Config
	Wire   = wire.Wire
)

func Model() (string, error) {
	pin, err := jev.LoadPin()
	if err != nil || pin.Model == "" {
		return Alias, err
	}
	return strings.TrimPrefix(pin.Model, openRouterVendor) + patchZero, nil
}

func New(config Config) (*Wire, error) {
	model, err := Model()
	if err != nil {
		return nil, err
	}
	return wire.New(wire.Spec{
		Name:            Name,
		Endpoint:        Endpoint,
		Alias:           model,
		FailOp:          "typesafe.New",
		RequestIDHeader: RequestIDHeader,
	}, config)
}
