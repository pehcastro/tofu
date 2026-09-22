package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"tofu/internal/konst"
	"tofu/internal/llm/cred"
	"tofu/internal/llm/models"
	"tofu/internal/sys"
	"tofu/internal/transport"
	shipped "tofu/library"
)

const (
	modelsUsage      = "usage: tofu models [--discover] [--refresh] [--json]"
	registryFileName = "model-windows.json"
	registryDirMode  = 0o755
)

func registryPath() (string, error) {
	dir, err := sys.HomeConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, registryFileName), nil
}

func modelRegistry() (models.Registry, error) {
	path, err := registryPath()
	if err != nil {
		return models.ShippedRegistry()
	}
	return models.RegistryAt(path)
}

func refreshRegistry(ctx context.Context, out io.Writer) int {
	refusal := func(err error) int {
		_, _ = fmt.Fprintf(out, "%s: %v\n", models.RefreshVerb, err)
		return exitVerdict
	}
	source := models.RegistrySource()
	path, err := registryPath()
	if err != nil {
		return refusal(err)
	}
	client, err := transport.New(transport.Config{
		AttemptTimeout: time.Duration(konst.TurnAttemptTimeoutMillis) * time.Millisecond,
		Concurrency:    1,
	})
	if err != nil {
		return refusal(err)
	}
	body, err := models.FetchRegistry(ctx, client, source)
	if err != nil {
		return refusal(err)
	}
	registry, err := models.ParseRegistry(body, source)
	if err != nil {
		return refusal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), registryDirMode); err != nil {
		return refusal(err)
	}
	if err := registry.Store(path); err != nil {
		return refusal(err)
	}
	_, _ = fmt.Fprintf(out, "context windows from %s: %d, written to %s\n", source, len(registry.Windows), path)
	return exitOK
}

type modelReport struct {
	Slug          string   `json:"slug"`
	Provider      string   `json:"provider"`
	ID            string   `json:"id"`
	Subscription  string   `json:"subscription"`
	Use           string   `json:"use"`
	Windows       []string `json:"windows"`
	ContextTokens int      `json:"context_tokens,omitempty"`
	WindowFrom    string   `json:"window_from,omitempty"`
	Roles         []string `json:"roles,omitempty"`
	Reason        string   `json:"reason,omitempty"`
	File          string   `json:"file"`
}

type modelsReport struct {
	Subscriptions []string      `json:"subscriptions"`
	Defaults      []string      `json:"defaults"`
	Usable        int           `json:"usable"`
	Table         string        `json:"table"`
	Windowed      int           `json:"windowed"`
	Unbound       []string      `json:"unbound_roles,omitempty"`
	Models        []modelReport `json:"models"`
}

func modelLibrary() (models.Library, error) {
	layers, err := models.Layers(shipped.Files())
	if err != nil {
		return models.Library{}, err
	}
	return models.Load(layers)
}

func modelsVerb(args []string, out, errOut io.Writer, shade palette) int {
	discover, refresh, asJSON := false, false, false
	for _, arg := range args {
		switch arg {
		case "--discover":
			discover = true
		case "--refresh":
			refresh = true
		case jsonFlag:
			asJSON = true
		default:
			_, _ = fmt.Fprintf(errOut, "tofu models: unknown flag %q, %s\n", arg, modelsUsage)
			return exitUsage
		}
	}
	if refresh {
		return refreshRegistry(context.Background(), out)
	}
	library, err := modelLibrary()
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "tofu models: %v\n", err)
		return exitUsage
	}
	registry, err := modelRegistry()
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "tofu models: %v\n", err)
		return exitUsage
	}
	if discover {
		return discoverModels(context.Background(), library, registry, out)
	}
	report := modelsOf(library, registry)
	if !asJSON {
		_, _ = fmt.Fprint(out, modelsText(library, report, shade))
		return exitOK
	}
	if err := writeJSON(out, report); err != nil {
		_, _ = fmt.Fprintf(errOut, "tofu models: %v\n", err)
		return exitVerdict
	}
	return exitOK
}

func modelsOf(library models.Library, registry models.Registry) modelsReport {
	report := modelsReport{Table: registry.From, Models: make([]modelReport, 0, len(library.Models))}
	for _, spec := range library.Subscriptions {
		report.Subscriptions = append(report.Subscriptions, string(spec.ID))
	}
	bound, declared := map[string][]string{}, map[models.RoleID]bool{}
	for _, role := range library.Roles {
		slug := role.Model.Slug()
		bound[slug] = append(bound[slug], string(role.ID))
		declared[role.ID] = true
	}
	for _, id := range models.RoleIDs() {
		if !declared[id] {
			report.Unbound = append(report.Unbound, string(id))
		}
	}
	for _, model := range library.Models {
		contextTokens, windowFrom := models.WindowFor(model, registry, models.Served{})
		if contextTokens > 0 {
			report.Windowed++
		}
		report.Models = append(report.Models, modelReport{
			Slug:          model.Slug(),
			Provider:      string(model.Provider),
			ID:            model.ID,
			Subscription:  string(model.Subscription),
			Use:           string(model.Use),
			Windows:       model.Windows,
			ContextTokens: contextTokens,
			WindowFrom:    windowFrom,
			Roles:         bound[model.Slug()],
			Reason:        model.Reason,
			File:          model.File,
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

func modelsText(library models.Library, report modelsReport, shade palette) string {
	count := strconv.Itoa(report.Usable) + " of " + strconv.Itoa(len(report.Models)) + " usable"
	var body strings.Builder
	body.WriteString(headline(strings.Join(report.Defaults, ", "), shade.settled(count), len(count)) + "\n")
	for _, spec := range library.Subscriptions {
		body.WriteString("\n")
		for _, line := range subscriptionLines(report, spec) {
			body.WriteString(line + "\n")
		}
	}
	body.WriteString("\n")
	for _, line := range wrapped("windows", strconv.Itoa(report.Windowed)+" of "+strconv.Itoa(len(report.Models))+
		" models take a context window from "+report.Table+", and "+models.RefreshVerb+" reads the table again") {
		body.WriteString(line + "\n")
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
			lines = append(lines, wrapped(label, withRoles(model)+" by default on --wire "+spec.Wire+
				", a "+strconv.Itoa(model.ContextTokens)+" token window, spends "+strings.Join(model.Windows, " and "))...)
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

func discoverModels(ctx context.Context, library models.Library, registry models.Registry, out io.Writer) int {
	store, client, err := discoveryStore()
	if err != nil {
		_, _ = fmt.Fprintf(out, "tofu models: %v\n", err)
		return exitVerdict
	}
	defer func() { _ = store.Close() }()

	code := exitOK
	for _, spec := range library.Subscriptions {
		credential, err := cred.Lookup(string(spec.ID))
		if err != nil {
			_, _ = fmt.Fprintf(out, "%s: %v\n", spec.ID, err)
			code = exitVerdict
			continue
		}
		row, present, err := store.Row(credential.Provider)
		if err != nil {
			_, _ = fmt.Fprintf(out, "%s: %v\n", spec.ID, err)
			code = exitVerdict
			continue
		}
		if !present {
			_, _ = fmt.Fprintf(out, "%s: no credential, run tofu login %s\n", spec.ID, spec.ID)
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
		for _, line := range library.Reconcile(served, registry).Lines() {
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

func outsideTheLibrary(library models.Library, wire string) error {
	return fmt.Errorf(
		"the model library covers the subscriptions reached by --wire %s, and --wire %s spends an api key, which is money rather than a window",
		strings.Join(library.Wires(), " and --wire "), wire)
}

func selectModel(wire, want string) (models.Model, error) {
	library, err := modelLibrary()
	if err != nil {
		return models.Model{}, err
	}
	spec, carried := library.ForWire(wire)
	if !carried {
		return models.Model{}, outsideTheLibrary(library, wire)
	}
	if want == "" {
		return library.Default(spec.ID)
	}
	model, err := library.Select(want)
	if err != nil {
		return models.Model{}, err
	}
	if model.Subscription != spec.ID {
		return models.Model{}, fmt.Errorf("%s belongs to the %s subscription, and --wire %s reaches %s",
			model.Slug(), model.Subscription, wire, spec.ID)
	}
	return model, nil
}
