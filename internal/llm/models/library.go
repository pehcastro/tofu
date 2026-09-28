package models

import (
	"fmt"
	"strings"

	"tofu/internal/llm"
	"tofu/internal/sys"
)

type Provider string

const (
	Anthropic  Provider = "anthropic"
	OpenAI     Provider = "openai"
	TypeSafe   Provider = "typesafe"
	OpenRouter Provider = "openrouter"
)

func (p Provider) valid() bool {
	switch p {
	case Anthropic, OpenAI, TypeSafe, OpenRouter:
		return true
	}
	return false
}

func (p Provider) KeyName() string {
	switch p {
	case OpenRouter:
		return sys.OpenRouterKeyName
	case TypeSafe:
		return sys.TypeSafeKeyName
	case Anthropic, OpenAI:
		return ""
	}
	panic("models: unknown provider " + string(p))
}

type Kind string

const (
	KindLLM        Kind = "llm"
	KindClassifier Kind = "classifier"
)

func (k Kind) valid() bool {
	switch k {
	case KindLLM, KindClassifier:
		return true
	}
	return false
}

type Pays string

const (
	PaysSubscription Pays = "subscription"
	PaysKey          Pays = "key"
)

type Subscription string

const (
	ClaudeSub Subscription = "claude-sub"
	CodexSub  Subscription = "codex-sub"
)

func AllSubscriptions() []Subscription {
	return []Subscription{ClaudeSub, CodexSub}
}

func (s Subscription) valid() bool {
	for _, known := range AllSubscriptions() {
		if s == known {
			return true
		}
	}
	return false
}

type Use string

const (
	UseDefault  Use = "default"
	UseAllowed  Use = "allowed"
	UseExcluded Use = "excluded"
)

func (u Use) valid() bool {
	switch u {
	case UseDefault, UseAllowed, UseExcluded:
		return true
	}
	return false
}

type Vision string

const (
	VisionUnstated Vision = ""
	VisionSees     Vision = "yes"
	VisionBlind    Vision = "no"
)

func (v Vision) valid() bool {
	switch v {
	case VisionUnstated, VisionSees, VisionBlind:
		return true
	}
	return false
}

type SubscriptionSpec struct {
	ID        Subscription
	Provider  Provider
	Wire      string
	Windows   []string
	NotModels []string
	File      string
}

type Model struct {
	Provider     Provider
	ID           string
	Subscription Subscription
	Windows      []string
	Use          Use
	Kind         Kind
	Vision       Vision
	Efforts      []llm.Effort
	Reason       string
	File         string
	Layer        string
	From         string
	Found        string
}

func (m Model) EffortTaken(asked llm.Effort) llm.Effort {
	if len(m.Efforts) == 0 {
		return ""
	}
	return asked
}

func (m Model) Slug() string {
	switch {
	case m.Subscription != "":
		return string(m.Subscription) + "/" + m.ID
	case m.Provider != "":
		return string(m.Provider) + "/" + m.ID
	}
	return m.ID
}

func (m Model) VendorSlug() string { return string(m.Provider) + "/" + m.ID }

func (m Model) Pays() Pays {
	if m.Subscription != "" {
		return PaysSubscription
	}
	return PaysKey
}

func (m Model) WindowText() string { return strings.Join(m.Windows, " and ") }

type Library struct {
	Subscriptions []SubscriptionSpec
	Models        []Model
	Roles         []Role
	Broken        []Broken
}

func (c Library) WireFor(id Subscription) string {
	for _, spec := range c.Subscriptions {
		if spec.ID == id {
			return spec.Wire
		}
	}
	return ""
}

func (c Library) ForWire(wire string) (SubscriptionSpec, bool) {
	for _, spec := range c.Subscriptions {
		if spec.Wire == wire {
			return spec, true
		}
	}
	return SubscriptionSpec{}, false
}

func (c Library) Wires() []string {
	wires := make([]string, 0, len(c.Subscriptions))
	for _, spec := range c.Subscriptions {
		wires = append(wires, spec.Wire)
	}
	return wires
}

type RefusalKind int

const (
	RefusedUnknown RefusalKind = iota
	RefusedExcluded
)

type Refusal struct {
	Kind  RefusalKind
	Slug  string
	Model Model
	Known []string
}

func (r *Refusal) Error() string {
	switch r.Kind {
	case RefusedExcluded:
		return "the model library excludes " + r.Slug + ": " + r.Model.Reason
	case RefusedUnknown:
		return "the model library has no " + r.Slug + ", it has " + strings.Join(r.Known, ", ") + "; run " + ReloadVerb + " to add what your accounts serve"
	}
	panic("models: unknown refusal kind")
}

func (c Library) Select(slug string) (Model, error) {
	known := make([]string, 0, len(c.Models))
	for _, model := range c.Models {
		if model.Slug() != slug {
			if model.Use != UseExcluded {
				known = append(known, model.Slug())
			}
			continue
		}
		if model.Use == UseExcluded {
			return Model{}, &Refusal{Kind: RefusedExcluded, Slug: slug, Model: model}
		}
		return model, nil
	}
	return Model{}, &Refusal{Kind: RefusedUnknown, Slug: slug, Known: known}
}

func (c Library) Resolve(recorded string) (Model, bool) {
	for _, model := range c.Models {
		if model.Slug() == recorded {
			return model, true
		}
	}
	var bare Model
	bareMatches := 0
	for _, model := range c.Models {
		if model.ID == recorded {
			bare, bareMatches = model, bareMatches+1
		}
	}
	return bare, bareMatches == 1
}

func (c Library) Default(subscription Subscription) (Model, error) {
	for _, model := range c.Models {
		if model.Subscription == subscription && model.Use == UseDefault {
			return model, nil
		}
	}
	return Model{}, fmt.Errorf("the model library has no default for the %s subscription", subscription)
}
