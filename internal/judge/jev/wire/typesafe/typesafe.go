package typesafe

import (
	"cmp"
	"context"
	"fmt"
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

	legacyVendor   = "typesafe/"
	releasePattern = "jev-%d.%d"
	patchZero      = ".0"
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
	return pinned(pin), nil
}

func pinned(pin jev.Pin) string {
	model := strings.TrimPrefix(pin.Model, legacyVendor)
	if strings.Count(model, ".") == 1 {
		return model + patchZero
	}
	return model
}

func New(config Config) (*Wire, error) {
	model, err := Model()
	if err != nil {
		return nil, err
	}
	return on(model, config)
}

func on(model string, config Config) (*Wire, error) {
	return wire.New(wire.Spec{Name: Name, Endpoint: Endpoint, Alias: model, FailOp: "typesafe.New", RequestIDHeader: RequestIDHeader}, config)
}

func CheckPin(ctx context.Context, config Config) (jev.PinCheck, jev.Pin) {
	pin, err := jev.LoadPin()
	if err != nil {
		return jev.PinCheck{From: Alias, Skipped: "the pin is unreadable, it stays", Error: err.Error()}, jev.Pin{}
	}
	check := jev.PinCheck{From: cmp.Or(pin.Model, Alias)}
	latest, err := on(Alias, config)
	if err != nil {
		check.Skipped, check.Error = "no TypeSafe key, the pin stays at "+check.From, err.Error()
		return check, jev.Pin{}
	}
	client, err := jev.NewClient(jev.Config{Wire: latest})
	if err != nil {
		check.Skipped, check.Error = "the pin stays at "+check.From, err.Error()
		return check, jev.Pin{}
	}
	decision, err := client.Ask(ctx, jev.Request{
		State: "tofu asks which jev build answers " + Alias,
		Questions: []jev.Question{{ID: "answered", Kind: jev.QuestionNoul, Instructions: "is `state` a question about a build",
			True: "it asks about a build", False: "it asks about something else"}},
	})
	if err != nil {
		check.Skipped, check.Error = "typesafe did not answer, the pin stays at "+check.From, err.Error()
		return check, jev.Pin{}
	}
	check.Found = decision.Build
	if pin.Model == "" || pinned(pin) != decision.Build {
		check.Newer = decision.Build
	}
	return check, jev.Pin{Model: decision.Build, Build: decision.Build}
}

func Serves(pin jev.Pin, build string) bool {
	var major, minor int
	if _, err := fmt.Sscanf(strings.TrimPrefix(pin.Model, legacyVendor), releasePattern, &major, &minor); err != nil {
		return false
	}
	release := fmt.Sprintf(releasePattern, major, minor)
	return strings.Contains(build, release+"-") || strings.Contains(build, release+".")
}
