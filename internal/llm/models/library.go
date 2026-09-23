package models

import (
	"fmt"
	"strings"
)

type Provider string

const (
	Anthropic Provider = "anthropic"
	OpenAI    Provider = "openai"
)

func (p Provider) valid() bool {
	switch p {
	case Anthropic, OpenAI:
		return true
	}
	return false
}

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
	Reason       string
	File         string
}

func (m Model) Slug() string {
	if m.Subscription != "" {
		return string(m.Subscription) + "/" + m.ID
	}
	return string(m.Provider) + "/" + m.ID
}

func (m Model) VendorSlug() string { return string(m.Provider) + "/" + m.ID }

func (m Model) WindowText() string { return strings.Join(m.Windows, " and ") }

type Library struct {
	Subscriptions []SubscriptionSpec
	Models        []Model
	Roles         []Role
	Broken        []Broken
}

func (c Library) wireFor(id Subscription) string {
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
		return "the model library has no " + r.Slug + ", it has " + strings.Join(r.Known, ", ")
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
