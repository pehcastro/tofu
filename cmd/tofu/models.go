package main

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"tofu/internal/konst"
	"tofu/internal/llm/cred"
	"tofu/internal/llm/models"
	"tofu/internal/subagent"
	"tofu/internal/sys"
	"tofu/internal/transport"
	shipped "tofu/library"
)

const (
	modelsUsage      = "usage: tofu models [reload] [--json]"
	registryFileName = "model-windows.json"
	registryDirMode  = 0o755
	shippedLayer     = "library"
)

type say func(format string, args ...any)

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

func refreshRegistry(ctx context.Context, client *transport.Client, said say) (models.Registry, error) {
	source := models.RegistrySource()
	path, err := registryPath()
	if err != nil {
		return models.Registry{}, err
	}
	body, err := models.FetchRegistry(ctx, client, source)
	if err != nil {
		return models.Registry{}, err
	}
	registry, err := models.ParseRegistry(body, source)
	if err != nil {
		return models.Registry{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), registryDirMode); err != nil {
		return models.Registry{}, err
	}
	if err := registry.Store(path); err != nil {
		return models.Registry{}, err
	}
	said("models.dev: context windows for %d models from %s, written to %s", len(registry.Windows), source, path)
	return registry, nil
}

type modelReport struct {
	Slug          string   `json:"slug"`
	Provider      string   `json:"provider"`
	ID            string   `json:"id"`
	Subscription  string   `json:"subscription"`
	Use           string   `json:"use"`
	Kind          string   `json:"kind"`
	Pays          string   `json:"pays"`
	Windows       []string `json:"windows"`
	ContextTokens int      `json:"context_tokens,omitempty"`
	WindowFrom    string   `json:"window_from,omitempty"`
	Roles         []string `json:"roles,omitempty"`
	Reason        string   `json:"reason,omitempty"`
	Layer         string   `json:"layer"`
	From          string   `json:"from,omitempty"`
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

func modelLibrary(dir string) (models.Library, error) {
	layers, err := models.Layers(shipped.Files(), dir)
	if err != nil {
		return models.Library{}, err
	}
	return models.Load(layers)
}

func modelsVerb(args []string, out, errOut io.Writer, shade palette) int {
	reload, asJSON := false, false
	for _, arg := range args {
		switch arg {
		case "reload":
			reload = true
		case "--discover", "--refresh":
			_, _ = fmt.Fprintf(out, "tofu models %s is now %s, which does both\n", arg, models.ReloadVerb)
			reload = true
		case jsonFlag:
			asJSON = true
		default:
			_, _ = fmt.Fprintf(errOut, "tofu models: unknown flag %q, %s\n", arg, modelsUsage)
			return exitUsage
		}
	}
	if reload {
		return reloadAccounts(context.Background(), out)
	}
	library, err := modelLibrary("")
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "tofu models: %v\n", err)
		return exitUsage
	}
	registry, err := modelRegistry()
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "tofu models: %v\n", err)
		return exitUsage
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
			Kind:          string(model.Kind),
			Pays:          string(model.Pays()),
			Windows:       model.Windows,
			ContextTokens: contextTokens,
			WindowFrom:    windowFrom,
			Roles:         bound[model.Slug()],
			Reason:        model.Reason,
			Layer:         model.Layer,
			From:          model.From,
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
	if lines := keyPaidLines(report); len(lines) > 0 {
		body.WriteString("\n")
		for _, line := range lines {
			body.WriteString(line + "\n")
		}
	}
	body.WriteString("\n")
	for _, line := range wrapped("windows", strconv.Itoa(report.Windowed)+" of "+strconv.Itoa(len(report.Models))+
		" models take a context window from "+report.Table+", and "+models.ReloadVerb+" reads the table again") {
		body.WriteString(line + "\n")
	}
	label := "roles"
	for _, id := range report.Unbound {
		body.WriteString("\n")
		role := models.RoleID(id)
		for _, line := range wrapped(label, role.Label()+": "+role.What()+". "+role.Unbound()) {
			body.WriteString(line + "\n")
		}
		label = ""
	}
	return body.String()
}

func withRoles(model modelReport) string {
	named := model.Slug + " (kind " + model.Kind + ", pays " + model.Pays
	if model.Layer != shippedLayer {
		named += ", from the " + model.Layer + " layer"
	}
	named += ")"
	if len(model.Roles) == 0 {
		return named
	}
	labels := make([]string, 0, len(model.Roles))
	for _, role := range model.Roles {
		labels = append(labels, models.RoleID(role).Label())
	}
	return named + " [" + strings.Join(labels, " and ") + "]"
}

func keyPaidLines(report modelsReport) []string {
	var lines []string
	label := "key"
	for _, model := range report.Models {
		if model.Subscription != "" {
			continue
		}
		lines = append(lines, wrapped(label, withRoles(model)+", use "+model.Use)...)
		label = ""
	}
	return lines
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

func reloadAccounts(ctx context.Context, out io.Writer) int {
	path, err := cred.Path()
	var store *cred.Store
	if err == nil {
		store, err = cred.Open(path)
	}
	if err != nil {
		_, _ = fmt.Fprintf(out, "%s: %v\n", models.ReloadVerb, err)
		return exitVerdict
	}
	defer func() { _ = store.Close() }()
	var accounts []models.Account
	for _, provider := range cred.AllProviders() {
		credential, err := cred.Lookup(string(provider))
		if err != nil {
			_, _ = fmt.Fprintf(out, "%s: %v\n", provider, err)
			continue
		}
		row, present, err := store.Row(provider)
		switch {
		case err != nil:
			_, _ = fmt.Fprintf(out, "%s: %v\n", provider, err)
		case !present:
			_, _ = fmt.Fprintf(out, "%s: not signed in, so nothing is asked of it; tofu login %s signs in\n", provider, provider)
		default:
			accounts = append(accounts, models.Account{
				Subscription: models.Subscription(provider),
				AccountID:    row.Credential.Identity.AccountID,
				Token:        cred.NewManager(store, credential).Access,
			})
		}
	}
	return reloadModels(ctx, accounts, out)
}

type reload struct {
	client   *transport.Client
	catalog  string
	registry models.Registry
	library  models.Library
	found    string
	said     say
}

func reloadModels(ctx context.Context, accounts []models.Account, out io.Writer) int {
	said := func(format string, args ...any) { _, _ = fmt.Fprintf(out, format+"\n", args...) }
	refused := func(err error) int {
		said("%s: %v", models.ReloadVerb, err)
		return exitVerdict
	}
	catalog, err := models.CatalogDir()
	if err != nil {
		return refused(err)
	}
	client, err := transport.New(transport.Config{
		AttemptTimeout: time.Duration(konst.TurnAttemptTimeoutMillis) * time.Millisecond,
		Concurrency:    1,
	})
	if err != nil {
		return refused(err)
	}
	dropShipped(catalog, said)
	registry, err := refreshRegistry(ctx, client, said)
	if err != nil {
		said("models.dev: %v, so the stored table stands", err)
		if registry, err = modelRegistry(); err != nil {
			return refused(err)
		}
	}
	library, err := modelLibrary("")
	if err != nil {
		return refused(err)
	}
	run := reload{client: client, catalog: catalog, registry: registry, library: library, found: time.Now().Format(time.DateOnly), said: said}
	clean := true
	for _, account := range accounts {
		clean = run.write(ctx, account) && clean
	}
	if !run.stillResolves() || !clean {
		return exitVerdict
	}
	return exitOK
}

func dropShipped(catalog string, said say) {
	files, _ := filepath.Glob(filepath.Join(catalog, "models", "*", "*.yaml"))
	for _, path := range files {
		name, _ := filepath.Rel(catalog, path)
		if _, err := fs.Stat(shipped.Files(), filepath.ToSlash(name)); err != nil {
			continue
		}
		if err := os.Remove(path); err != nil {
			said("catalog: %v", err)
			continue
		}
		said("catalog: removed %s, tofu ships %s now", path, filepath.ToSlash(name))
	}
}

func (r reload) write(ctx context.Context, account models.Account) bool {
	served, err := models.Discover(ctx, r.client, account)
	if err != nil {
		r.said("%s: the account list failed under %s: %v", account.Subscription, served.Pin, err)
		return false
	}
	plan := models.Plan(r.library.Reconcile(served, r.registry), r.registry, r.library, r.found)
	if err := plan.Write(r.catalog); err != nil {
		r.said("%s: %v", account.Subscription, err)
		return false
	}
	if len(plan) == 0 {
		r.said("%s: nothing new in the %d models the account serves under %s", account.Subscription, len(served.IDs), served.Pin)
	}
	for _, model := range plan {
		state := "new"
		if _, known := r.library.Resolve(model.Slug()); known {
			state = "changed"
		}
		r.said("%s: %s %s, use %s, %s, written to %s", account.Subscription, state, model.Slug(), model.Use,
			cmp.Or(model.Reason, model.From), filepath.Join(r.catalog, "models", string(model.Provider), model.ID+".yaml"))
	}
	return true
}

func (r reload) stillResolves() bool {
	library, err := modelLibrary("")
	if err != nil {
		r.said("%s: %v", models.ReloadVerb, err)
		return false
	}
	store, err := openSettings(".")
	if err != nil {
		r.said("%s: %v", models.ReloadVerb, err)
		return false
	}
	resolves := true
	for _, tier := range subagent.Tiers() {
		slug := strings.TrimSpace(store.Text(tier.Setting()))
		if slug == "" {
			continue
		}
		if _, err := library.Select(slug); err != nil {
			r.said("tier @%s: %v", tier, err)
			resolves = false
		}
	}
	return resolves
}

func outsideTheLibrary(library models.Library, wire string) error {
	return fmt.Errorf(
		"the model library covers the subscriptions reached by --wire %s, and --wire %s spends an api key, which is money rather than a window",
		strings.Join(library.Wires(), " and --wire "), wire)
}

func selectModel(wire, want string) (models.Model, error) {
	library, err := modelLibrary("")
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
