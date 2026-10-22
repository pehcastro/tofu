package typesafe

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"tofu/internal/judge/jev"
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

	legacyVendor   = "typesafe/"
	releasePattern = "jev-%d.%d"
	patchZero      = ".0"
)

type Config struct {
	Endpoint  string
	Key       string
	Transport transport.Config
}

type Wire struct {
	model    string
	endpoint string
	key      string
	client   *transport.Client
}

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

func Judge() (*jev.Client, error) {
	key, err := jev.TypeSafeKey(sys.CredentialFileName)
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
	return on(model, config)
}

func on(model string, config Config) (*Wire, error) {
	if config.Key == "" {
		return nil, transport.Fail("typesafe.New", transport.KindMissingCredential, nil, "the wire has no credential")
	}
	client, err := transport.New(config.Transport)
	if err != nil {
		return nil, err
	}
	return &Wire{model: model, endpoint: cmp.Or(config.Endpoint, Endpoint), key: config.Key, client: client}, nil
}

func (w *Wire) Model() string { return w.model }

func (w *Wire) Caps() jev.WireCaps {
	return jev.WireCaps{
		Name:              Name,
		MaxStateTokens:    konst.JudgeStateTokenCeiling,
		MaxRequestTokens:  konst.JudgeRequestTokenCeiling,
		MaxRequestBytes:   jev.EstimateBytes(konst.JudgeStateTokenCeiling),
		CriteriaKinds:     []jev.CriteriaKind{jev.CriteriaString, jev.CriteriaObject, jev.CriteriaNull},
		MaxChoiceOptions:  konst.ChoiceCeiling,
		MaxScoreLevels:    konst.JudgeScoreLevelCeiling,
		ReturnsConfidence: true,
	}
}

func (w *Wire) Post(ctx context.Context, body []byte) (jev.Raw, error) {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+w.key)
	header.Set("Content-Type", "application/json")
	header.Set("Accept", "application/json")
	response, err := w.client.Do(ctx, transport.Request{Method: http.MethodPost, URL: w.endpoint, Body: body, Header: header})
	if err != nil {
		return jev.Raw{}, err
	}
	return jev.Raw{
		Body:      response.Body,
		RequestID: cmp.Or(response.Header.Get(RequestIDHeader), response.RequestID),
		Attempts:  response.Attempts,
		Latency:   response.Elapsed,
	}, nil
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
