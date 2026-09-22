package models

import (
	"cmp"
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"tofu/internal/llm"
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
	Windows      map[string]int
}

func Discover(ctx context.Context, client *transport.Client, account Account) (Served, error) {
	served := Served{Subscription: account.Subscription, Pin: pinOf(account.Subscription)}
	token, err := account.Token(ctx)
	if err != nil {
		return served, err
	}
	dump := discoveryRequest(account, token)
	header := http.Header{}
	for _, pair := range dump.Headers {
		header.Set(pair.Name, pair.Value)
	}
	response, err := client.Do(ctx, transport.Request{Method: dump.Method, URL: dump.URL, Header: header})
	if err != nil {
		return served, err
	}
	served.IDs, served.Windows, err = servedModels(response.Body)
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

func discoveryRequest(account Account, token string) llm.Dump {
	headers := []llm.Header{
		{Name: "Authorization", Value: "Bearer " + token},
		{Name: "Accept", Value: "application/json"},
	}
	url := ""
	switch account.Subscription {
	case Claude:
		url = anthropicModelsURL
		headers = append(headers,
			llm.Header{Name: "anthropic-version", Value: anthropic.AnthropicAPIVersion},
			llm.Header{Name: "anthropic-beta", Value: oauthBeta},
			llm.Header{Name: "User-Agent", Value: anthropic.ClaudeCodeUserAgent})
	case Codex:
		url = codexModelsURL
		if account.AccountID != "" {
			headers = append(headers, llm.Header{Name: codex.HeaderAccountID, Value: account.AccountID})
		}
		headers = append(headers,
			llm.Header{Name: codex.HeaderBeta, Value: codex.BetaResponsesSSE},
			llm.Header{Name: codex.HeaderOriginator, Value: codex.Originator},
			llm.Header{Name: codex.HeaderVersion, Value: codex.PinnedCodexClientVersion})
	default:
		panic("models: unknown subscription " + string(account.Subscription))
	}
	return llm.Dump{
		Method:      http.MethodGet,
		URL:         cmp.Or(account.BaseURL, url),
		Headers:     headers,
		Identifiers: []string{account.AccountID},
	}
}

type servedEntry struct {
	ID               string `json:"id"`
	Slug             string `json:"slug"`
	ContextWindow    int    `json:"context_window"`
	MaxInputTokens   int    `json:"max_input_tokens"`
	MaxContextWindow int    `json:"max_context_window"`
}

type servedBody struct {
	Data   []servedEntry `json:"data"`
	Models []servedEntry `json:"models"`
}

func servedModels(body []byte) ([]string, map[string]int, error) {
	var payload servedBody
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, nil, transport.Fail("models.Discover", transport.KindProvider, err,
			"the model list is not the shape either backend documents")
	}
	entries := payload.Models
	if len(entries) == 0 {
		entries = payload.Data
	}
	ids := make([]string, 0, len(entries))
	windows := make(map[string]int)
	for _, entry := range entries {
		id := cmp.Or(entry.Slug, entry.ID)
		if id == "" {
			continue
		}
		ids = append(ids, id)
		if tokens := cmp.Or(entry.ContextWindow, entry.MaxInputTokens, entry.MaxContextWindow); tokens > 0 {
			windows[id] = tokens
		}
	}
	return ids, windows, nil
}

func WindowFor(model Model, registry Registry, served Served) (int, string) {
	if tokens := registry.Window(model.VendorSlug()); tokens > 0 {
		return tokens, "as " + registry.From + " lists " + model.VendorSlug()
	}
	if tokens := served.Windows[model.ID]; tokens > 0 {
		return tokens, "as the " + string(served.Subscription) + " account reports it under " + served.Pin
	}
	return 0, ""
}

type Reconciliation struct {
	Served      Served
	Table       string
	Unknown     []string
	Unreachable []string
	Windows     []string
}

func (c Library) Reconcile(served Served, registry Registry) Reconciliation {
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
		if model.Subscription != served.Subscription {
			continue
		}
		if !isServed[model.ID] {
			result.Unreachable = append(result.Unreachable, model.Slug())
		}
		if tokens := served.Windows[model.ID]; tokens > 0 && tokens != registry.Window(model.VendorSlug()) {
			result.Windows = append(result.Windows, model.Slug()+" "+strconv.Itoa(tokens))
		}
	}
	result.Table = registry.From
	return result
}

func (r Reconciliation) Lines() []string {
	head := string(r.Served.Subscription) + ": "
	return []string{
		head + "the account serves " + list(r.Served.IDs) + " under " + r.Served.Pin,
		head + "served and not in the library: " + list(r.Unknown),
		head + "in the library and not served: " + list(r.Unreachable),
		head + "windows the account reports that " + r.Table + " does not match: " + list(r.Windows),
	}
}

func list(ids []string) string {
	if len(ids) == 0 {
		return "(none)"
	}
	return strings.Join(ids, ", ")
}
