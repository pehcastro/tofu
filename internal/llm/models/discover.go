package models

import (
	"cmp"
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"tofu/internal/llm/wire/anthropic"
	"tofu/internal/llm/wire/codex"
	"tofu/internal/transport"
)

const (
	anthropicModelsURL = anthropic.OfficialBaseURL + "/v1/models?limit=100"
	codexModelsURL     = codex.SubscriptionBaseURL + "/codex/models?client_version=" + codex.PinnedCodexClientVersion
	oauthBeta          = "oauth-2025-04-20"
)

type Account struct {
	Subscription Subscription
	AccountID    string
	Token        func(context.Context) (string, error)
	BaseURL      string
}

type Served struct {
	Subscription Subscription
	Pin          string
	IDs          []string
}

func Discover(ctx context.Context, client *transport.Client, account Account) (Served, error) {
	served := Served{Subscription: account.Subscription, Pin: pinOf(account.Subscription)}
	token, err := account.Token(ctx)
	if err != nil {
		return served, err
	}
	response, err := client.Do(ctx, discoveryRequest(account, token))
	if err != nil {
		return served, err
	}
	served.IDs, err = servedIDs(response.Body)
	if err != nil {
		return served, err
	}
	sort.Strings(served.IDs)
	return served, nil
}

func pinOf(subscription Subscription) string {
	switch subscription {
	case Claude:
		return "claude-cli " + anthropic.PinnedClaudeCodeVersion
	case Codex:
		return "codex client version " + codex.PinnedCodexClientVersion
	}
	panic("models: unknown subscription " + string(subscription))
}

func discoveryRequest(account Account, token string) transport.Request {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+token)
	header.Set("Accept", "application/json")
	url := ""
	switch account.Subscription {
	case Claude:
		url = anthropicModelsURL
		header.Set("anthropic-version", anthropic.AnthropicAPIVersion)
		header.Set("anthropic-beta", oauthBeta)
		header.Set("User-Agent", anthropic.ClaudeCodeUserAgent)
	case Codex:
		url = codexModelsURL
		if account.AccountID != "" {
			header.Set(codex.HeaderAccountID, account.AccountID)
		}
		header.Set(codex.HeaderBeta, codex.BetaResponsesSSE)
		header.Set(codex.HeaderOriginator, codex.Originator)
		header.Set(codex.HeaderVersion, codex.PinnedCodexClientVersion)
	default:
		panic("models: unknown subscription " + string(account.Subscription))
	}
	return transport.Request{
		Method: http.MethodGet,
		URL:    cmp.Or(account.BaseURL, url),
		Header: header,
	}
}

type servedEntry struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
}

type servedBody struct {
	Data   []servedEntry `json:"data"`
	Models []servedEntry `json:"models"`
}

func servedIDs(body []byte) ([]string, error) {
	var payload servedBody
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, transport.Fail("models.Discover", transport.KindProvider, nil,
			"the model list is not the shape either backend documents")
	}
	entries := payload.Models
	if len(entries) == 0 {
		entries = payload.Data
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if id := cmp.Or(entry.Slug, entry.ID); id != "" {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

type Reconciliation struct {
	Served      Served
	Unknown     []string
	Unreachable []string
}

func (c Catalog) Reconcile(served Served) Reconciliation {
	accounted := make(map[string]bool, len(c.Models))
	for _, spec := range c.Subscriptions {
		if spec.ID != served.Subscription {
			continue
		}
		for _, id := range spec.NotModels {
			accounted[id] = true
		}
	}
	for _, model := range c.Models {
		if model.Subscription == served.Subscription {
			accounted[model.ID] = true
		}
	}
	isServed := make(map[string]bool, len(served.IDs))
	result := Reconciliation{Served: served}
	for _, id := range served.IDs {
		isServed[id] = true
		if !accounted[id] {
			result.Unknown = append(result.Unknown, id)
		}
	}
	for _, model := range c.Models {
		if model.Subscription == served.Subscription && !isServed[model.ID] {
			result.Unreachable = append(result.Unreachable, model.Slug())
		}
	}
	return result
}

func (r Reconciliation) Lines() []string {
	head := string(r.Served.Subscription) + ": "
	return []string{
		head + "the account serves " + list(r.Served.IDs) + " under " + r.Served.Pin,
		head + "served and not in the catalog: " + list(r.Unknown),
		head + "in the catalog and not served: " + list(r.Unreachable),
	}
}

func list(ids []string) string {
	if len(ids) == 0 {
		return "(none)"
	}
	return strings.Join(ids, ", ")
}
