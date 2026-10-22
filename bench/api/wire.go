package api

import (
	"context"
	"errors"
	"time"

	"tofu/internal/judge/jev"
	"tofu/internal/judge/jev/wire/typesafe"
	"tofu/internal/konst"
	"tofu/internal/llm/models"
	"tofu/internal/transport"
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

func NewWire(key string) (*typesafe.Wire, error) {
	model, err := JudgeModel()
	if err != nil {
		return nil, err
	}
	return typesafe.New(model, typesafe.Config{Key: key, Transport: judgeTransport()})
}

func JudgeModel() (string, error) {
	layers, err := models.Layers(shipped.Files(), "")
	if err != nil {
		return "", err
	}
	library, err := models.Load(layers)
	if err != nil {
		return "", err
	}
	if bound, ok := library.Bound(models.RoleClassifier); ok && bound.Provider == models.TypeSafe {
		return bound.Requested(), nil
	}
	for _, model := range library.Models {
		if model.Provider == models.TypeSafe && model.Kind == models.KindClassifier && model.Use != models.UseExcluded {
			return model.Requested(), nil
		}
	}
	return "", errors.New("the model library has no typesafe classifier file, so the bench has no model to ask for")
}

type Call struct {
	Request  jev.Request
	Response jev.Response
	Raw      jev.Raw
	Err      error
}

func Ask(ctx context.Context, wire jev.Wire, request jev.Request) Call {
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
	response, err := jev.Decode(raw.Body)
	if err != nil {
		call.Err = err
		return call
	}
	call.Response = response
	if err := jev.Validate(request, response); err != nil {
		call.Err = err
		return call
	}
	return call
}
