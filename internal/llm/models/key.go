package models

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/internal/konst"
	"tofu/internal/llm/wire/codex"
	"tofu/internal/sys"
	"tofu/internal/transport"
)

const MetaBaseURLVariable = "TOFU_META_BASE_URL"

func MetaBaseURL() string {
	return cmp.Or(os.Getenv(MetaBaseURLVariable), codex.MetaBaseURL)
}

func StoreMetaKey(ctx context.Context, key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("a meta key is one line and not empty")
	}
	client, err := transport.New(transport.Config{
		AttemptTimeout: time.Duration(konst.TurnAttemptTimeoutMillis) * time.Millisecond,
		Concurrency:    1,
	})
	if err != nil {
		return err
	}
	_, err = client.Do(ctx, transport.Request{
		Method: http.MethodGet,
		URL:    MetaBaseURL() + codex.ModelsPath,
		Header: http.Header{"Authorization": {"Bearer " + key}, "Accept": {"application/json"}},
	})
	if err != nil {
		refused := &KeyRefused{Provider: Meta, Detail: err.Error()}
		var answered *transport.Error
		if errors.As(err, &answered) && answered.Status != 0 {
			refused.Status, refused.Detail = answered.Status, answered.Detail
		}
		refused.Detail = strings.TrimSpace(strings.ReplaceAll(refused.Detail, key, sys.KeyRedactedMark))
		return refused
	}
	return sys.SaveKey(sys.MetaMuseKeyName, key)
}

type KeyRefused struct {
	Provider Provider
	Status   int
	Detail   string
}

func (e *KeyRefused) Brief() string {
	if e.Status == 0 {
		return "could not reach " + e.Provider.Display()
	}
	return e.Provider.Display() + " refused the key (" + strconv.Itoa(e.Status) + ")"
}

func (e *KeyRefused) Error() string {
	return e.Brief() + ", so nothing was written: " + e.Detail
}

func (c Library) KeyDefault(provider Provider) (Model, error) {
	var offered []Model
	for _, model := range c.Models {
		if model.Provider == provider && model.Pays() == PaysKey && model.Kind == KindLLM && model.Use != UseExcluded && model.Notice == "" {
			offered = append(offered, model)
		}
	}
	if len(offered) == 0 {
		return Model{}, errors.New("the model library has no " + string(provider) + " model to run without a --model")
	}
	return slices.MaxFunc(offered, func(a, b Model) int {
		_, aVersion := familyOf(a.ID)
		_, bVersion := familyOf(b.ID)
		return slices.Compare(aVersion, bVersion)
	}), nil
}
