package main

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	shipped "boji/catalog"
	"boji/internal/konst"
	"boji/internal/llm/cred"
	"boji/internal/llm/models"
	"boji/internal/transport"
)

const modelsUsage = "usage: boji models [--discover] [--json]"

type modelReport struct {
	Slug         string   `json:"slug"`
	Provider     string   `json:"provider"`
	ID           string   `json:"id"`
	Subscription string   `json:"subscription"`
	Use          string   `json:"use"`
	Windows      []string `json:"windows"`
	Roles        []string `json:"roles,omitempty"`
	Reason       string   `json:"reason,omitempty"`
	File         string   `json:"file"`
}

type modelsReport struct {
	Subscriptions []string      `json:"subscriptions"`
	Defaults      []string      `json:"defaults"`
	Usable        int           `json:"usable"`
	Unbound       []string      `json:"unbound_roles,omitempty"`
	Models        []modelReport `json:"models"`
}

func modelCatalog() (models.Catalog, error) {
	layers, err := models.Layers(shipped.Files())
	if err != nil {
		return models.Catalog{}, err
	}
	return models.Load(layers)
}

func modelsVerb(args []string, out, errOut io.Writer, shade palette) int {
	discover, asJSON := false, false
	for _, arg := range args {
		switch arg {
		case "--discover":
			discover = true
		case jsonFlag:
			asJSON = true
		default:
			_, _ = fmt.Fprintf(errOut, "boji models: unknown flag %q, %s\n", arg, modelsUsage)
			return exitUsage
		}
	}
	catalog, err := modelCatalog()
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "boji models: %v\n", err)
		return exitUsage
	}
	if discover {
		return discoverModels(context.Background(), catalog, out)
	}
	report := modelsOf(catalog)
	if !asJSON {
		_, _ = fmt.Fprint(out, modelsText(catalog, report, shade))
		return exitOK
	}
	if err := writeJSON(out, report); err != nil {
		_, _ = fmt.Fprintf(errOut, "boji models: %v\n", err)
		return exitVerdict
	}
	return exitOK
}

func modelsOf(catalog models.Catalog) modelsReport {
	report := modelsReport{Models: make([]modelReport, 0, len(catalog.Models))}
	for _, spec := range catalog.Subscriptions {
		report.Subscriptions = append(report.Subscriptions, string(spec.ID))
	}
	bound, declared := map[string][]string{}, map[models.RoleID]bool{}
	for _, role := range catalog.Roles {
		slug := role.Model.Slug()
		bound[slug] = append(bound[slug], string(role.ID))
		declared[role.ID] = true
	}
	for _, id := range models.RoleIDs() {
		if !declared[id] {
			report.Unbound = append(report.Unbound, string(id))
		}
	}
	for _, model := range catalog.Models {
		report.Models = append(report.Models, modelReport{
			Slug:         model.Slug(),
			Provider:     string(model.Provider),
			ID:           model.ID,
			Subscription: string(model.Subscription),
			Use:          string(model.Use),
			Windows:      model.Windows,
			Roles:        bound[model.Slug()],
			Reason:       model.Reason,
			File:         model.File,
		})
		switch model.Use {
		case models.UseDefault:
			report.Defaults = append(report.Defaults, model.Slug())
			report.Usable++
		case models.UseAllowed:
			report.Usable++
		case models.UseExcluded:
		}
	}
	return report
}

func modelsText(catalog models.Catalog, report modelsReport, shade palette) string {
	count := strconv.Itoa(report.Usable) + " of " + strconv.Itoa(len(report.Models)) + " usable"
	var body strings.Builder
	body.WriteString(headline(strings.Join(report.Defaults, ", "), shade.settled(count), len(count)) + "\n")
	for _, spec := range catalog.Subscriptions {
		body.WriteString("\n")
		for _, line := range subscriptionLines(report, spec) {
			body.WriteString(line + "\n")
		}
	}
	label := "roles"
	for _, id := range report.Unbound {
		body.WriteString("\n")
		for _, line := range wrapped(label, id+" has nothing bound, so "+models.RoleID(id).What()+" takes the subscription default") {
			body.WriteString(line + "\n")
		}
		label = ""
	}
	return body.String()
}

func withRoles(model modelReport) string {
	if len(model.Roles) == 0 {
		return model.Slug
	}
	return model.Slug + " [" + strings.Join(model.Roles, " and ") + "]"
}

func subscriptionLines(report modelsReport, spec models.SubscriptionSpec) []string {
	var allowed, reasons []string
	excluded := map[string]int{}
	var lines []string
	label := string(spec.ID)
	for _, model := range report.Models {
		if model.Subscription != string(spec.ID) {
			continue
		}
		switch models.Use(model.Use) {
		case models.UseDefault:
			lines = append(lines, wrapped(label, withRoles(model)+" by default on --wire "+spec.Wire+", spends "+strings.Join(model.Windows, " and "))...)
			label = ""
		case models.UseAllowed:
			allowed = append(allowed, withRoles(model))
		case models.UseExcluded:
			if excluded[model.Reason] == 0 {
				reasons = append(reasons, model.Reason)
			}
			excluded[model.Reason]++
		}
	}
	if len(allowed) > 0 {
		lines = append(lines, wrapped(label, "also allowed, "+strings.Join(allowed, ", "))...)
		label = ""
	}
	for _, reason := range reasons {
		lines = append(lines, wrapped(label, strconv.Itoa(excluded[reason])+" excluded, "+reason)...)
		label = ""
	}
	return lines
}

func discoverModels(ctx context.Context, catalog models.Catalog, out io.Writer) int {
	store, client, err := discoveryStore()
	if err != nil {
		_, _ = fmt.Fprintf(out, "boji models: %v\n", err)
		return exitVerdict
	}
	defer func() { _ = store.Close() }()

	code := exitOK
	for _, spec := range catalog.Subscriptions {
		credential, err := cred.Lookup(spec.Wire)
		if err != nil {
			_, _ = fmt.Fprintf(out, "%s: %v\n", spec.ID, err)
			code = exitVerdict
			continue
		}
		row, present, err := store.Row(cred.Provider(spec.Wire))
		if err != nil || !present {
			_, _ = fmt.Fprintf(out, "%s: no credential, run boji login %s\n", spec.ID, spec.Wire)
			code = exitVerdict
			continue
		}
		served, err := models.Discover(ctx, client, models.Account{
			Subscription: spec.ID,
			AccountID:    row.Credential.Identity.AccountID,
			Token:        cred.NewManager(store, credential).Access,
		})
		if err != nil {
			_, _ = fmt.Fprintf(out, "%s: discovery failed under %s, so a short list is this pin rather than the account: %v\n",
				spec.ID, served.Pin, err)
			code = exitVerdict
			continue
		}
		for _, line := range catalog.Reconcile(served).Lines() {
			_, _ = fmt.Fprintln(out, line)
		}
	}
	return code
}

func discoveryStore() (*cred.Store, *transport.Client, error) {
	path, err := cred.Path()
	if err != nil {
		return nil, nil, err
	}
	store, err := cred.Open(path)
	if err != nil {
		return nil, nil, err
	}
	client, err := transport.New(transport.Config{
		AttemptTimeout: time.Duration(konst.TurnAttemptTimeoutMillis) * time.Millisecond,
		Concurrency:    1,
	})
	if err != nil {
		_ = store.Close()
		return nil, nil, err
	}
	return store, client, nil
}

func outsideTheCatalog(catalog models.Catalog, wire string) error {
	return fmt.Errorf(
		"the model catalog covers the subscriptions reached by --wire %s, and --wire %s spends an api key, which is money rather than a window",
		strings.Join(catalog.Wires(), " and --wire "), wire)
}

func selectModel(wire, want string) (models.Model, error) {
	catalog, err := modelCatalog()
	if err != nil {
		return models.Model{}, err
	}
	spec, carried := catalog.ForWire(wire)
	if !carried {
		return models.Model{}, outsideTheCatalog(catalog, wire)
	}
	if want == "" {
		return catalog.Default(spec.ID)
	}
	model, err := catalog.Select(want)
	if err != nil {
		return models.Model{}, err
	}
	if model.Subscription != spec.ID {
		return models.Model{}, fmt.Errorf("%s belongs to the %s subscription, and --wire %s reaches %s",
			model.Slug(), model.Subscription, wire, spec.ID)
	}
	return model, nil
}
