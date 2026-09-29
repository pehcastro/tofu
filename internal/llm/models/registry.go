package models

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/internal/transport"
	"tofu/library"
)

//go:generate go run ./data/snapshot anthropic openai meta

//go:embed data/models-dev.json
var shippedRegistry []byte

//go:embed data/models-dev.taken
var shippedRegistryTaken string

const (
	RegistryURL         = "https://models.dev/api.json"
	RegistryURLVariable = "TOFU_MODELS_REGISTRY_URL"
	pricesGlob          = modelsDir + "/" + pricesDir + "/*.yaml"
	datedSuffixDigits   = 8
	writtenFileMode     = 0o644
)

type Facts struct {
	ToolCalls bool     `json:"tool_calls"`
	Reasoning bool     `json:"reasoning"`
	Efforts   []string `json:"efforts,omitempty"`
	Images    bool     `json:"images"`
}

type Rates struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cache_read,omitempty"`
	CacheWrite float64 `json:"cache_write,omitempty"`
}

type Tier struct {
	Rates
	AboveTokens int `json:"above_tokens"`
}

type Price struct {
	Rates
	Tiers []Tier `json:"tiers,omitempty"`
	From  string `json:"from"`
	Taken string `json:"taken,omitempty"`
}

type Registry struct {
	From    string           `json:"from"`
	Windows map[string]int   `json:"windows"`
	Facts   map[string]Facts `json:"facts,omitempty"`
	Prices  map[string]Price `json:"prices,omitempty"`
}

type modelsDevCost struct {
	Rates
	Tiers []struct {
		Rates
		Tier struct {
			Type string `json:"type"`
			Size int    `json:"size"`
		} `json:"tier"`
	} `json:"tiers"`
}

func (c modelsDevCost) price(from string) (Price, bool) {
	price := Price{Rates: c.Rates, From: from}
	for _, tier := range c.Tiers {
		if tier.Tier.Type != "context" {
			return Price{}, false
		}
		price.Tiers = append(price.Tiers, Tier{Rates: tier.Rates, AboveTokens: tier.Tier.Size})
	}
	return price, true
}

func ParseRegistry(body []byte, from string) (Registry, error) {
	var payload map[string]struct {
		Models map[string]struct {
			Limit struct {
				Context int `json:"context"`
			} `json:"limit"`
			ToolCall         *bool `json:"tool_call"`
			Reasoning        bool  `json:"reasoning"`
			ReasoningOptions []struct {
				Type   string   `json:"type"`
				Values []string `json:"values"`
			} `json:"reasoning_options"`
			Modalities struct {
				Input []string `json:"input"`
			} `json:"modalities"`
			Cost *modelsDevCost `json:"cost"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return Registry{}, fmt.Errorf("models: %s is not the shape models.dev serves: %w", from, err)
	}
	registry := Registry{From: from, Windows: make(map[string]int), Facts: make(map[string]Facts), Prices: make(map[string]Price)}
	for provider, listed := range payload {
		for id, model := range listed.Models {
			slug := provider + "/" + id
			if model.Limit.Context > 0 {
				registry.Windows[slug] = model.Limit.Context
			}
			if model.Cost != nil {
				if price, priced := model.Cost.price(from); priced {
					registry.Prices[slug] = price
				}
			}
			if model.ToolCall == nil {
				continue
			}
			facts := Facts{ToolCalls: *model.ToolCall, Reasoning: model.Reasoning, Images: slices.Contains(model.Modalities.Input, "image")}
			for _, option := range model.ReasoningOptions {
				if option.Type == "effort" {
					facts.Efforts = option.Values
				}
			}
			registry.Facts[slug] = facts
		}
	}
	if len(registry.Windows) == 0 {
		return Registry{}, fmt.Errorf("models: %s lists no model with a context window", from)
	}
	return registry, nil
}

func ShippedRegistry() (Registry, error) {
	taken := strings.TrimSpace(shippedRegistryTaken)
	registry, err := ParseRegistry(shippedRegistry, "the snapshot of models.dev taken on "+taken)
	if err != nil {
		return Registry{}, err
	}
	return withPriceOverrides(registry.takenOn(taken), library.Files())
}

func RegistryAt(path string) (Registry, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return ShippedRegistry()
	}
	written, err := os.Stat(path)
	var stored Registry
	if err != nil || json.Unmarshal(body, &stored) != nil || len(stored.Windows) == 0 {
		return Registry{}, fmt.Errorf("models: %s is not a registry tofu wrote, delete it and run %s", path, ReloadVerb)
	}
	return withPriceOverrides(stored.takenOn(written.ModTime().Format(time.DateOnly)), library.Files())
}

func (r Registry) takenOn(day string) Registry {
	for slug, price := range r.Prices {
		if price.Taken == "" {
			price.Taken = day
			r.Prices[slug] = price
		}
	}
	return r
}

func withPriceOverrides(r Registry, overrides fs.FS) (Registry, error) {
	files, err := fs.Glob(overrides, pricesGlob)
	if err != nil {
		return Registry{}, err
	}
	if r.Prices == nil {
		r.Prices = make(map[string]Price)
	}
	for _, file := range files {
		body, err := fs.ReadFile(overrides, file)
		if err != nil {
			return Registry{}, err
		}
		vendor := strings.TrimSuffix(path.Base(file), ".yaml")
		for line := range strings.Lines(string(body)) {
			if strings.TrimSpace(line) == "" {
				continue
			}
			id, card, _ := strings.Cut(line, ":")
			price, err := priceCard(card, "library/"+file)
			if err != nil {
				return Registry{}, fmt.Errorf("models: %s, %s: %w", file, strings.TrimSpace(id), err)
			}
			r.Prices[vendor+"/"+strings.TrimSpace(id)] = price
		}
	}
	return r, nil
}

func priceCard(card, from string) (Price, error) {
	price, named := Price{From: from}, map[string]bool{}
	for _, field := range strings.Split(card, ",") {
		name, value, _ := strings.Cut(strings.TrimSpace(field), " ")
		if name == "taken" {
			price.Taken = value
			continue
		}
		rate, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return Price{}, fmt.Errorf("%s is not a price per million tokens: %q", name, value)
		}
		switch name {
		case "input":
			price.Input = rate
		case "output":
			price.Output = rate
		case "cache_read":
			price.CacheRead = rate
		case "cache_write":
			price.CacheWrite = rate
		default:
			return Price{}, fmt.Errorf("unknown field %q, a card names input, output, cache_read, cache_write and taken", name)
		}
		named[name] = true
	}
	if !named["input"] || !named["output"] {
		return Price{}, errors.New("a card names input and output at least")
	}
	return price, nil
}

func (r Registry) Store(path string) error {
	body, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return os.WriteFile(path, body, writtenFileMode)
}

func RegistrySource() string {
	if set := strings.TrimSpace(os.Getenv(RegistryURLVariable)); set != "" {
		return set
	}
	return RegistryURL
}

func FetchRegistry(ctx context.Context, client *transport.Client, url string) ([]byte, error) {
	response, err := client.Do(ctx, transport.Request{
		Method: http.MethodGet,
		URL:    url,
		Header: http.Header{"Accept": []string{"application/json"}},
	})
	if err != nil {
		return nil, err
	}
	return response.Body, nil
}

func (r Registry) Window(slug string) int {
	if tokens := r.Windows[slug]; tokens > 0 {
		return tokens
	}
	return r.Windows[withoutDatedSuffix(slug)]
}

func (r Registry) Fact(slug string) (Facts, bool) {
	if facts, known := r.Facts[slug]; known {
		return facts, true
	}
	facts, known := r.Facts[withoutDatedSuffix(slug)]
	return facts, known
}

func withoutDatedSuffix(slug string) string {
	at := strings.LastIndex(slug, "-")
	if at < 0 || len(slug)-at-1 != datedSuffixDigits {
		return slug
	}
	if _, err := strconv.Atoi(slug[at+1:]); err != nil {
		return slug
	}
	return slug[:at]
}
