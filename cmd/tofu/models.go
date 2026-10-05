package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/interface/cli"
	"tofu/internal/konst"
	"tofu/internal/llm/cred"
	"tofu/internal/llm/models"
	settingspkg "tofu/internal/settings"
	"tofu/internal/subagent"
	"tofu/internal/sys"
	"tofu/internal/transport"
	"tofu/internal/widget"
	shipped "tofu/library"
)

const (
	modelsUsage      = "tofu models [reload] [--json]"
	registryFileName = "model-windows.json"
	registryDirMode  = 0o755
	shippedLayer     = "library"
	tokensPerMillion = 1_000_000
)

const (
	sourceReloaded    = "reloaded"
	sourceRefused     = "refused"
	sourceFailed      = "failed"
	sourceNotSignedIn = "not signed in"
)

const (
	modelAdded   = "added"
	modelChanged = "changed"
	modelDropped = "dropped"
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

func refreshRegistry(ctx context.Context, client *transport.Client) (models.Registry, error) {
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
	return registry, registry.Store(path)
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
	Notice        string   `json:"notice,omitempty"`
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
	Published     int           `json:"published_windows"`
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

func verbFailed(out, errOut io.Writer, verb string, asJSON bool, err error) int {
	if asJSON {
		_ = writeJSON(out, cli.Envelope{Verb: verb, At: time.Now(), Problems: []cli.Problem{{What: err.Error()}}})
		return exitVerdict
	}
	page := cli.Detect(errOut, os.Environ())
	_ = page.Print(errOut, page.ErrorLine("tofu "+verb+": "+err.Error(), ""))
	return exitVerdict
}

func modelsVerb(args []string, out, errOut io.Writer) int {
	reload, asJSON := false, jsonAsked(args)
	for _, arg := range args {
		switch arg {
		case "reload":
			reload = true
		case "--discover", "--refresh":
			_, _ = fmt.Fprintf(errOut, "tofu models %s is now %s\n", arg, models.ReloadVerb)
			reload = true
		case jsonFlag:
		default:
			return verbOutput{verb: "models", usageLine: modelsUsage, asJSON: asJSON, out: out, errOut: errOut}.usage(errors.New("unknown flag " + strconv.Quote(arg)))
		}
	}
	if reload {
		report, err := reloadSignedIn(context.Background())
		return showReload(out, errOut, asJSON, report, err)
	}
	library, err := modelLibrary("")
	if err != nil {
		return verbFailed(out, errOut, "models", asJSON, err)
	}
	registry, err := modelRegistry()
	if err != nil {
		return verbFailed(out, errOut, "models", asJSON, err)
	}
	report := modelsOf(library, registry)
	return show(out, asJSON, cli.Envelope{Verb: "models", OK: true, At: time.Now(), Data: report},
		func(page cli.Page) []string { return modelsLines(page, report) })
}

func show(out io.Writer, asJSON bool, envelope cli.Envelope, lines func(cli.Page) []string) int {
	var err error
	if asJSON {
		err = writeJSON(out, envelope)
	} else {
		page := cli.Detect(out, os.Environ())
		err = page.Print(out, lines(page))
	}
	if err != nil || !envelope.OK {
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
		switch {
		case registry.Window(model.VendorSlug()) > 0:
			report.Windowed++
		case contextTokens > 0:
			report.Published++
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
			Notice:        model.Notice,
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

func modelsLines(page cli.Page, report modelsReport) []string {
	lines := page.Title("Models", []string{strconv.Itoa(len(report.Models)) + " known"}, cli.Verdict{Mark: cli.Done, Text: strconv.Itoa(report.Usable) + " usable"})
	bound := map[models.RoleID]string{}
	for _, subscription := range append(slices.Clone(report.Subscriptions), "") {
		var rows []cli.Row
		var notices []string
		for _, model := range report.Models {
			if model.Subscription == subscription {
				rows = append(rows, modelRow(model))
				if model.Notice != "" && !slices.Contains(notices, model.Notice) {
					notices = append(notices, model.Notice)
				}
			}
			for _, role := range model.Roles {
				bound[models.RoleID(role)] = model.Slug
			}
		}
		if len(rows) > 0 {
			lines = append(lines, "", page.Section(cmp.Or(subscription, "api key"), cli.Verdict{}))
			lines = append(lines, cli.Indent(page.Rows(rows)...)...)
		}
		for _, notice := range notices {
			lines = append(lines, cli.Indent(page.Glyph(cli.Warn)+" "+notice)...)
		}
	}
	var roles []cli.Row
	for _, id := range models.RoleIDs() {
		row := cli.Row{Mark: cli.Active, Cells: []string{id.Label(), bound[id]}}
		if row.Cells[1] == "" {
			row.Mark, row.Cells[1] = cli.Idle, "unbound"
		}
		roles = append(roles, row)
	}
	lines = append(lines, "", page.Section("roles", cli.Verdict{}))
	lines = append(lines, cli.Indent(page.Rows(roles)...)...)
	lines = append(lines, "", page.Section("windows", cli.Verdict{}))
	lines = append(lines, cli.Indent(page.Facts([]cli.Fact{
		{Label: "table", Text: report.Table},
		{Label: "listed", Text: nonZero(report.Windowed)},
		{Label: "published", Text: nonZero(report.Published)},
	})...)...)
	return append(lines, cli.Indent(page.Hint(models.ReloadVerb))...)
}

func nonZero(count int) string {
	if count == 0 {
		return ""
	}
	return strconv.Itoa(count)
}

func modelRow(model modelReport) cli.Row {
	var detail []string
	for _, role := range model.Roles {
		detail = append(detail, models.RoleID(role).Label())
	}
	if model.Layer != shippedLayer {
		detail = append(detail, model.Layer+" layer")
	}
	row := cli.Row{Cells: []string{model.Slug, model.Use, windowShort(model.ContextTokens)}, Detail: strings.Join(detail, " · ")}
	switch {
	case model.Notice != "":
		row.Mark = cli.Warn
	case models.Use(model.Use) == models.UseDefault:
		row.Mark = cli.Active
	case models.Use(model.Use) == models.UseAllowed:
		row.Mark = cli.Done
	case models.Use(model.Use) == models.UseExcluded:
		row.Mark = cli.Idle
	default:
		panic("tofu: unknown model use " + model.Use)
	}
	return row
}

func windowShort(tokens int) string {
	switch {
	case tokens == 0:
		return ""
	case tokens >= tokensPerMillion:
		return strings.TrimRight(strings.TrimRight(strconv.FormatFloat(float64(tokens)/tokensPerMillion, 'f', 2, 64), "0"), ".") + "M"
	}
	return widget.Count(tokens)
}

type modelReload struct {
	ContextWindows int            `json:"context_windows"`
	Table          string         `json:"table"`
	TableError     string         `json:"table_error,omitempty"`
	Unshadowed     []string       `json:"removed_from_catalog,omitempty"`
	Sources        []reloadSource `json:"sources"`
	Unresolved     []string       `json:"unresolved_tiers,omitempty"`
	Versions       versionCheck   `json:"versions"`
}

type versionCheck struct {
	Raised  []settingspkg.RaisedVersion `json:"raised,omitempty"`
	Skipped string                      `json:"skipped,omitempty"`
	Error   string                      `json:"error,omitempty"`
}

type reloadSource struct {
	Source  string        `json:"source"`
	State   string        `json:"state"`
	Served  int           `json:"served"`
	Changes []modelChange `json:"changes"`
	Failure string        `json:"failure,omitempty"`
	Error   string        `json:"error,omitempty"`
	Hint    string        `json:"hint,omitempty"`
}

type modelChange struct {
	Model  string `json:"model"`
	Slug   string `json:"slug"`
	Change string `json:"change"`
	Use    string `json:"use"`
	Reason string `json:"reason,omitempty"`
	File   string `json:"file"`
}

func (c modelChange) mark() cli.Mark {
	switch c.Change {
	case modelAdded:
		return cli.Added
	case modelChanged:
		return cli.Changed
	case modelDropped:
		return cli.Removed
	}
	panic("tofu: unknown model change " + c.Change)
}

func (s reloadSource) failed(what string, err error) reloadSource {
	s.State, s.Failure, s.Error = sourceFailed, what, err.Error()
	var refused *transport.Error
	if errors.As(err, &refused) && refused.Status != 0 {
		s.State, s.Failure = sourceRefused, "model list refused ("+strconv.Itoa(refused.Status)+")"
	}
	switch transport.KindOf(err) {
	case transport.KindAuth, transport.KindMissingCredential:
		s.Hint = loginHint(s.Source)
	}
	return s
}

func reloadAccounts(ctx context.Context, out io.Writer) int {
	report, err := reloadSignedIn(ctx)
	return showReload(out, out, false, report, err)
}

func reloadSignedIn(ctx context.Context) (modelReload, error) {
	path, err := cred.Path()
	var store *cred.Store
	if err == nil {
		store, err = cred.Open(path)
	}
	if err != nil {
		return modelReload{}, err
	}
	defer func() { _ = store.Close() }()
	client, err := reloadClient()
	if err != nil {
		return modelReload{}, err
	}
	checked := raiseVersions(ctx, client)
	var accounts []models.Account
	var settled []reloadSource
	versions, _ := subFingerprint(".")
	for _, provider := range cred.AllProviders() {
		source := reloadSource{Source: string(provider), State: sourceNotSignedIn, Hint: loginHint(string(provider))}
		credential, err := cred.Lookup(string(provider))
		if err != nil {
			settled = append(settled, source.failed("sign-in unreadable", err))
			continue
		}
		row, present, err := store.Row(provider)
		switch {
		case err != nil:
			settled = append(settled, source.failed("sign-in unreadable", err))
		case !present:
			settled = append(settled, source)
		default:
			accounts = append(accounts, models.Account{
				Subscription:  models.Subscription(provider),
				AccountID:     row.Credential.Identity.AccountID,
				Token:         cred.NewManager(store, credential).Access,
				ClientVersion: clientVersion(versions, provider),
			})
		}
	}
	report, err := reloadModels(ctx, client, accounts, settled)
	report.Versions = checked
	return report, err
}

func raiseVersions(ctx context.Context, client *transport.Client) versionCheck {
	latest, err := settingspkg.LatestFingerprint(ctx, client, settingspkg.NpmRegistry())
	if err != nil {
		return versionCheck{Skipped: "unreachable, the version check was skipped", Error: err.Error()}
	}
	store, err := openSettings(".")
	if err != nil {
		return versionCheck{Skipped: "settings unreadable, the version check was skipped", Error: err.Error()}
	}
	raised, err := store.RaiseFingerprint(latest)
	if err != nil {
		return versionCheck{Raised: raised, Skipped: "settings unwritable, the rest of the version check was skipped", Error: err.Error()}
	}
	return versionCheck{Raised: raised}
}

type reloadRun struct {
	client   *transport.Client
	catalog  string
	registry models.Registry
	library  models.Library
	found    string
}

func reloadClient() (*transport.Client, error) {
	return transport.New(transport.Config{
		AttemptTimeout: time.Duration(konst.TurnAttemptTimeoutMillis) * time.Millisecond,
		Concurrency:    1,
	})
}

func reloadModels(ctx context.Context, client *transport.Client, accounts []models.Account, settled []reloadSource) (modelReload, error) {
	catalog, err := models.CatalogDir()
	if err != nil {
		return modelReload{}, err
	}
	report := modelReload{Unshadowed: dropShipped(catalog), Sources: settled}
	registry, err := refreshRegistry(ctx, client)
	if err != nil {
		report.TableError = err.Error()
		if registry, err = modelRegistry(); err != nil {
			return report, err
		}
	}
	report.ContextWindows, report.Table = len(registry.Windows), registry.From
	library, err := modelLibrary("")
	if err != nil {
		return report, err
	}
	run := reloadRun{client: client, catalog: catalog, registry: registry, library: library, found: time.Now().Format(time.DateOnly)}
	for _, account := range accounts {
		report.Sources = append(report.Sources, run.account(ctx, account))
	}
	slices.SortStableFunc(report.Sources, func(a, b reloadSource) int { return strings.Compare(a.Source, b.Source) })
	report.Unresolved, err = unresolvedTiers()
	return report, err
}

func dropShipped(catalog string) []string {
	var dropped []string
	files, _ := filepath.Glob(filepath.Join(catalog, "models", "*", "*.yaml"))
	for _, path := range files {
		name, _ := filepath.Rel(catalog, path)
		if _, err := fs.Stat(shipped.Files(), filepath.ToSlash(name)); err == nil && os.Remove(path) == nil {
			dropped = append(dropped, filepath.ToSlash(name))
		}
	}
	return dropped
}

func (r reloadRun) account(ctx context.Context, account models.Account) reloadSource {
	source := reloadSource{Source: string(account.Subscription), State: sourceReloaded}
	served, err := models.Discover(ctx, r.client, account)
	if err != nil {
		return source.failed("model list failed", err)
	}
	plan := models.Plan(r.library.Reconcile(served, r.registry), r.registry, r.library, r.found)
	if err := plan.Write(r.catalog); err != nil {
		return source.failed("catalog write failed", err)
	}
	source.Served = len(served.IDs)
	for _, model := range plan {
		change := modelChange{Model: model.ID, Slug: model.Slug(), Change: modelAdded, Use: string(model.Use), Reason: model.Reason,
			File: filepath.Join(r.catalog, "models", string(model.Provider), model.ID+".yaml")}
		if was, known := r.library.Resolve(model.Slug()); known {
			change.Change = modelChanged
			if model.Use == models.UseExcluded && was.Use != models.UseExcluded {
				change.Change = modelDropped
			}
		}
		source.Changes = append(source.Changes, change)
	}
	return source
}

func unresolvedTiers() ([]string, error) {
	library, err := modelLibrary("")
	if err != nil {
		return nil, err
	}
	store, err := openSettings(".")
	if err != nil {
		return nil, err
	}
	var unresolved []string
	for _, tier := range subagent.Tiers() {
		if slug := strings.TrimSpace(store.Text(tier.Setting())); slug != "" {
			if _, err := library.Select(slug); err != nil {
				unresolved = append(unresolved, "tier @"+string(tier)+": "+err.Error())
			}
		}
	}
	return unresolved, nil
}

func (r modelReload) envelope() cli.Envelope {
	var problems []cli.Problem
	for _, source := range r.Sources {
		if source.Failure != "" {
			problems = append(problems, cli.Problem{What: source.Source + ": " + source.Failure, Hint: source.Hint})
		}
	}
	for _, tier := range r.Unresolved {
		problems = append(problems, cli.Problem{What: tier})
	}
	return cli.Envelope{Verb: "models reload", OK: len(problems) == 0, At: time.Now(), Data: r, Problems: problems}
}

func showReload(out, errOut io.Writer, asJSON bool, report modelReload, err error) int {
	if err != nil {
		return verbFailed(out, errOut, "models reload", asJSON, err)
	}
	return show(out, asJSON, report.envelope(), func(page cli.Page) []string { return reloadLines(page, report) })
}

func reloadLines(page cli.Page, report modelReload) []string {
	counts := map[cli.Mark]int{cli.Fail: len(report.Unresolved)}
	for _, source := range report.Sources {
		for _, change := range source.Changes {
			counts[change.mark()]++
		}
		if source.Failure != "" {
			counts[cli.Fail]++
		}
	}
	said := countsSaid(counts, []markNoun{{cli.Added, "new"}, {cli.Changed, "changed"}, {cli.Removed, "dropped"}, {cli.Fail, "failed"}})
	if raised := len(report.Versions.Raised); raised > 0 {
		noun := " versions raised"
		if raised == 1 {
			noun = " version raised"
		}
		said = strings.TrimPrefix(said+" · "+strconv.Itoa(raised)+noun, " · ")
	}
	verdict := cli.Verdict{Mark: cli.Done, Text: cmp.Or(said, "nothing changed")}
	if counts[cli.Fail] > 0 {
		verdict.Mark = cli.Warn
	}
	table := cli.Verdict{Mark: cli.Done, Text: strconv.Itoa(report.ContextWindows) + " context windows"}
	if report.TableError != "" {
		table = cli.Verdict{Mark: cli.Warn, Text: "unreachable, the stored table stands"}
	}
	lines := append(page.Title("Model reload", nil, verdict), "", page.Status("models.dev", table))
	for _, raised := range report.Versions.Raised {
		lines = append(lines, page.Status("npm", cli.Verdict{Mark: cli.Done, Text: raised.Key + " raised from " + raised.From + " to " + raised.To}))
	}
	if report.Versions.Skipped != "" {
		lines = append(lines, page.Status("npm", cli.Verdict{Mark: cli.Warn, Text: report.Versions.Skipped}))
	}
	if removed := len(report.Unshadowed); removed > 0 {
		noun := " files removed"
		if removed == 1 {
			noun = " file removed"
		}
		lines = append(lines, page.Status("catalog", cli.Verdict{Mark: cli.Done, Text: strconv.Itoa(removed) + noun}))
	}
	for _, source := range report.Sources {
		lines = append(lines, "")
		switch source.State {
		case sourceReloaded:
			rows := make([]cli.Row, len(source.Changes))
			for i, change := range source.Changes {
				rows[i] = cli.Row{Mark: change.mark(), Cells: []string{change.Model}, Detail: change.Use}
			}
			lines = append(lines, page.Section(source.Source, cli.Verdict{Mark: cli.Done, Text: strconv.Itoa(source.Served) + " served"}))
			lines = append(lines, cli.Indent(page.Rows(rows)...)...)
		case sourceNotSignedIn:
			lines = append(lines, page.Section(source.Source, cli.Verdict{Mark: cli.Idle, Text: source.State}))
			lines = append(lines, cli.Indent(page.Hint(source.Hint))...)
		case sourceRefused, sourceFailed:
			lines = append(lines, page.Section(source.Source, cli.Verdict{Mark: cli.Fail, Text: source.Failure}))
			if source.Hint != "" {
				lines = append(lines, cli.Indent(page.Hint(source.Hint))...)
			}
		default:
			panic("tofu: unknown reload state " + source.State)
		}
	}
	if len(report.Unresolved) > 0 {
		lines = append(lines, "")
	}
	for _, tier := range report.Unresolved {
		lines = append(lines, page.ErrorLine(tier, "")...)
	}
	return lines
}

type markNoun struct {
	mark cli.Mark
	noun string
}

func countsSaid(counts map[cli.Mark]int, nouns []markNoun) string {
	var parts []string
	for _, each := range nouns {
		if counts[each.mark] > 0 {
			parts = append(parts, strconv.Itoa(counts[each.mark])+" "+each.noun)
		}
	}
	return strings.Join(parts, " · ")
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
