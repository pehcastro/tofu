package models

import (
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"tofu/internal/llm"
	"tofu/internal/sys"
)

const (
	catalogLayer = "catalog"
	ReloadVerb   = "tofu models reload"
)

type CatalogPlan []Model

func CatalogDir() (string, error) {
	home, err := sys.HomeConfigDir()
	if err != nil {
		return "", err
	}
	return sys.Join(home, catalogLayer), nil
}

func Plan(reconciled Reconciliation, registry Registry, library Library, found string) CatalogPlan {
	account := reconciled.Served
	stopped := "the " + string(account.Subscription) + " account no longer serves it"
	served := make(map[string]bool, len(account.IDs))
	for _, id := range account.IDs {
		served[id] = true
	}
	var plan CatalogPlan
	for _, id := range reconciled.Unknown {
		plan = append(plan, library.discovered(account, id, registry, found))
	}
	for _, model := range library.Models {
		if model.Subscription != account.Subscription || model.From == "" {
			continue
		}
		switch {
		case !served[model.ID] && model.Use != UseExcluded:
			model.Use, model.Reason = UseExcluded, stopped
			plan = append(plan, model)
		case served[model.ID] && model.Reason == stopped:
			plan = append(plan, library.discovered(account, model.ID, registry, model.Found))
		}
	}
	return plan
}

func (c Library) discovered(account Served, id string, registry Registry, found string) Model {
	model := Model{
		ID:           id,
		Subscription: account.Subscription,
		Use:          UseAllowed,
		From:         "listed by the " + string(account.Subscription) + " account under " + account.Pin,
		Found:        found,
	}
	for _, spec := range c.Subscriptions {
		if spec.ID == account.Subscription {
			model.Provider = spec.Provider
		}
	}
	facts, listed := registry.Fact(model.VendorSlug())
	if listed && !facts.ToolCalls {
		model.Use, model.Reason = UseExcluded, "models.dev lists "+model.VendorSlug()+" without tool calls, and tofu drives every model through tools"
	}
	if sibling, has := c.newestSibling(model); has {
		model.Efforts, model.Vision = sibling.Efforts, sibling.Vision
		model.From += ", efforts and vision as " + sibling.Slug()
		return model
	}
	if !listed {
		return model
	}
	model.Vision = VisionBlind
	if facts.Images {
		model.Vision = VisionSees
	}
	for _, raw := range facts.Efforts {
		if effort, err := llm.ParseEffort(raw); err == nil && facts.Reasoning {
			model.Efforts = append(model.Efforts, effort)
		}
	}
	model.From += ", efforts and vision from " + registry.From
	return model
}

func (c Library) newestSibling(model Model) (Model, bool) {
	family, _ := familyOf(model.ID)
	var newest Model
	var newestVersion []int
	found := false
	for _, candidate := range c.Models {
		name, version := familyOf(candidate.ID)
		if candidate.Subscription != model.Subscription || candidate.ID == model.ID || candidate.Use == UseExcluded || name != family {
			continue
		}
		if !found || slices.Compare(version, newestVersion) > 0 {
			newest, newestVersion, found = candidate, version, true
		}
	}
	return newest, found
}

func familyOf(id string) (string, []int) {
	var words []string
	var version []int
	for _, part := range strings.Split(id, "-") {
		switch {
		case strings.Trim(part, "0123456789.") != "":
			words = append(words, part)
		case len(part) != datedSuffixDigits:
			for _, digits := range strings.Split(part, ".") {
				number, _ := strconv.Atoi(digits)
				version = append(version, number)
			}
		}
	}
	return strings.Join(words, "-"), version
}

func (p CatalogPlan) Write(dir string) error {
	for _, model := range p {
		name := string(model.Provider) + "/" + model.ID + ".yaml"
		if !model.Provider.valid() || model.ID == "" || strings.HasPrefix(model.ID, ".") || strings.ContainsAny(model.ID, `/\:`) || !filepath.IsLocal(name) {
			return fmt.Errorf("models: %q is not a name the catalog can hold as a file", model.VendorSlug())
		}
	}
	for _, model := range p {
		path := filepath.Join(dir, modelsDir, string(model.Provider), model.ID+".yaml")
		var body strings.Builder
		for _, field := range [][2]string{
			{"subscription", string(model.Subscription)},
			{"use", string(model.Use)},
			{"reason", model.Reason},
			{"vision", string(model.Vision)},
			{"efforts", llm.EffortList(model.Efforts)},
			{"from", model.From},
			{"found", model.Found},
		} {
			if field[1] != "" {
				body.WriteString(field[0] + ": " + field[1] + "\n")
			}
		}
		if err := sys.WriteFile(path, []byte(body.String()), writtenFileMode); err != nil {
			return err
		}
	}
	return nil
}
