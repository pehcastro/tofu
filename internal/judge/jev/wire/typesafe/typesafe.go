package typesafe

import (
	"strings"
	"time"

	"tofu/internal/judge/jev"
	"tofu/internal/judge/jev/wire"
	"tofu/internal/konst"
	"tofu/internal/sys"
	"tofu/internal/transport"
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

func Judge() (*jev.Client, error) {
	key, err := jev.KeyFor(sys.CredentialFileName, jev.TypeSafeVariable)
	if err != nil {
		return nil, err
	}
	wire, err := New(Config{Key: key, Transport: transport.Config{
		AttemptTimeout: konst.JudgeTimeoutMillis * time.Millisecond,
		Retries:        konst.JudgeRetries,
		Backoff:        konst.JudgeBackoffMillis * time.Millisecond,
		Concurrency:    1,
	}})
	if err != nil {
		return nil, err
	}
	return jev.NewClient(jev.Config{Wire: wire})
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
