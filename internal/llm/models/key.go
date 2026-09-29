package models

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
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
	if err == nil {
		_, err = client.Do(ctx, transport.Request{
			Method: http.MethodGet,
			URL:    MetaBaseURL() + codex.ModelsPath,
			Header: http.Header{"Authorization": {"Bearer " + key}, "Accept": {"application/json"}},
		})
	}
	var answered *transport.Error
	if errors.As(err, &answered) && answered.Status != 0 {
		err = fmt.Errorf("meta answered %d %s", answered.Status, answered.Detail)
	}
	if err != nil {
		return errors.New("the key did not reach meta, so nothing was written: " + strings.TrimSpace(strings.ReplaceAll(err.Error(), key, sys.KeyRedactedMark)))
	}
	return sys.SaveKey(sys.MetaMuseKeyName, key)
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
