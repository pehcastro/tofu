package api

import (
	"context"
	"time"

	"tofu/internal/judge/jev"
	"tofu/internal/judge/jev/wire/openrouter"
	"tofu/internal/judge/jev/wire/typesafe"
	"tofu/internal/konst"
	"tofu/internal/transport"
)

func judgeTransport() transport.Config {
	return transport.Config{
		AttemptTimeout: time.Duration(konst.JudgeTimeoutMillis) * time.Millisecond,
		Retries:        konst.JudgeRetries,
		Backoff:        250 * time.Millisecond,
		Concurrency:    1,
	}
}

func NewWire(key string) (*openrouter.Wire, error) {
	return openrouter.New(openrouter.Config{Key: key, Transport: judgeTransport()})
}

func NewTypeSafeWire(key string) (*typesafe.Wire, error) {
	return typesafe.New(typesafe.Config{Key: key, Transport: judgeTransport()})
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
