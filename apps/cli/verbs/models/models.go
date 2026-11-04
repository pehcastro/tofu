package models

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

	"tofu/apps/cli/output"
	cli "tofu/apps/cli/page"
	"tofu/apps/cli/wiring"
	"tofu/internal/agent/subagents"
	"tofu/internal/cli/render"
	"tofu/internal/host"
	"tofu/internal/judge/gate"
	"tofu/internal/konst"
	"tofu/internal/models/classifiers"
	jevcatalog "tofu/internal/models/classifiers/catalog"
	"tofu/internal/models/classifiers/core"
	cred "tofu/internal/models/llm/accounts"
	models "tofu/internal/models/llm/catalog"
	"tofu/internal/models/transport"
	settingspkg "tofu/internal/store/settings"
	"tofu/internal/sys"
	shipped "tofu/library"
)

const (
	Usage           = "tofu models [reload] [--json], or tofu models bind <orchestrator|classifier|sub-agent> <provider/model> [--json]"
	registryDirMode = 0o755
	shippedLayer    = "library"
)

const (
	SourceReloaded    = "reloaded"
	SourceRefused     = "refused"
	SourceFailed      = "failed"
	SourceNotSignedIn = "not signed in"
)

const (
	modelAdded   = "added"
	modelChanged = "changed"
	modelDropped = "dropped"
)

func refreshRegistry(ctx context.Context, client *transport.Client) (models.Registry, error) {
	source := models.RegistrySource()
	path, err := wiring.RegistryPath()
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

func verbFailed(out, errOut io.Writer, verb string, asJSON bool, err error) int {
	if asJSON {
		_ = output.WriteJSON(out, cli.Envelope{Verb: verb, At: time.Now(), Problems: []cli.Problem{{What: err.Error()}}})
		return output.ExitVerdict
	}
	page := cli.Detect(errOut, os.Environ())
	_ = page.Print(errOut, page.ErrorLine("tofu "+verb+": "+err.Error(), ""))
	return output.ExitVerdict
}

func bindVerb(args []string, out, errOut io.Writer) int {
	o := output.Verb{Name: "models bind", UsageLine: Usage, AsJSON: output.JSONAsked(args), Out: out, ErrOut: errOut}
	args = slices.DeleteFunc(slices.Clone(args), func(arg string) bool { return arg == output.JSONFlag })
	if len(args) != 2 {
		return o.Usage(errors.New("models bind takes a role and a model written provider/name"))
	}
	role, slug := models.RoleID(args[0]), args[1]
	library, err := wiring.ModelLibrary("")
	if err != nil {
		return o.Fail(err)
	}
	if _, err := library.Fits(role, slug); err != nil {
		return o.Fail(err)
	}
	home, err := sys.HomeConfigDir()
	if err != nil {
		return o.Fail(err)
	}
	if err := models.BindRole(home, role, slug); err != nil {
		return o.Fail(err)
	}
	file := models.RoleFile(home, role)
	return o.Receipt(host.WriteReceipt{Changes: []host.FileChange{{Change: host.ChangeChanged, What: string(role) + " now runs " + slug, File: file}}, Undo: "delete " + file})
}

func Verb(args []string, out, errOut io.Writer) int {
	if len(args) > 0 && args[0] == "bind" {
		return bindVerb(args[1:], out, errOut)
	}
	reload, asJSON := false, output.JSONAsked(args)
	for _, arg := range args {
		switch arg {
		case "reload":
			reload = true
		case "--discover", "--refresh":
			_, _ = fmt.Fprintf(errOut, "tofu models %s is now %s\n", arg, models.ReloadVerb)
			reload = true
		case output.JSONFlag:
		default:
			return output.Verb{Name: "models", UsageLine: Usage, AsJSON: asJSON, Out: out, ErrOut: errOut}.Usage(errors.New("unknown flag " + strconv.Quote(arg)))
		}
	}
	if reload {
		held := wiring.HoldStore()
		defer held.Close()
		report, err := reloadSignedIn(context.Background(), held, jevPinRaised)
		code := showReload(out, errOut, asJSON, report, err)
		wiring.StampReload(code)
		return code
	}
	library, err := wiring.ModelLibrary("")
	if err != nil {
		return verbFailed(out, errOut, "models", asJSON, err)
	}
	registry, err := wiring.ModelRegistry()
	if err != nil {
		return verbFailed(out, errOut, "models", asJSON, err)
	}
	report := modelsOf(library, registry)
	problems := make([]cli.Problem, len(library.Broken))
	for i, refused := range library.Broken {
		problems[i] = cli.Problem{What: "refused " + refused.Error()}
	}
	return output.Show(out, asJSON, cli.Envelope{Verb: "models", OK: len(problems) == 0, At: time.Now(), Data: report, Problems: problems},
		func(page cli.Page) []string {
			lines := modelsLines(page, report)
			for _, problem := range problems {
				lines = append(lines, page.ErrorLine(problem.What, "")...)
			}
			return lines
		})
}

func modelsOf(library models.Library, registry models.Registry) host.ModelsReport {
	report := host.ModelsReport{Table: registry.From, Models: make([]host.ModelReport, 0, len(library.Models))}
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
		report.Models = append(report.Models, host.ModelReport{
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

func modelsLines(page cli.Page, report host.ModelsReport) []string {
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
		{Label: "listed", Text: output.NonZero(report.Windowed)},
		{Label: "published", Text: output.NonZero(report.Published)},
	})...)...)
	return append(lines, cli.Indent(page.Hint(models.ReloadVerb))...)
}

func modelRow(model host.ModelReport) cli.Row {
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
	case tokens >= wiring.TokensPerMillion:
		return strings.TrimRight(strings.TrimRight(strconv.FormatFloat(float64(tokens)/wiring.TokensPerMillion, 'f', 2, 64), "0"), ".") + "M"
	}
	return render.Count(tokens)
}

type ModelReload struct {
	host.ModelReload
	Jev jevcatalog.PinCheck `json:"jev,omitzero"`
}

func modelChangeMark(c host.ModelChange) cli.Mark {
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

func failedSource(s host.ReloadSource, what string, err error) host.ReloadSource {
	s.State, s.Failure, s.Error = SourceFailed, what, err.Error()
	var refused *transport.Error
	if errors.As(err, &refused) && refused.Status != 0 {
		s.State, s.Failure = SourceRefused, "model list refused ("+strconv.Itoa(refused.Status)+")"
	}
	switch transport.KindOf(err) {
	case transport.KindAuth, transport.KindMissingCredential:
		s.Hint = wiring.LoginHint(s.Source)
	}
	return s
}

func Reload(ctx context.Context, h wiring.HeldStore, out io.Writer) int {
	report, err := reloadSignedIn(ctx, h, jevPinChecked)
	return showReload(out, out, false, report, err)
}

type jevPinning int

const (
	jevPinChecked jevPinning = iota
	jevPinRaised
)

func checkJevPin(ctx context.Context, keys core.Keyring, listing *transport.Client, pinning jevPinning) jevcatalog.PinCheck {
	route, err := classifiers.Resolve(".", keys)
	if route.Model.Provider == "" {
		return jevcatalog.PinCheck{Skipped: "the pin stays", Error: err.Error()}
	}
	check, newest := route.CheckPin(ctx, listing)
	switch pinning {
	case jevPinChecked:
		return check
	case jevPinRaised:
	default:
		panic("tofu: unknown jev pinning " + strconv.Itoa(int(pinning)))
	}
	if check.Newer == "" {
		return check
	}
	if err := newest.Store(); err != nil {
		check.Skipped, check.Error = "the pin could not be written, it stays at "+check.From, err.Error()
		return check
	}
	check.To = newest.Model
	dir, err := sys.CalibrationDir()
	if err != nil {
		check.Shadowed = append(check.Shadowed, "calibration locks unreadable: "+err.Error())
		return check
	}
	names, _ := sys.ListFiles(dir, ".lock")
	for _, name := range names {
		path := filepath.Join(dir, name)
		lock, err := gate.LoadLock(path)
		switch {
		case err != nil:
			check.Shadowed = append(check.Shadowed, err.Error())
		case !route.Serves(newest, lock.Build):
			check.Shadowed = append(check.Shadowed, fmt.Sprintf("%s@%d runs in shadow until %s names a %s build, it names %s", lock.Rule, lock.RuleVersion, path, newest.Model, lock.Build))
		}
	}
	return check
}

func reloadSignedIn(ctx context.Context, held wiring.HeldStore, pinning jevPinning) (ModelReload, error) {
	if held.Err != nil {
		return ModelReload{}, held.Err
	}
	store := held.Store
	client, err := ReloadClient()
	if err != nil {
		return ModelReload{}, err
	}
	checked, pinned := raiseVersions(ctx, client), checkJevPin(ctx, held.Keys(), client, pinning)
	var accounts []models.Account
	var settled []host.ReloadSource
	versions, _ := wiring.SubFingerprint(".")
	for _, provider := range cred.AllProviders() {
		source := host.ReloadSource{Source: string(provider), State: SourceNotSignedIn, Hint: wiring.LoginHint(string(provider))}
		credential, err := cred.Lookup(string(provider))
		if err != nil {
			settled = append(settled, failedSource(source, "sign-in unreadable", err))
			continue
		}
		row, present, err := store.Row(provider)
		switch {
		case err != nil:
			settled = append(settled, failedSource(source, "sign-in unreadable", err))
		case !present:
			settled = append(settled, source)
		default:
			accounts = append(accounts, models.Account{
				Subscription:  models.Subscription(provider),
				AccountID:     row.Credential.Identity.AccountID,
				Token:         cred.NewManager(store, credential).Access,
				ClientVersion: wiring.ClientVersion(versions, provider),
			})
		}
	}
	report, err := ReloadModels(ctx, client, accounts, settled)
	report.Versions, report.Jev = checked, pinned
	return report, err
}

func raiseVersions(ctx context.Context, client *transport.Client) host.VersionCheck {
	latest, err := settingspkg.LatestFingerprint(ctx, client, settingspkg.NpmRegistry())
	if err != nil {
		return host.VersionCheck{Skipped: "unreachable, the version check was skipped", Error: err.Error()}
	}
	store, err := settingspkg.OpenFor(".")
	if err != nil {
		return host.VersionCheck{Skipped: "settings unreadable, the version check was skipped", Error: err.Error()}
	}
	raised, err := store.RaiseFingerprint(latest)
	if err != nil {
		return host.VersionCheck{Raised: raised, Skipped: "settings unwritable, the rest of the version check was skipped", Error: err.Error()}
	}
	return host.VersionCheck{Raised: raised}
}

type reloadRun struct {
	client   *transport.Client
	catalog  string
	registry models.Registry
	library  models.Library
	found    string
}

func ReloadClient() (*transport.Client, error) {
	return transport.New(transport.Config{
		AttemptTimeout: time.Duration(konst.TurnAttemptTimeoutMillis) * time.Millisecond,
		Concurrency:    1,
	})
}

func ReloadModels(ctx context.Context, client *transport.Client, accounts []models.Account, settled []host.ReloadSource) (ModelReload, error) {
	catalog, err := models.CatalogDir()
	if err != nil {
		return ModelReload{}, err
	}
	report := ModelReload{ModelReload: host.ModelReload{Unshadowed: dropShipped(catalog), Sources: settled}}
	registry, err := refreshRegistry(ctx, client)
	if err != nil {
		report.TableError = err.Error()
		if registry, err = wiring.ModelRegistry(); err != nil {
			return report, err
		}
	}
	report.ContextWindows, report.Table = len(registry.Windows), registry.From
	library, err := wiring.ModelLibrary("")
	if err != nil {
		return report, err
	}
	run := reloadRun{client: client, catalog: catalog, registry: registry, library: library, found: time.Now().Format(time.DateOnly)}
	for _, account := range accounts {
		report.Sources = append(report.Sources, run.account(ctx, account))
	}
	slices.SortStableFunc(report.Sources, func(a, b host.ReloadSource) int { return strings.Compare(a.Source, b.Source) })
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

func (r reloadRun) account(ctx context.Context, account models.Account) host.ReloadSource {
	source := host.ReloadSource{Source: string(account.Subscription), State: SourceReloaded}
	served, err := models.Discover(ctx, r.client, account)
	if err != nil {
		return failedSource(source, "model list failed", err)
	}
	plan := models.Plan(r.library.Reconcile(served, r.registry), r.registry, r.library, r.found)
	if err := plan.Write(r.catalog); err != nil {
		return failedSource(source, "catalog write failed", err)
	}
	source.Served = len(served.IDs)
	for _, model := range plan {
		change := host.ModelChange{Model: model.ID, Slug: model.Slug(), Change: modelAdded, Use: string(model.Use), Reason: model.Reason,
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
	library, err := wiring.ModelLibrary("")
	if err != nil {
		return nil, err
	}
	store, err := settingspkg.OpenFor(".")
	if err != nil {
		return nil, err
	}
	var unresolved []string
	for _, tier := range subagents.Tiers() {
		if slug := strings.TrimSpace(store.Text(tier.Setting())); slug != "" {
			if _, err := library.Select(slug); err != nil {
				unresolved = append(unresolved, "tier @"+string(tier)+": "+err.Error())
			}
		}
	}
	return unresolved, nil
}

func (r ModelReload) Envelope() cli.Envelope {
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

func showReload(out, errOut io.Writer, asJSON bool, report ModelReload, err error) int {
	if err != nil {
		return verbFailed(out, errOut, "models reload", asJSON, err)
	}
	return output.Show(out, asJSON, report.Envelope(), func(page cli.Page) []string { return ReloadLines(page, report) })
}

func ReloadLines(page cli.Page, report ModelReload) []string {
	counts := map[cli.Mark]int{cli.Fail: len(report.Unresolved)}
	for _, source := range report.Sources {
		for _, change := range source.Changes {
			counts[modelChangeMark(change)]++
		}
		if source.Failure != "" {
			counts[cli.Fail]++
		}
	}
	said := output.CountsSaid(counts, []output.MarkNoun{{Mark: cli.Added, Noun: "new"}, {Mark: cli.Changed, Noun: "changed"}, {Mark: cli.Removed, Noun: "dropped"}, {Mark: cli.Fail, Noun: "failed"}})
	if raised := len(report.Versions.Raised); raised > 0 {
		noun := " versions raised"
		if raised == 1 {
			noun = " version raised"
		}
		said = strings.TrimPrefix(said+" · "+strconv.Itoa(raised)+noun, " · ")
	}
	if report.Jev.To != "" {
		said = strings.TrimPrefix(said+" · jev pin raised", " · ")
	}
	verdict := cli.Verdict{Mark: cli.Done, Text: cmp.Or(said, "nothing changed")}
	if counts[cli.Fail] > 0 || len(report.Jev.Shadowed) > 0 {
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
	switch pin := report.Jev; {
	case pin.Skipped != "":
		lines = append(lines, page.Status("jev", cli.Verdict{Mark: cli.Warn, Text: pin.Skipped}))
	case pin.To != "":
		lines = append(lines, page.Status("jev", cli.Verdict{Mark: cli.Done, Text: "found " + pin.Found + ", pin raised from " + pin.From + " to " + pin.To}))
	case pin.Newer != "":
		lines = append(lines, page.Status("jev", cli.Verdict{Mark: cli.Warn, Text: "found " + pin.Found + ", newer than " + pin.From + ", " + models.ReloadVerb + " raises the pin"}))
	case pin.Found != "":
		lines = append(lines, page.Status("jev", cli.Verdict{Mark: cli.Done, Text: "found " + pin.Found + ", already pinned"}))
	}
	for _, shadowed := range report.Jev.Shadowed {
		lines = append(lines, page.Status("gate", cli.Verdict{Mark: cli.Warn, Text: shadowed}))
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
		case SourceReloaded:
			rows := make([]cli.Row, len(source.Changes))
			for i, change := range source.Changes {
				rows[i] = cli.Row{Mark: modelChangeMark(change), Cells: []string{change.Model}, Detail: change.Use}
			}
			lines = append(lines, page.Section(source.Source, cli.Verdict{Mark: cli.Done, Text: strconv.Itoa(source.Served) + " served"}))
			lines = append(lines, cli.Indent(page.Rows(rows)...)...)
		case SourceNotSignedIn:
			lines = append(lines, page.Section(source.Source, cli.Verdict{Mark: cli.Idle, Text: source.State}))
			lines = append(lines, cli.Indent(page.Hint(source.Hint))...)
		case SourceRefused, SourceFailed:
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
