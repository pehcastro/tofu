package classifiers

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"time"

	"tofu/internal/konst"
	"tofu/internal/models/classifiers/core"
	"tofu/internal/models/classifiers/providers"
	"tofu/internal/models/classifiers/providers/openrouter"
	"tofu/internal/models/classifiers/providers/typesafe"
	"tofu/internal/models/llm/catalog"
	"tofu/internal/models/transport"
	"tofu/internal/sys"
	shipped "tofu/library"
)

const (
	EndpointVariable = "TOFU_JUDGE_ENDPOINT"
	unset            = "the gate is off because no classifier is bound: store its key with tofu login classifier and pick it in Models"
)

type provider struct {
	key      string
	build    func(string, providers.Config) (*providers.Wire, error)
	alias    func(string) (string, error)
	checkPin func(context.Context, string, providers.Config, *transport.Client) (core.PinCheck, core.Pin)
	serves   func(core.Pin, string) bool
}

func providerOf(of catalog.Provider) (provider, bool) {
	switch of {
	case catalog.TypeSafe:
		return provider{key: sys.TypeSafeKeyName, build: typesafe.New, alias: typesafe.Model, serves: typesafe.Serves,
			checkPin: func(ctx context.Context, bound string, config providers.Config, _ *transport.Client) (core.PinCheck, core.Pin) {
				return typesafe.CheckPin(ctx, bound, config)
			}}, true
	case catalog.OpenRouter:
		return provider{key: sys.OpenRouterClassifierKeyName, build: openrouter.New, alias: openrouter.Model, serves: openrouter.Serves, checkPin: openrouter.CheckPin}, true
	case catalog.Anthropic, catalog.OpenAI, catalog.Meta:
		return provider{}, false
	}
	panic("classifier: unknown provider " + string(of))
}

type Route struct {
	Model catalog.Model
	Key   string
}

func KeyName(of catalog.Provider) string {
	served, _ := providerOf(of)
	return served.key
}

func Bound(dir string, keys core.Keyring) (catalog.Model, error) {
	layers, err := catalog.Layers(shipped.Files(), dir)
	if err != nil {
		return catalog.Model{}, err
	}
	loaded, err := catalog.Load(layers)
	var broken *catalog.BrokenLibrary
	if err != nil && !errors.As(err, &broken) {
		return catalog.Model{}, err
	}
	if model, bound := loaded.Bound(catalog.RoleClassifier); bound {
		return model, nil
	}
	var stored []string
	for _, model := range loaded.Models {
		if model.Kind != catalog.KindClassifier {
			continue
		}
		name := KeyName(model.Provider)
		if key, _ := keys.KeyFor(sys.CredentialFileName, name); key != "" && !slices.Contains(stored, name) {
			stored = append(stored, name)
		}
	}
	if len(stored) == 0 {
		return catalog.Model{}, core.Unbound(unset)
	}
	verb := " is"
	if len(stored) > 1 {
		verb = " are"
	}
	return catalog.Model{}, core.Unbound("the gate is off because no classifier is bound, though " + strings.Join(stored, " and ") + verb +
		" stored: pick the classifier in Models or run tofu models bind classifier provider/model")
}

func Resolve(dir string, keys core.Keyring) (Route, error) {
	model, err := Bound(dir, keys)
	if err != nil {
		return Route{}, err
	}
	route := Route{Model: model}
	served, err := route.served()
	if err != nil {
		return Route{}, err
	}
	route.Key, err = keys.KeyFor(sys.CredentialFileName, served.key)
	return route, err
}

func Open(dir string, keys core.Keyring, concurrency int) (*core.Client, error) {
	route, err := Resolve(dir, keys)
	if err != nil {
		return nil, err
	}
	return route.Client(concurrency)
}

func (r Route) config(concurrency int) providers.Config {
	return providers.Config{
		Key:      r.Key,
		Endpoint: os.Getenv(EndpointVariable),
		Transport: transport.Config{
			AttemptTimeout: konst.JudgeTimeoutMillis * time.Millisecond,
			Retries:        konst.JudgeRetries,
			Backoff:        konst.JudgeBackoffMillis * time.Millisecond,
			Concurrency:    concurrency,
		},
	}
}

func (r Route) served() (provider, error) {
	served, ok := providerOf(r.Model.Provider)
	if !ok {
		return provider{}, errors.New("the classifier is bound to " + r.Model.Slug() + ", and " + string(r.Model.Provider) + " serves no classifier")
	}
	return served, nil
}

func (r Route) Client(concurrency int) (*core.Client, error) {
	served, err := r.served()
	if err != nil {
		return nil, err
	}
	built, err := served.build(r.Model.Requested(), r.config(concurrency))
	if err != nil {
		return nil, err
	}
	return core.NewClient(core.Config{Wire: built, Key: served.key})
}

func (r Route) Alias() (string, error) {
	served, err := r.served()
	if err != nil {
		return "", err
	}
	return served.alias(r.Model.Requested())
}

func (r Route) CheckPin(ctx context.Context, listing *transport.Client) (core.PinCheck, core.Pin) {
	served, err := r.served()
	if err != nil {
		return core.PinCheck{Skipped: "the pin stays", Error: err.Error()}, core.Pin{}
	}
	return served.checkPin(ctx, r.Model.Requested(), r.config(1), listing)
}

func (r Route) Serves(pin core.Pin, build string) bool {
	served, err := r.served()
	return err == nil && served.serves(pin, build)
}
