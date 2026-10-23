package classifier

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"time"

	"tofu/internal/judge/jev"
	"tofu/internal/judge/jev/wire"
	"tofu/internal/judge/jev/wire/openrouter"
	"tofu/internal/judge/jev/wire/typesafe"
	"tofu/internal/konst"
	"tofu/internal/llm/models"
	"tofu/internal/sys"
	"tofu/internal/transport"
	shipped "tofu/library"
)

const (
	EndpointVariable = "TOFU_JUDGE_ENDPOINT"
	unset            = "the gate is off because no classifier is bound: store its key with tofu login classifier and pick it in Models"
)

type provider struct {
	key      string
	build    func(string, wire.Config) (*wire.Wire, error)
	alias    func(string) (string, error)
	checkPin func(context.Context, string, wire.Config, *transport.Client) (jev.PinCheck, jev.Pin)
	serves   func(jev.Pin, string) bool
}

func providerOf(of models.Provider) (provider, bool) {
	switch of {
	case models.TypeSafe:
		return provider{key: sys.TypeSafeKeyName, build: typesafe.New, alias: typesafe.Model, serves: typesafe.Serves,
			checkPin: func(ctx context.Context, bound string, config wire.Config, _ *transport.Client) (jev.PinCheck, jev.Pin) {
				return typesafe.CheckPin(ctx, bound, config)
			}}, true
	case models.OpenRouter:
		return provider{key: sys.OpenRouterClassifierKeyName, build: openrouter.New, alias: openrouter.Model, serves: openrouter.Serves, checkPin: openrouter.CheckPin}, true
	case models.Anthropic, models.OpenAI, models.Meta:
		return provider{}, false
	}
	panic("classifier: unknown provider " + string(of))
}

type Route struct {
	Model models.Model
	Key   string
}

func KeyName(of models.Provider) string {
	served, _ := providerOf(of)
	return served.key
}

func Bound(dir string, keys jev.Keyring) (models.Model, error) {
	layers, err := models.Layers(shipped.Files(), dir)
	if err != nil {
		return models.Model{}, err
	}
	loaded, err := models.Load(layers)
	var broken *models.BrokenLibrary
	if err != nil && !errors.As(err, &broken) {
		return models.Model{}, err
	}
	if model, bound := loaded.Bound(models.RoleClassifier); bound {
		return model, nil
	}
	var stored []string
	for _, model := range loaded.Models {
		if model.Kind != models.KindClassifier {
			continue
		}
		name := KeyName(model.Provider)
		if key, _ := keys.KeyFor(sys.CredentialFileName, name); key != "" && !slices.Contains(stored, name) {
			stored = append(stored, name)
		}
	}
	if len(stored) == 0 {
		return models.Model{}, jev.Unbound(unset)
	}
	verb := " is"
	if len(stored) > 1 {
		verb = " are"
	}
	return models.Model{}, jev.Unbound("the gate is off because no classifier is bound, though " + strings.Join(stored, " and ") + verb +
		" stored: pick the classifier in Models or run tofu models bind classifier provider/model")
}

func Resolve(dir string, keys jev.Keyring) (Route, error) {
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

func Open(dir string, keys jev.Keyring, concurrency int) (*jev.Client, error) {
	route, err := Resolve(dir, keys)
	if err != nil {
		return nil, err
	}
	return route.Client(concurrency)
}

func (r Route) config(concurrency int) wire.Config {
	return wire.Config{
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

func (r Route) Client(concurrency int) (*jev.Client, error) {
	served, err := r.served()
	if err != nil {
		return nil, err
	}
	built, err := served.build(r.Model.Requested(), r.config(concurrency))
	if err != nil {
		return nil, err
	}
	return jev.NewClient(jev.Config{Wire: built, Key: served.key})
}

func (r Route) Alias() (string, error) {
	served, err := r.served()
	if err != nil {
		return "", err
	}
	return served.alias(r.Model.Requested())
}

func (r Route) CheckPin(ctx context.Context, listing *transport.Client) (jev.PinCheck, jev.Pin) {
	served, err := r.served()
	if err != nil {
		return jev.PinCheck{Skipped: "the pin stays", Error: err.Error()}, jev.Pin{}
	}
	return served.checkPin(ctx, r.Model.Requested(), r.config(1), listing)
}

func (r Route) Serves(pin jev.Pin, build string) bool {
	served, err := r.served()
	return err == nil && served.serves(pin, build)
}
