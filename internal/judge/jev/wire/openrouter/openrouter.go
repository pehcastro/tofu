package openrouter

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"tofu/internal/judge/jev"
	"tofu/internal/judge/jev/wire"
	"tofu/internal/transport"
)

const (
	Name            = "openrouter"
	Endpoint        = "https://openrouter.ai/api/alpha/decisions"
	ListingVariable = "TOFU_JEV_LISTING_URL"
	RequestIDHeader = "X-Generation-Id"

	listingURL        = "https://openrouter.ai/api/v1/models"
	vendorPrefix      = "typesafe/"
	releasePattern    = "jev-%d.%d"
	modelPattern      = vendorPrefix + releasePattern
	endpointSeparator = " | "
)

type (
	Config = wire.Config
	Wire   = wire.Wire
)

func Model(bound string) (string, error) {
	pin, err := jev.LoadPin()
	if err != nil || pin.Model == "" {
		return bound, err
	}
	return listed(pin.Model), nil
}

func listed(model string) string {
	var major, minor int
	if _, err := fmt.Sscanf(strings.TrimPrefix(model, vendorPrefix), releasePattern, &major, &minor); err != nil {
		return model
	}
	return fmt.Sprintf(modelPattern, major, minor)
}

func New(bound string, config Config) (*Wire, error) {
	model, err := Model(bound)
	if err != nil {
		return nil, err
	}
	return on(model, config)
}

func on(model string, config Config) (*Wire, error) {
	return wire.New(wire.Spec{Name: Name, Endpoint: Endpoint, Model: model, FailOp: "openrouter.New", RequestIDHeader: RequestIDHeader}, config)
}

func CheckPin(ctx context.Context, bound string, config Config, client *transport.Client) (jev.PinCheck, jev.Pin) {
	pin, err := jev.LoadPin()
	if err != nil {
		return jev.PinCheck{From: bound, Skipped: "the pin is unreadable, it stays", Error: err.Error()}, jev.Pin{}
	}
	check := jev.PinCheck{From: cmp.Or(pin.Model, bound)}
	seed := pin.Model
	if seed == "" {
		answering, err := on(bound, config)
		if err == nil {
			seed, err = wire.AnsweringBuild(ctx, answering)
		}
		if err != nil {
			check.Skipped, check.Error = "nothing is pinned and "+bound+" did not answer, so there is no build to look past", err.Error()
			return check, jev.Pin{}
		}
	}
	var major, minor int
	if _, err := fmt.Sscanf(listed(seed), modelPattern, &major, &minor); err != nil {
		check.Skipped, check.Error = "the pin names no jev version, it stays at "+check.From, err.Error()
		return check, jev.Pin{}
	}
	var newest jev.Pin
	for _, next := range [][2]int{{major, minor}, {major, minor + 1}, {major + 1, 0}} {
		model := fmt.Sprintf(modelPattern, next[0], next[1])
		build, err := listedBuild(ctx, client, model)
		if err != nil {
			check.Skipped, check.Error = "unreachable, the pin stays at "+check.From, err.Error()
			return check, jev.Pin{}
		}
		if build != "" {
			newest = jev.Pin{Model: model, Build: build}
		}
	}
	check.Found = newest.Build
	switch {
	case newest.Model == "":
		check.Skipped = "openrouter lists no jev build, the pin stays at " + check.From
	case newest.Model != listed(pin.Model):
		check.Newer = newest.Model
	}
	return check, newest
}

func Serves(pin jev.Pin, build string) bool {
	version := strings.TrimPrefix(listed(pin.Model), vendorPrefix)
	return strings.Contains(build, version+"-") || strings.Contains(build, version+".")
}

func listedBuild(ctx context.Context, client *transport.Client, model string) (string, error) {
	response, err := client.Do(ctx, transport.Request{
		Method: http.MethodGet,
		URL:    cmp.Or(strings.TrimSpace(os.Getenv(ListingVariable)), listingURL) + "/" + model + "/endpoints",
		Header: http.Header{"Accept": []string{"application/json"}},
	})
	var refused *transport.Error
	if errors.As(err, &refused) && refused.Status == http.StatusNotFound {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var listing struct {
		Data struct {
			Endpoints []struct {
				Name string `json:"name"`
			} `json:"endpoints"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body, &listing); err != nil {
		return "", fmt.Errorf("the openrouter listing for %s is not JSON: %w", model, err)
	}
	for _, endpoint := range listing.Data.Endpoints {
		if _, build, _ := strings.Cut(endpoint.Name, endpointSeparator); strings.HasPrefix(build, model+"-") {
			return build, nil
		}
	}
	return "", fmt.Errorf("openrouter lists %s with no dated build", model)
}
