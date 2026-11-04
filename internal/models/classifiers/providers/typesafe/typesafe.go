package typesafe

import (
	"cmp"
	"context"
	"fmt"
	"strings"

	"tofu/internal/models/classifiers/core"
	"tofu/internal/models/classifiers/providers"
)

const (
	Name            = "typesafe"
	Endpoint        = "https://api.typesafe.ai/v1/systemone"
	RequestIDHeader = "x-typesafe-request-id"

	DollarsPerInputToken = 42.0 / 1e9

	legacyVendor   = "typesafe/"
	releasePattern = "jev-%d.%d"
	patchZero      = ".0"
)

type (
	Config = providers.Config
	Wire   = providers.Wire
)

func Model(bound string) (string, error) {
	pin, err := core.LoadPin()
	if err != nil || pin.Model == "" {
		return bound, err
	}
	return pinned(pin), nil
}

func pinned(pin core.Pin) string {
	model := strings.TrimPrefix(pin.Model, legacyVendor)
	if strings.Count(model, ".") == 1 {
		return model + patchZero
	}
	return model
}

func New(bound string, config Config) (*Wire, error) {
	model, err := Model(bound)
	if err != nil {
		return nil, err
	}
	return on(model, config)
}

func on(model string, config Config) (*Wire, error) {
	return providers.New(providers.Spec{Name: Name, Endpoint: Endpoint, Model: model, FailOp: "typesafe.New", RequestIDHeader: RequestIDHeader, ListUSDPerInput: DollarsPerInputToken}, config)
}

func CheckPin(ctx context.Context, bound string, config Config) (core.PinCheck, core.Pin) {
	pin, err := core.LoadPin()
	if err != nil {
		return core.PinCheck{From: bound, Skipped: "the pin is unreadable, it stays", Error: err.Error()}, core.Pin{}
	}
	check := core.PinCheck{From: cmp.Or(pin.Model, bound)}
	latest, err := on(bound, config)
	if err != nil {
		check.Skipped, check.Error = "no TypeSafe key, the pin stays at "+check.From, err.Error()
		return check, core.Pin{}
	}
	build, err := providers.AnsweringBuild(ctx, latest)
	if err != nil {
		check.Skipped, check.Error = "typesafe did not answer, the pin stays at "+check.From, err.Error()
		return check, core.Pin{}
	}
	check.Found = build
	if pin.Model == "" || pinned(pin) != build {
		check.Newer = build
	}
	return check, core.Pin{Model: build, Build: build}
}

func Serves(pin core.Pin, build string) bool {
	var major, minor int
	if _, err := fmt.Sscanf(strings.TrimPrefix(pin.Model, legacyVendor), releasePattern, &major, &minor); err != nil {
		return false
	}
	release := fmt.Sprintf(releasePattern, major, minor)
	return strings.Contains(build, release+"-") || strings.Contains(build, release+".")
}
