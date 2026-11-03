package models

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"

	"tofu/internal/sys"
)

const customFields = "provider and id, and may name subscription, use, reason, notice, vision, plan, verbosity, reasoning_context, efforts and served_as"

type customModel struct {
	Provider         Provider `json:"provider"`
	ID               string   `json:"id"`
	Subscription     string   `json:"subscription"`
	Use              string   `json:"use"`
	Reason           string   `json:"reason"`
	Notice           string   `json:"notice"`
	Vision           string   `json:"vision"`
	Plan             string   `json:"plan"`
	Verbosity        string   `json:"verbosity"`
	ReasoningContext string   `json:"reasoning_context"`
	Efforts          []string `json:"efforts"`
	ServedAs         string   `json:"served_as"`
}

func (k Kind) customDir() string {
	switch k {
	case KindLLM:
		return "llms"
	case KindClassifier:
		return "classifiers"
	}
	panic("models: unknown kind " + string(k))
}

func (k Kind) noModule(provider Provider, subscription string) string {
	reached := string(provider) + " with a key"
	if subscription != "" {
		reached = string(provider) + " through " + subscription
	}
	switch k {
	case KindClassifier:
		if subscription == "" && (provider == TypeSafe || provider == OpenRouter) {
			return ""
		}
		return fmt.Sprintf("tofu has no classifier module for %s: a classifier runs on %s or %s with a key", reached, TypeSafe, OpenRouter)
	case KindLLM:
		if subscription == "" && (provider == OpenRouter || provider == Meta) || subscription != "" && (provider == Anthropic || provider == OpenAI) {
			return ""
		}
		return fmt.Sprintf("tofu has no llm module for %s: an llm runs on %s or %s through a subscription, or on %s or %s with a key",
			reached, Anthropic, OpenAI, OpenRouter, Meta)
	}
	panic("models: unknown kind " + string(k))
}

func readCustom(layer Layer, into *merged) []Broken {
	var refused []Broken
	for _, kind := range []Kind{KindLLM, KindClassifier} {
		dir := modelsDir + "/" + kind.customDir()
		for _, entry := range entriesOf(layer.FS, dir) {
			at := sys.Join(layer.Origin, dir, entry.Name())
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				refused = append(refused, Broken{File: at, Why: "a custom " + string(kind) + " is one json file"})
				continue
			}
			custom, why := decodeCustom(layer.FS, dir+"/"+entry.Name(), kind)
			if why != "" {
				refused = append(refused, Broken{File: at, Why: why})
				continue
			}
			*into.sheetOf(string(custom.Provider) + "/" + custom.ID) = sheet{file: at, layer: layer.Name, values: map[string]string{
				"kind":              string(kind),
				"subscription":      custom.Subscription,
				"use":               cmp.Or(custom.Use, string(UseAllowed)),
				"reason":            custom.Reason,
				"notice":            custom.Notice,
				"vision":            custom.Vision,
				"plan":              custom.Plan,
				"verbosity":         custom.Verbosity,
				"reasoning_context": custom.ReasoningContext,
				"efforts":           strings.Join(custom.Efforts, ","),
				"served_as":         custom.ServedAs,
			}}
		}
	}
	return refused
}

func decodeCustom(fsys fs.FS, file string, kind Kind) (customModel, string) {
	var custom customModel
	body, err := fs.ReadFile(fsys, file)
	if err != nil {
		return custom, err.Error()
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&custom); err != nil {
		return custom, err.Error() + ", and a custom " + string(kind) + " names " + customFields
	}
	if custom.Provider == "" || custom.ID == "" {
		return custom, "a custom " + string(kind) + " names " + customFields
	}
	return custom, kind.noModule(custom.Provider, custom.Subscription)
}
