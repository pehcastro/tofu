package accounts

import (
	"fmt"
	"time"
)

type Provider string

const (
	ClaudeSub Provider = "claude-sub"
	CodexSub  Provider = "codex-sub"
)

func AllProviders() []Provider {
	return []Provider{ClaudeSub, CodexSub}
}

func retiredProviderWords() map[string]Provider {
	return map[string]Provider{"anthropic": ClaudeSub, "codex": CodexSub}
}

func Canonical(name string) (Provider, bool) {
	for _, known := range AllProviders() {
		if name == string(known) {
			return known, true
		}
	}
	if source, retired := retiredProviderWords()[name]; retired {
		return source, true
	}
	return "", false
}

func ParseProvider(name string) (Provider, error) {
	if source, ok := Canonical(name); ok {
		return source, nil
	}
	return "", fmt.Errorf("cred: unknown provider %q, want %s or %s", name, ClaudeSub, CodexSub)
}

type TokenBody string

const (
	BodyJSON TokenBody = "json"
	BodyForm TokenBody = "form"
)

const KindOAuth = "oauth"

type Spec struct {
	Provider            Provider
	ClientID            string
	AuthorizeURL        string
	Scopes              []string
	AuthorizeParams     map[string]string
	CallbackPort        int
	CallbackPath        string
	PortFallback        bool
	TokenURL            string
	TokenBody           TokenBody
	SendStateOnExchange bool
	RefreshHeaders      map[string]string
	ExpirySkew          time.Duration
	IdentityTokenField  string
	AccountIDPath       string
	EmailPath           string
	OrgIDPath           string
	OrgNamePath         string
	Instructions        string
}

type Specs map[Provider]Spec

func (s Specs) Lookup(name string) (Spec, error) {
	source, err := ParseProvider(name)
	if err != nil {
		return Spec{}, err
	}
	spec, known := s[source]
	if !known {
		return Spec{}, fmt.Errorf("cred: no sign-in spec for %s", source)
	}
	return spec, nil
}
