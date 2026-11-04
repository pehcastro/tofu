package wiring

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strconv"

	"tofu/internal/cli/render"
	"tofu/internal/host"
	"tofu/internal/konst"
	"tofu/internal/models/classifiers"
	"tofu/internal/models/classifiers/core"
	"tofu/internal/models/llm/accounts"
	"tofu/internal/models/llm/catalog"
	"tofu/internal/sys"
)

const (
	OpenRouterName    = string(catalog.OpenRouter)
	TypeSafeName      = string(catalog.TypeSafe)
	MetaName          = string(catalog.Meta)
	BraveName         = "brave"
	BraveDisplay      = "Brave"
	OpenRouterFix     = "run tofu login classifier openrouter and paste the key when it asks"
	noAccountYet      = "no account captured yet"
	accountMarkBytes  = 2
	openRouterReports = "OpenRouter reports spend and limits for the key and a cost per call; per-model history needs a management key"
	typeSafeReports   = "TypeSafe reports tokens per call; it has no spend, balance or limit API, so cost is estimated from list price"
	metaReports       = "Meta reports tokens per call and team rate limits; it has no spend or balance API, so cost is estimated from list price"
)

type KeyFacts struct {
	Role                                          host.AccountRole
	Provider, Variable, Name, Use, Reports, Model string
}

func (f KeyFacts) Hint() string { return CommandFor("login", f.Role, f.Provider) }

func (f KeyFacts) Status(resolve func(variable string) string) host.KeyStatus {
	key := host.KeyStatus{Role: f.Role, Provider: f.Provider, Variable: f.Variable, ProviderReports: f.Reports, Model: f.Model}
	if value := resolve(f.Variable); value != "" {
		key.Key = render.Mask(value)
	}
	return key
}

func classifierKeyUse(bound catalog.Model, err error, provider catalog.Provider) string {
	switch {
	case core.IsUnbound(err):
		return "unused, no classifier is bound: pick one in Models"
	case err != nil:
		return "unused, the classifier is unreadable: " + err.Error()
	case bound.Provider != provider:
		return "unused, the classifier is " + bound.Slug()
	}
	model, err := classifiers.Route{Model: bound}.Alias()
	if err != nil {
		return "bound, the pin is unreadable: " + err.Error()
	}
	return "bound, judges tool calls with " + model
}

func BraveReports() string {
	return "Brave reports monthly calls remaining and the reset; it has no spend API, so cost is estimated from " +
		strconv.FormatFloat(konst.BraveSearchDollarsPerCall*1000, 'f', -1, 64) + " USD per 1000 calls"
}

func KeyTable() []KeyFacts {
	bound, err := BoundClassifier()
	boundOn := func(provider catalog.Provider) string {
		if err != nil || bound.Provider != provider {
			return ""
		}
		return bound.Slug()
	}
	return []KeyFacts{
		{host.RoleLLM, MetaName, sys.MetaMuseKeyName, catalog.Meta.Display(), "serves the meta models", metaReports, ""},
		{host.RoleLLM, OpenRouterName, sys.OpenRouterKeyName, catalog.OpenRouter.Display(), "serves the openrouter models on your own key", openRouterReports, ""},
		{host.RoleClassifier, TypeSafeName, classifiers.KeyName(catalog.TypeSafe), catalog.TypeSafe.Display(), classifierKeyUse(bound, err, catalog.TypeSafe), typeSafeReports, boundOn(catalog.TypeSafe)},
		{host.RoleClassifier, OpenRouterName, classifiers.KeyName(catalog.OpenRouter), catalog.OpenRouter.Display(), classifierKeyUse(bound, err, catalog.OpenRouter), openRouterReports, boundOn(catalog.OpenRouter)},
		{host.RoleSearch, BraveName, sys.BraveSearchKeyName, BraveDisplay, "backs the web search tool", BraveReports(), ""},
	}
}

func KeyFactsOf(of host.AccountRole, provider string) KeyFacts {
	table := KeyTable()
	return table[slices.IndexFunc(table, func(fact KeyFacts) bool { return fact.Role == of && fact.Provider == provider })]
}

func KeyStatuses(resolve func(variable string) string) []host.KeyStatus {
	var keys []host.KeyStatus
	for _, fact := range KeyTable() {
		keys = append(keys, fact.Status(resolve))
	}
	return keys
}

func firstRoleOf(provider string) host.AccountRole {
	table := KeyTable()
	if at := slices.IndexFunc(table, func(fact KeyFacts) bool { return fact.Provider == provider }); at >= 0 {
		return table[at].Role
	}
	return host.RoleLLM
}

func LoginWords(provider string) []string { return []string{string(firstRoleOf(provider)), provider} }

func LoginHint(provider string) string { return CommandFor("login", firstRoleOf(provider), provider) }

func AccountName(identity accounts.Identity, redact bool) string {
	if redact {
		return cmp.Or(MaskedAccount(identity), noAccountYet)
	}
	return cmp.Or(identity.Email, identity.AccountID, noAccountYet)
}

func MaskedAccount(identity accounts.Identity) string {
	account := cmp.Or(identity.Email, identity.AccountID)
	if account == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(account))
	return render.Mask(account) + "#" + hex.EncodeToString(sum[:accountMarkBytes])
}
