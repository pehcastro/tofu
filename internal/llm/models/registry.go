package models

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"

	"tofu/internal/transport"
)

//go:embed data/models-dev.json
var shippedRegistry []byte

const (
	ShippedRegistryName = "the snapshot of models.dev taken on 2026-09-21"
	RegistryURL         = "https://models.dev/api.json"
	RegistryURLVariable = "TOFU_MODELS_REGISTRY_URL"
	datedSuffixDigits   = 8
	writtenFileMode     = 0o644
)

type Facts struct {
	ToolCalls bool     `json:"tool_calls"`
	Reasoning bool     `json:"reasoning"`
	Efforts   []string `json:"efforts,omitempty"`
	Images    bool     `json:"images"`
}

type Registry struct {
	From    string           `json:"from"`
	Windows map[string]int   `json:"windows"`
	Facts   map[string]Facts `json:"facts,omitempty"`
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
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return Registry{}, fmt.Errorf("models: %s is not the shape models.dev serves: %w", from, err)
	}
	registry := Registry{From: from, Windows: make(map[string]int), Facts: make(map[string]Facts)}
	for provider, listed := range payload {
		for id, model := range listed.Models {
			slug := provider + "/" + id
			if model.Limit.Context > 0 {
				registry.Windows[slug] = model.Limit.Context
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
	return ParseRegistry(shippedRegistry, ShippedRegistryName)
}

func RegistryAt(path string) (Registry, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return ShippedRegistry()
	}
	var stored Registry
	if err := json.Unmarshal(body, &stored); err != nil || len(stored.Windows) == 0 {
		return Registry{}, fmt.Errorf("models: %s is not a registry tofu wrote, delete it and run %s", path, ReloadVerb)
	}
	return stored, nil
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
