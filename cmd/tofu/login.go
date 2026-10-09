package main

import (
	"bufio"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/mattn/go-isatty"

	"tofu/interface/cli"
	"tofu/interface/tui/keyfield"
	"tofu/internal/host"
	"tofu/internal/judge/jev"
	"tofu/internal/llm/cred"
	"tofu/internal/llm/models"
	"tofu/internal/sys"
	"tofu/internal/transport"
	"tofu/internal/widget"
)

const (
	loginUsage       = "tofu login llm <claude-sub|codex-sub> [--paste], tofu login llm meta, tofu login classifier <openrouter|typesafe>, tofu login search brave"
	logoutUsage      = "tofu logout <llm|classifier|search> <provider> [number from tofu login --status]"
	asideUsage       = "tofu login --disable|--enable <number from tofu login --status>"
	setAsideCause    = "set aside by hand, run tofu login --enable to bring it back"
	openRouterName   = string(models.OpenRouter)
	typeSafeName     = string(models.TypeSafe)
	metaName         = string(models.Meta)
	braveName        = "brave"
	braveDisplay     = "Brave"
	openRouterFix    = "run tofu login classifier openrouter and paste the key when it asks"
	minttyEchoes     = "this terminal shows what is typed, so the key will stay on screen: pipe it in instead, tofu login <role> <provider> < key.txt"
	stillResolved    = ", and the one in use is still set in the environment or a .env, which tofu does not change"
	accountMarkBytes = 2
	changeSignedIn   = "signed_in"
	changeSignedOut  = "signed_out"
	changeSetAside   = "set_aside"
	changeEnabled    = "enabled"
)

type role = host.AccountRole

const (
	roleLLM        = host.RoleLLM
	roleClassifier = host.RoleClassifier
	roleSearch     = host.RoleSearch
)

func roleOf(provider string) role {
	switch provider {
	case openRouterName, typeSafeName:
		return roleClassifier
	case braveName:
		return roleSearch
	}
	return roleLLM
}

func isRole(word string) bool {
	return slices.Contains([]role{roleLLM, roleClassifier, roleSearch}, role(word))
}

func loginWords(provider string) []string { return []string{string(roleOf(provider)), provider} }

func commandFor(verb, provider string) string {
	return "tofu " + verb + " " + strings.Join(loginWords(provider), " ")
}

func loginHint(provider string) string { return commandFor("login", provider) }

func keyedProvider(provider string) bool {
	return slices.Contains([]string{openRouterName, typeSafeName, metaName, braveName}, provider)
}

type loginRefusal struct {
	problem cli.Problem
	code    int
}

type loginReceipt struct {
	text string
	data any
}

type accountReceipt struct {
	ID      int64  `json:"id"`
	Source  string `json:"source"`
	Account string `json:"account"`
	Change  string `json:"change"`
}

func usageRefusal(what, hint string) *loginRefusal {
	return &loginRefusal{cli.Problem{What: what, Hint: hint}, exitUsage}
}

func failure(what, hint string) *loginRefusal {
	return &loginRefusal{cli.Problem{What: what, Hint: hint}, exitVerdict}
}

func refusedBy(display, name string, err error) *loginRefusal {
	var refused *models.KeyRefused
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
	return failure(what, loginHint(name))
}

func loginVerb(args []string, in io.Reader, out, errOut io.Writer) int {
	now := time.Now()
	if len(args) > 0 && args[0] == "--status" {
		return statusVerb(args[1:], out, errOut, now, nil)
	}
	return answerVerb("login", args, out, errOut, now, func(args []string) (loginReceipt, *loginRefusal) {
		return login(args, in, errOut, now)
	})
}

func logoutVerb(args []string, out, errOut io.Writer) int {
	return answerVerb("logout", args, out, errOut, time.Now(), logout)
}

func answerVerb(verb string, args []string, out, errOut io.Writer, now time.Time, act func([]string) (loginReceipt, *loginRefusal)) int {
	asJSON := slices.Contains(args, jsonFlag)
	args = slices.DeleteFunc(slices.Clone(args), func(arg string) bool { return arg == jsonFlag })
	if len(args) > 0 {
		verb += " " + args[0]
	}
	receipt, refusal := act(args)
	if refusal != nil {
		return loginFailed(verb, refusal, asJSON, out, errOut, now)
	}
	if asJSON {
		return jsonExit(out, cli.Envelope{Verb: verb, OK: true, At: now, Data: receipt.data})
	}
	path, _ := cred.Path()
	page := cli.Detect(out, os.Environ())
	if page.Print(out, []string{page.Receipt(cli.Done, receipt.text, path)}) != nil {
		return exitVerdict
	}
	return exitOK
}

func login(args []string, in io.Reader, prompts io.Writer, now time.Time) (loginReceipt, *loginRefusal) {
	if len(args) == 0 {
		return loginReceipt{}, usageRefusal("no role named", loginUsage)
	}
	switch {
	case args[0] == "--disable" || args[0] == "--enable":
		if len(args) != 2 {
			return loginReceipt{}, usageRefusal("want one credential number", asideUsage)
		}
		return setCredentialAside(args[0] == "--disable", args[1], now)
	case !isRole(args[0]):
		return loginReceipt{}, retiredLogin(args[0])
	}
	spec, refusal := providerFor("login", role(args[0]), args[1:])
	if refusal != nil {
		return loginReceipt{}, refusal
	}
	if spec.Provider == "" {
		if len(args) > 2 {
			return loginReceipt{}, usageRefusal("a key is read from a prompt, never from an argument", loginHint(args[1]))
		}
		return loginKey(args[1], in, prompts)
	}
	paste := false
	for _, arg := range args[2:] {
		if arg != "--paste" {
			return loginReceipt{}, usageRefusal("unknown flag "+arg, loginUsage)
		}
		paste = true
	}
	return signIn(spec, paste, in, prompts, now)
}

func retiredLogin(word string) *loginRefusal {
	provider := word
	spec, err := cred.Lookup(word)
	switch {
	case err == nil:
		provider = string(spec.Provider)
	case !keyedProvider(word):
		return usageRefusal("unknown role "+word, loginUsage)
	}
	return usageRefusal("tofu login "+word+" is now "+loginHint(provider), loginHint(provider))
}

func providerFor(verb string, want role, args []string) (cred.Spec, *loginRefusal) {
	if len(args) == 0 {
		return cred.Spec{}, usageRefusal("no "+string(want)+" provider named", loginUsage)
	}
	provider := args[0]
	spec, err := cred.Lookup(provider)
	switch {
	case err == nil && string(spec.Provider) != provider:
		return cred.Spec{}, usageRefusal(provider+" is now "+string(spec.Provider), commandFor(verb, string(spec.Provider)))
	case err != nil && !keyedProvider(provider):
		return cred.Spec{}, usageRefusal("unknown provider "+provider, loginUsage)
	case roleOf(provider) != want:
		return cred.Spec{}, usageRefusal(provider+" is under "+string(roleOf(provider))+", not "+string(want), commandFor(verb, provider))
	}
	return spec, nil
}

func logout(args []string) (loginReceipt, *loginRefusal) {
	if len(args) == 0 {
		return loginReceipt{}, usageRefusal("no role named", logoutUsage)
	}
	if !isRole(args[0]) {
		return loginReceipt{}, usageRefusal("unknown role "+args[0], logoutUsage)
	}
	spec, refusal := providerFor("logout", role(args[0]), args[1:])
	switch {
	case refusal != nil:
		return loginReceipt{}, refusal
	case spec.Provider == "" && len(args) > 2:
		return loginReceipt{}, usageRefusal("a key has no number", commandFor("logout", args[1]))
	case spec.Provider == "":
		return removeKey(args[1])
	case len(args) > 3:
		return loginReceipt{}, usageRefusal("want at most one credential number", logoutUsage)
	}
	return removeAccount(spec.Provider, args[2:])
}

func resolvedKey(variable string) string {
	key, _ := jev.KeyFor(sys.CredentialFileName, variable)
	return key
}

func removeKey(provider string) (loginReceipt, *loginRefusal) {
	before := keyStatusOf(provider, resolvedKey)
	removed, err := sys.RemoveKey(before.Variable)
	if err != nil {
		return loginReceipt{}, failure(err.Error(), "")
	}
	after := keyStatusOf(provider, resolvedKey)
	tail := ""
	if after.Key != "" {
		tail = stillResolved
	}
	if !removed {
		return loginReceipt{}, failure("no "+keyFactsOf(provider).name+" key is stored"+tail, "tofu login --status")
	}
	return loginReceipt{text: keyFactsOf(provider).name + " key " + before.Key + " removed" + tail, data: after}, nil
}

func removeAccount(provider cred.Provider, number []string) (loginReceipt, *loginRefusal) {
	store, err := openStoredCredentials()
	if err != nil {
		return loginReceipt{}, failure(err.Error(), "")
	}
	none := failure("no "+string(provider)+" account is signed in", "tofu login --status")
	if store == nil {
		return loginReceipt{}, none
	}
	defer func() { _ = store.Close() }()
	rows, err := store.List()
	if err != nil {
		return loginReceipt{}, failure(err.Error(), "")
	}
	rows = slices.DeleteFunc(rows, func(row cred.Row) bool { return row.Credential.Provider != provider })
	hint := commandFor("logout", string(provider)) + " <number>"
	switch {
	case len(rows) == 0:
		return loginReceipt{}, none
	case len(number) == 0 && len(rows) > 1:
		var named []string
		for _, row := range rows {
			named = append(named, "#"+strconv.FormatInt(row.ID, 10)+" "+accountName(row.Credential.Identity, false))
		}
		return loginReceipt{}, usageRefusal(strconv.Itoa(len(rows))+" "+string(provider)+" accounts are signed in, name one: "+strings.Join(named, ", "), hint)
	case len(number) == 1:
		id, err := strconv.ParseInt(number[0], 10, 64)
		if err != nil {
			return loginReceipt{}, usageRefusal("not a credential number: "+number[0], hint)
		}
		rows = slices.DeleteFunc(rows, func(row cred.Row) bool { return row.ID != id })
		if len(rows) == 0 {
			return loginReceipt{}, failure("no "+string(provider)+" account is numbered "+number[0], "tofu login --status")
		}
	}
	if err := store.Delete(rows[0].ID); err != nil {
		return loginReceipt{}, failure(err.Error(), "")
	}
	return receiptOf(rows[0], changeSignedOut), nil
}

func loginFailed(verb string, refusal *loginRefusal, asJSON bool, out, errOut io.Writer, now time.Time) int {
	if asJSON {
		_ = writeJSON(out, cli.Envelope{Verb: verb, At: now, Problems: []cli.Problem{refusal.problem}})
		return refusal.code
	}
	page := cli.Detect(errOut, os.Environ())
	_ = page.Print(errOut, page.ErrorLine(refusal.problem.What, refusal.problem.Hint))
	return refusal.code
}

func jsonExit(out io.Writer, envelope cli.Envelope) int {
	if writeJSON(out, envelope) != nil {
		return exitVerdict
	}
	return exitOK
}

func openStoredCredentials() (*cred.Store, error) {
	path, err := cred.Path()
	if err != nil {
		return nil, err
	}
	present, err := sys.Exists(path)
	if err != nil || !present {
		return nil, err
	}
	return cred.Open(path)
}

func maskedAccount(identity cred.Identity) string {
	account := cmp.Or(identity.Email, identity.AccountID)
	if account == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(account))
	return widget.Mask(account) + "#" + hex.EncodeToString(sum[:accountMarkBytes])
}

func receiptOf(row cred.Row, change string) loginReceipt {
	account := accountReceipt{ID: row.ID, Source: string(row.Credential.Provider), Account: accountName(row.Credential.Identity, false), Change: change}
	text := "#" + strconv.FormatInt(account.ID, 10) + " " + account.Source + " " + account.Account + " " + spoken(change)
	return loginReceipt{text: text, data: account}
}

func spoken(code string) string { return strings.ReplaceAll(code, "_", " ") }

func setCredentialAside(disable bool, number string, now time.Time) (loginReceipt, *loginRefusal) {
	id, err := strconv.ParseInt(number, 10, 64)
	if err != nil {
		return loginReceipt{}, usageRefusal("not a credential number: "+number, asideUsage)
	}
	missing := failure("no credential is numbered "+number, "tofu login --status")
	store, err := openStoredCredentials()
	if err != nil {
		return loginReceipt{}, failure(err.Error(), "")
	}
	if store == nil {
		return loginReceipt{}, missing
	}
	defer func() { _ = store.Close() }()
	row, held, err := store.RowByID(id)
	switch {
	case err != nil:
		return loginReceipt{}, failure(err.Error(), "")
	case !held:
		return loginReceipt{}, missing
	}
	change := changeEnabled
	if disable {
		change, err = changeSetAside, store.Disable(id, setAsideCause, now)
	} else {
		err = store.Enable(id, now)
	}
	if err != nil {
		return loginReceipt{}, failure(err.Error(), "")
	}
	return receiptOf(row, change), nil
}

func signIn(spec cred.Spec, paste bool, in io.Reader, prompts io.Writer, now time.Time) (loginReceipt, *loginRefusal) {
	path, err := cred.Path()
	if err != nil {
		return loginReceipt{}, failure(err.Error(), "")
	}
	store, err := cred.Open(path)
	if err != nil {
		return loginReceipt{}, failure(err.Error(), "")
	}
	defer func() { _ = store.Close() }()
	name := string(spec.Provider)
	options := cred.LoginOptions{Spec: spec, Announce: func(line string) { _, _ = io.WriteString(prompts, line+"\n") }, Open: cred.OpenBrowser}
	if paste {
		options.Open = nil
		options.Paste = func() (string, error) { return promptPaste(in, prompts) }
	}
	credential, err := cred.Login(context.Background(), options)
	if err != nil {
		return loginReceipt{}, refusedBy(name, name, err)
	}
	if err := store.Save(credential, now); err != nil {
		return loginReceipt{}, failure(err.Error(), "")
	}
	rows, err := store.List()
	if err != nil {
		return loginReceipt{}, failure(err.Error(), "")
	}
	for _, row := range rows {
		if row.Credential.Provider == spec.Provider && row.Credential.Identity == credential.Identity {
			return receiptOf(row, changeSignedIn), nil
		}
	}
	return loginReceipt{}, failure("the sign-in was stored and then not found", "tofu login --status")
}

func loginKey(name string, in io.Reader, prompts io.Writer) (loginReceipt, *loginRefusal) {
	stored := keyStatusOf(name, func(string) string { return "" })
	key, err := promptKey(in, prompts, keyFactsOf(name).name, stored.Variable)
	if errors.As(err, &keyfield.Cancelled{}) {
		return loginReceipt{}, usageRefusal(err.Error(), loginHint(name))
	}
	if err != nil {
		return loginReceipt{}, failure(err.Error(), loginHint(name))
	}
	return storeKey(context.Background(), stored, key)
}

func storeKey(ctx context.Context, stored keyStatus, key string) (loginReceipt, *loginRefusal) {
	stored.Key = widget.Mask(key)
	done := " checked and stored"
	var err error
	switch stored.Provider {
	case braveName:
		done = " stored"
	case metaName:
		err = models.StoreMetaKey(ctx, key)
	default:
		err = reachesJev(ctx, models.Provider(stored.Provider), key)
	}
	if err != nil {
		return loginReceipt{}, refusedBy(keyFactsOf(stored.Provider).name, stored.Provider, err)
	}
	if err := sys.SaveKey(stored.Variable, key); err != nil {
		return loginReceipt{}, failure(err.Error(), loginHint(stored.Provider))
	}
	if classifier, err := boundClassifier(); err == nil && stored.Role == roleClassifier && string(classifier.Provider) != stored.Provider {
		done += ", but " + classifier.Provider.Display() + " stays the classifier while its key is stored"
	}
	return loginReceipt{text: keyFactsOf(stored.Provider).name + " key " + stored.Key + done, data: stored}, nil
}

func storeKeyFor(ctx context.Context, variable, key string) (string, error) {
	keys := keyStatuses(func(string) string { return "" })
	at := slices.IndexFunc(keys, func(status keyStatus) bool { return status.Variable == variable })
	if at < 0 {
		return "", errors.New("tofu stores no key named " + variable)
	}
	receipt, refusal := storeKey(ctx, keys[at], key)
	if refusal != nil {
		return "", errors.New(refusal.problem.What)
	}
	return receipt.text, nil
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

func reachesJev(ctx context.Context, provider models.Provider, key string) error {
	client, err := jevClientFor(provider, key, oneCallAtATime)
	if err != nil {
		return err
	}
	_, err = client.Ask(ctx, jev.Request{
		State: map[string]any{"check": "tofu login " + string(provider)},
		Questions: []jev.Question{{
			ID:           "reachable",
			Kind:         jev.QuestionNoul,
			Instructions: "does `check` say this request is a login check",
			True:         "it does",
			False:        "it does not",
		}},
	})
	return err
}

func locateGateKey() (jev.Located, error) {
	classifier, err := boundClassifier()
	if err != nil {
		return jev.Located{}, err
	}
	return jev.LocateFor(sys.CredentialFileName, classifier.Provider.KeyName())
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
