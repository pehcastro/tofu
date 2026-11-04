package typesafe

import (
	"context"
	"errors"
	"time"

	"tofu/internal/konst"
	"tofu/internal/models/classifiers/core"
	"tofu/internal/models/llm/catalog"
	"tofu/internal/models/transport"
	shipped "tofu/library"
)

func judgeTransport() transport.Config {
	return transport.Config{
		AttemptTimeout: time.Duration(konst.JudgeTimeoutMillis) * time.Millisecond,
		Retries:        konst.JudgeRetries,
		Backoff:        time.Duration(konst.JudgeBackoffMillis) * time.Millisecond,
		Concurrency:    konst.SiftConcurrency,
	}
}

func NewWire(key string) (*Wire, error) {
	model, err := JudgeModel()
	if err != nil {
		return nil, err
	}
	return New(model, Config{Key: key, Transport: judgeTransport()})
}

func JudgeModel() (string, error) {
	layers, err := catalog.Layers(shipped.Files(), "")
	if err != nil {
		return "", err
	}
	library, err := catalog.Load(layers)
	if err != nil {
		return "", err
	}
	if bound, ok := library.Bound(catalog.RoleClassifier); ok && bound.Provider == catalog.TypeSafe {
		return bound.Requested(), nil
	}
	for _, model := range library.Models {
		if model.Provider == catalog.TypeSafe && model.Kind == catalog.KindClassifier && model.Use != catalog.UseExcluded {
			return model.Requested(), nil
		}
	}
	return "", errors.New("the model library has no typesafe classifier file, so the bench has no model to ask for")
}

type Call struct {
	Request  core.Request
	Response core.Response
	Raw      core.Raw
	Err      error
}

func Ask(ctx context.Context, wire core.Wire, request core.Request) Call {
	call := Call{Request: request}
	body, err := request.Encode(wire.Model())
	if err != nil {
		call.Err = err
		return call
	}
	raw, err := wire.Post(ctx, body)
	call.Raw = raw
	if err != nil {
		call.Err = err
		return call
	}
	response, err := core.Decode(raw.Body)
	if err != nil {
		call.Err = err
		return call
	}
	call.Response = response
	if err := core.Validate(request, response); err != nil {
		call.Err = err
		return call
	}
	return call
}
