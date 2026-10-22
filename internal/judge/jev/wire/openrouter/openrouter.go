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
	Alias           = "~typesafe/jev-latest"
	ListingVariable = "TOFU_JEV_LISTING_URL"

	listingURL        = "https://openrouter.ai/api/v1/models"
	shipped           = "typesafe/jev-1.13"
	vendorPrefix      = "typesafe/"
	releasePattern    = "jev-%d.%d"
	modelPattern      = vendorPrefix + releasePattern
	endpointSeparator = " | "
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
	return listed(pin.Model), nil
}

func listed(model string) string {
	var major, minor int
	if _, err := fmt.Sscanf(strings.TrimPrefix(model, vendorPrefix), releasePattern, &major, &minor); err != nil {
		return model
	}
	return fmt.Sprintf(modelPattern, major, minor)
}

func New(config Config) (*Wire, error) {
	model, err := Model()
	if err != nil {
		return nil, err
	}
	return wire.New(wire.Spec{Name: Name, Endpoint: Endpoint, Alias: model, FailOp: "openrouter.New"}, config)
}

func CheckPin(ctx context.Context, client *transport.Client) (jev.PinCheck, jev.Pin) {
	pin, err := jev.LoadPin()
	if err != nil {
		return jev.PinCheck{From: Alias, Skipped: "the pin is unreadable, it stays", Error: err.Error()}, jev.Pin{}
	}
	check := jev.PinCheck{From: cmp.Or(pin.Model, Alias)}
	var major, minor int
	if _, err := fmt.Sscanf(listed(cmp.Or(pin.Model, shipped)), modelPattern, &major, &minor); err != nil {
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
