package login

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/mattn/go-isatty"

	"tofu/apps/cli/output"
	cli "tofu/apps/cli/page"
	"tofu/apps/cli/wiring"
	"tofu/internal/cli/render"
	"tofu/internal/host"
	"tofu/internal/models/classifiers"
	"tofu/internal/models/classifiers/core"
	"tofu/internal/models/llm/accounts"
	"tofu/internal/models/llm/catalog"
	"tofu/internal/models/llm/quota"
	"tofu/internal/models/transport"
	platformcredential "tofu/internal/platform/credential"
	"tofu/internal/ui/tui/composer/keyfield"
)

const (
	Usage           = "tofu login llm <claude-sub|codex-sub> [--paste], tofu login llm <meta|openrouter>, tofu login classifier <openrouter|typesafe>, tofu login search brave"
	LogoutUsage     = "tofu logout <llm|classifier|search> <provider> [number from tofu login --status]"
	AsideUsage      = "tofu login --disable|--enable <number from tofu login --status>"
	minttyEchoes    = "this terminal shows what is typed, so the key will stay on screen: pipe it in instead, tofu login <role> <provider> < key.txt"
	stillResolved   = ", and the one in use is still set in the environment or a .env, which tofu does not change"
	changeSignedIn  = "signed_in"
	changeSignedOut = "signed_out"
	changeSetAside  = "set_aside"
	changeEnabled   = "enabled"
)

func isRole(word string) bool {
	return slices.Contains([]host.AccountRole{host.RoleLLM, host.RoleClassifier, host.RoleSearch}, host.AccountRole(word))
}

func keyedProvider(provider string) bool {
	return slices.ContainsFunc(wiring.KeyTable(), func(fact wiring.KeyFacts) bool { return fact.Provider == provider })
}

func keyedUnder(of host.AccountRole, provider string) bool {
	return slices.ContainsFunc(wiring.KeyTable(), func(fact wiring.KeyFacts) bool { return fact.Role == of && fact.Provider == provider })
}

type Receipt struct {
	Text string
	Data any
}

type accountReceipt struct {
	ID      int64  `json:"id"`
	Source  string `json:"source"`
	Account string `json:"account"`
	Change  string `json:"change"`
}

func refusedBy(display, hint string, err error) *output.Refusal {
	var refused *catalog.KeyRefused
	var answered *transport.Error
	what := err.Error()
	switch {
	case errors.As(err, &refused):
		what = refused.Brief()
	case errors.As(err, &answered) && answered.Status != 0:
		what = display + " refused the key (" + strconv.Itoa(answered.Status) + ")"
	case errors.As(err, &answered):
		what = "could not reach " + display
	}
	return output.Failure(what, hint)
}

type Deps struct {
	Status func(ctx context.Context, args []string, out, errOut io.Writer, now time.Time, urls map[quota.Provider]string) int
}

func Verb(deps Deps) func(ctx context.Context, args []string, in io.Reader, out, errOut io.Writer) int {
	return func(ctx context.Context, args []string, in io.Reader, out, errOut io.Writer) int {
		now := time.Now()
		if len(args) > 0 && args[0] == "--status" {
			return deps.Status(ctx, args[1:], out, errOut, now, nil)
		}
		return answerVerb("login", args, out, errOut, now, func(args []string) (Receipt, *output.Refusal) {
			return login(ctx, args, in, errOut, now)
		})
	}
}

func LogoutVerb(args []string, out, errOut io.Writer) int {
	return answerVerb("logout", args, out, errOut, time.Now(), Logout)
}

func answerVerb(verb string, args []string, out, errOut io.Writer, now time.Time, act func([]string) (Receipt, *output.Refusal)) int {
	asJSON := slices.Contains(args, output.JSONFlag)
	args = slices.DeleteFunc(slices.Clone(args), func(arg string) bool { return arg == output.JSONFlag })
	if len(args) > 0 {
		verb += " " + args[0]
	}
	receipt, refusal := act(args)
	if refusal != nil {
		return output.Refused(verb, refusal, asJSON, out, errOut, now)
	}
	if asJSON {
		return output.JSONExit(out, cli.Envelope{Verb: verb, OK: true, At: now, Data: receipt.Data})
	}
	path, _ := accounts.Path()
	page := cli.Detect(out, os.Environ())
	if page.Print(out, []string{page.Receipt(cli.Done, receipt.Text, path)}) != nil {
		return output.ExitVerdict
	}
	return output.ExitOK
}

func login(ctx context.Context, args []string, in io.Reader, prompts io.Writer, now time.Time) (Receipt, *output.Refusal) {
	if len(args) == 0 {
		return Receipt{}, output.UsageRefusal("no role named", Usage)
	}
	switch {
	case args[0] == "--disable" || args[0] == "--enable":
		if len(args) != 2 {
			return Receipt{}, output.UsageRefusal("want one credential number", AsideUsage)
		}
		return setCredentialAside(args[0] == "--disable", args[1], now)
	case !isRole(args[0]):
		return Receipt{}, retiredLogin(args[0])
	}
	spec, refusal := providerFor("login", host.AccountRole(args[0]), args[1:])
	if refusal != nil {
		return Receipt{}, refusal
	}
	if spec.Provider == "" {
		facts := wiring.KeyFactsOf(host.AccountRole(args[0]), args[1])
		if len(args) > 2 {
			return Receipt{}, output.UsageRefusal("a key is read from a prompt, never from an argument", facts.Hint())
		}
		return loginKey(ctx, facts, in, prompts)
	}
	paste := false
	for _, arg := range args[2:] {
		if arg != "--paste" {
			return Receipt{}, output.UsageRefusal("unknown flag "+arg, Usage)
		}
		paste = true
	}
	return signIn(ctx, spec, paste, in, prompts, now)
}

func retiredLogin(word string) *output.Refusal {
	provider := word
	spec, err := wiring.Specs().Lookup(word)
	switch {
	case err == nil:
		provider = string(spec.Provider)
	case !keyedProvider(word):
		return output.UsageRefusal("unknown role "+word, Usage)
	}
	return output.UsageRefusal("tofu login "+word+" is now "+wiring.LoginHint(provider), wiring.LoginHint(provider))
}

func providerFor(verb string, want host.AccountRole, args []string) (accounts.Spec, *output.Refusal) {
	if len(args) == 0 {
		return accounts.Spec{}, output.UsageRefusal("no "+string(want)+" provider named", Usage)
	}
	provider := args[0]
	spec, err := wiring.Specs().Lookup(provider)
	switch {
	case err == nil && string(spec.Provider) != provider:
		return accounts.Spec{}, output.UsageRefusal(provider+" is now "+string(spec.Provider), wiring.CommandFor(verb, host.RoleLLM, string(spec.Provider)))
	case err == nil && want != host.RoleLLM:
		return accounts.Spec{}, output.UsageRefusal(provider+" is under "+string(host.RoleLLM)+", not "+string(want), wiring.CommandFor(verb, host.RoleLLM, provider))
	case err != nil && !keyedProvider(provider):
		return accounts.Spec{}, output.UsageRefusal("unknown provider "+provider, Usage)
	case err != nil && !keyedUnder(want, provider):
		return accounts.Spec{}, output.UsageRefusal(provider+" has no "+string(want)+" key", wiring.LoginHint(provider))
	}
	return spec, nil
}

func Logout(args []string) (Receipt, *output.Refusal) {
	if len(args) == 0 {
		return Receipt{}, output.UsageRefusal("no role named", LogoutUsage)
	}
	if !isRole(args[0]) {
		return Receipt{}, output.UsageRefusal("unknown role "+args[0], LogoutUsage)
	}
	spec, refusal := providerFor("logout", host.AccountRole(args[0]), args[1:])
	switch {
	case refusal != nil:
		return Receipt{}, refusal
	case spec.Provider == "" && len(args) > 2:
		return Receipt{}, output.UsageRefusal("a key has no number", wiring.CommandFor("logout", host.AccountRole(args[0]), args[1]))
	case spec.Provider == "":
		return removeKey(wiring.KeyFactsOf(host.AccountRole(args[0]), args[1]))
	case len(args) > 3:
		return Receipt{}, output.UsageRefusal("want at most one credential number", LogoutUsage)
	}
	return removeAccount(spec.Provider, args[2:])
}

func removeKey(facts wiring.KeyFacts) (Receipt, *output.Refusal) {
	before := facts.Status(wiring.ResolvedKey)
	removed, err := platformcredential.RemoveKey(facts.Variable)
	if err != nil {
		return Receipt{}, output.Failure(err.Error(), "")
	}
	after := facts.Status(wiring.ResolvedKey)
	tail := ""
	if after.Key != "" {
		tail = stillResolved
	}
	if !removed {
		return Receipt{}, output.Failure("no "+facts.Name+" key is stored"+tail, "tofu login --status")
	}
	return Receipt{Text: facts.Name + " key " + before.Key + " removed" + tail, Data: after}, nil
}

func removeAccount(provider accounts.Provider, number []string) (Receipt, *output.Refusal) {
	store, err := wiring.OpenStoredCredentials()
	if err != nil {
		return Receipt{}, output.Failure(err.Error(), "")
	}
	none := output.Failure("no "+string(provider)+" account is signed in", "tofu login --status")
	if store == nil {
		return Receipt{}, none
	}
	defer func() { _ = store.Close() }()
	rows, err := store.List()
	if err != nil {
		return Receipt{}, output.Failure(err.Error(), "")
	}
	rows = slices.DeleteFunc(rows, func(row accounts.Row) bool { return row.Credential.Provider != provider })
	hint := wiring.CommandFor("logout", host.RoleLLM, string(provider)) + " <number>"
	switch {
	case len(rows) == 0:
		return Receipt{}, none
	case len(number) == 0 && len(rows) > 1:
		var named []string
		for _, row := range rows {
			named = append(named, "#"+strconv.FormatInt(row.ID, 10)+" "+wiring.AccountName(row.Credential.Identity, false))
		}
		return Receipt{}, output.UsageRefusal(strconv.Itoa(len(rows))+" "+string(provider)+" accounts are signed in, name one: "+strings.Join(named, ", "), hint)
	case len(number) == 1:
		id, err := strconv.ParseInt(number[0], 10, 64)
		if err != nil {
			return Receipt{}, output.UsageRefusal("not a credential number: "+number[0], hint)
		}
		rows = slices.DeleteFunc(rows, func(row accounts.Row) bool { return row.ID != id })
		if len(rows) == 0 {
			return Receipt{}, output.Failure("no "+string(provider)+" account is numbered "+number[0], "tofu login --status")
		}
	}
	if err := store.Delete(rows[0].ID); err != nil {
		return Receipt{}, output.Failure(err.Error(), "")
	}
	return receiptOf(rows[0], changeSignedOut), nil
}

func receiptOf(row accounts.Row, change string) Receipt {
	account := accountReceipt{ID: row.ID, Source: string(row.Credential.Provider), Account: wiring.AccountName(row.Credential.Identity, false), Change: change}
	text := "#" + strconv.FormatInt(account.ID, 10) + " " + account.Source + " " + account.Account + " " + strings.ReplaceAll(change, "_", " ")
	return Receipt{Text: text, Data: account}
}

func setCredentialAside(disable bool, number string, now time.Time) (Receipt, *output.Refusal) {
	id, err := strconv.ParseInt(number, 10, 64)
	if err != nil {
		return Receipt{}, output.UsageRefusal("not a credential number: "+number, AsideUsage)
	}
	missing := output.Failure("no credential is numbered "+number, "tofu login --status")
	store, err := wiring.OpenStoredCredentials()
	if err != nil {
		return Receipt{}, output.Failure(err.Error(), "")
	}
	if store == nil {
		return Receipt{}, missing
	}
	defer func() { _ = store.Close() }()
	row, held, err := store.RowByID(id)
	switch {
	case err != nil:
		return Receipt{}, output.Failure(err.Error(), "")
	case !held:
		return Receipt{}, missing
	}
	change := changeEnabled
	if disable {
		change, err = changeSetAside, store.Disable(id, wiring.SetAsideCause, now)
	} else {
		err = store.Enable(id, now)
	}
	if err != nil {
		return Receipt{}, output.Failure(err.Error(), "")
	}
	return receiptOf(row, change), nil
}

func signIn(ctx context.Context, spec accounts.Spec, paste bool, in io.Reader, prompts io.Writer, now time.Time) (Receipt, *output.Refusal) {
	path, err := accounts.Path()
	if err != nil {
		return Receipt{}, output.Failure(err.Error(), "")
	}
	store, err := accounts.Open(path, wiring.Specs())
	if err != nil {
		return Receipt{}, output.Failure(err.Error(), "")
	}
	defer func() { _ = store.Close() }()
	name := string(spec.Provider)
	options := accounts.LoginOptions{Spec: spec, Announce: func(line string) { _, _ = io.WriteString(prompts, line+"\n") }, Open: accounts.OpenBrowser}
	if paste {
		options.Open = nil
		options.Paste = func() (string, error) { return promptPaste(in, prompts) }
	}
	credential, err := accounts.Login(ctx, options)
	if err != nil {
		return Receipt{}, refusedBy(name, wiring.LoginHint(name), err)
	}
	if err := store.Save(credential, now); err != nil {
		return Receipt{}, output.Failure(err.Error(), "")
	}
	rows, err := store.List()
	if err != nil {
		return Receipt{}, output.Failure(err.Error(), "")
	}
	for _, row := range rows {
		if row.Credential.Provider == spec.Provider && row.Credential.Identity == credential.Identity {
			return receiptOf(row, changeSignedIn), nil
		}
	}
	return Receipt{}, output.Failure("the sign-in was stored and then not found", "tofu login --status")
}

func loginKey(ctx context.Context, facts wiring.KeyFacts, in io.Reader, prompts io.Writer) (Receipt, *output.Refusal) {
	key, err := promptKey(in, prompts, facts.Name, facts.Variable)
	if errors.As(err, &keyfield.Cancelled{}) {
		return Receipt{}, output.UsageRefusal(err.Error(), facts.Hint())
	}
	if err != nil {
		return Receipt{}, output.Failure(err.Error(), facts.Hint())
	}
	return storeKey(ctx, facts.Status(func(string) string { return "" }), key)
}

func storeKey(ctx context.Context, stored host.KeyStatus, key string) (Receipt, *output.Refusal) {
	stored.Key = render.Mask(key)
	done := " checked and stored"
	var err error
	switch stored.Provider {
	case wiring.BraveName, wiring.OpenRouterName:
		done = " stored"
	case wiring.MetaName:
		err = catalog.StoreMetaKey(ctx, key)
	default:
		err = reachesJev(ctx, catalog.Provider(stored.Provider), key)
	}
	facts := wiring.KeyFactsOf(stored.Role, stored.Provider)
	if err != nil {
		return Receipt{}, refusedBy(facts.Name, facts.Hint(), err)
	}
	if err := platformcredential.SaveKey(stored.Variable, key); err != nil {
		return Receipt{}, output.Failure(err.Error(), facts.Hint())
	}
	return Receipt{Text: facts.Name + " key " + stored.Key + done, Data: stored}, nil
}

func StoreKeyFor(ctx context.Context, variable, key string) (string, error) {
	keys := wiring.KeyStatuses(func(string) string { return "" })
	at := slices.IndexFunc(keys, func(status host.KeyStatus) bool { return status.Variable == variable })
	if at < 0 {
		return "", errors.New("tofu stores no key named " + variable)
	}
	receipt, refusal := storeKey(ctx, keys[at], key)
	if refusal != nil {
		return "", errors.New(refusal.Problem.What)
	}
	return receipt.Text, nil
}

func promptKey(in io.Reader, prompts io.Writer, name, variable string) (string, error) {
	title := "paste the " + name + " key, then press enter:"
	file, isFile := in.(*os.File)
	if isFile && term.IsTerminal(file.Fd()) {
		return keyfield.Read(file, prompts, title, variable)
	}
	if isFile && isatty.IsCygwinTerminal(file.Fd()) {
		_, _ = io.WriteString(prompts, minttyEchoes+"\n")
	}
	_, _ = io.WriteString(prompts, title+"\n")
	line, err := readLine(in)
	field := keyfield.New(variable)
	field.Paste(line)
	if field.Value() == "" {
		if err == nil || errors.Is(err, io.EOF) {
			err = keyfield.Cancelled{}
		}
		return "", err
	}
	_, _ = io.WriteString(prompts, field.Line()+"\n")
	return field.Value(), nil
}

func reachesJev(ctx context.Context, provider catalog.Provider, key string) error {
	library, err := wiring.ModelLibrary(".")
	if err != nil {
		return err
	}
	model, bound := library.Bound(catalog.RoleClassifier)
	if !bound || model.Provider != provider {
		at := slices.IndexFunc(library.Models, func(model catalog.Model) bool {
			return model.Kind == catalog.KindClassifier && model.Provider == provider
		})
		if at < 0 {
			return errors.New(string(provider) + " serves no classifier model in the library or .tofu/models")
		}
		model = library.Models[at]
	}
	client, err := classifiers.Route{Model: model, Key: key}.Client(wiring.OneCallAtATime)
	if err != nil {
		return err
	}
	_, err = client.Ask(ctx, core.Request{
		State: map[string]any{"check": "tofu login " + string(provider)},
		Questions: []core.Question{{
			ID:           "reachable",
			Kind:         core.QuestionNoul,
			Instructions: "does `check` say this request is a login check",
			True:         "it does",
			False:        "it does not",
		}},
	})
	return err
}

func promptPaste(in io.Reader, prompts io.Writer) (string, error) {
	_, _ = io.WriteString(prompts, "paste the final redirect URL or the authorization code, then press enter:\n")
	return readLine(in)
}

func readLine(in io.Reader) (string, error) {
	line, err := bufio.NewReader(in).ReadString('\n')
	if line == "" {
		return "", err
	}
	return line, nil
}
