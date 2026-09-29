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

	"tofu/interface/cli"
	"tofu/internal/judge/jev"
	"tofu/internal/llm/cred"
	"tofu/internal/llm/models"
	"tofu/internal/sys"
	"tofu/internal/transport"
	"tofu/internal/widget"
)

const (
	loginUsage       = "tofu login <claude-sub|codex-sub|openrouter|typesafe|meta|brave> [--paste] [--json]"
	asideUsage       = "tofu login --disable|--enable <number from tofu login --status>"
	setAsideCause    = "set aside by hand, run tofu login --enable to bring it back"
	openRouterName   = string(models.OpenRouter)
	typeSafeName     = string(models.TypeSafe)
	metaName         = string(models.Meta)
	braveName        = "brave"
	braveDisplay     = "Brave"
	openRouterFix    = "run tofu login openrouter and paste the key when it asks"
	accountMarkBytes = 2
	changeSignedIn   = "signed_in"
	changeSetAside   = "set_aside"
	changeEnabled    = "enabled"
)

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
	return failure(what, "tofu login "+name)
}

func loginVerb(args []string, in io.Reader, out, errOut io.Writer) int {
	now := time.Now()
	if len(args) > 0 && args[0] == "--status" {
		return statusVerb(args[1:], out, errOut, now, nil)
	}
	asJSON := slices.Contains(args, jsonFlag)
	args = slices.DeleteFunc(slices.Clone(args), func(arg string) bool { return arg == jsonFlag })
	verb := "login"
	if len(args) > 0 {
		verb += " " + args[0]
	}
	receipt, refusal := login(args, in, errOut, now)
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
		return loginReceipt{}, usageRefusal("no provider named", loginUsage)
	}
	switch args[0] {
	case "--disable", "--enable":
		if len(args) != 2 {
			return loginReceipt{}, usageRefusal("want one credential number", asideUsage)
		}
		return setCredentialAside(args[0] == "--disable", args[1], now)
	case openRouterName, typeSafeName, metaName, braveName:
		if len(args) > 1 {
			return loginReceipt{}, usageRefusal("a key is read from a prompt, never from an argument", "tofu login "+args[0])
		}
		return loginKey(args[0], in, prompts)
	}
	spec, err := cred.Lookup(args[0])
	if err != nil {
		return loginReceipt{}, usageRefusal("unknown provider "+args[0], loginUsage)
	}
	paste := false
	for _, arg := range args[1:] {
		if arg != "--paste" {
			return loginReceipt{}, usageRefusal("unknown flag "+arg, loginUsage)
		}
		paste = true
	}
	if args[0] != string(spec.Provider) {
		_, _ = io.WriteString(prompts, "tofu login "+args[0]+" is now tofu login "+string(spec.Provider)+"\n")
	}
	return signIn(spec, paste, in, prompts, now)
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
	key, err := promptKey(in, prompts, name)
	if err != nil {
		return loginReceipt{}, failure(err.Error(), "tofu login "+name)
	}
	var stored keyStatus
	for _, status := range keyStatuses(func(string) string { return key }) {
		if status.Provider == name {
			stored = status
		}
	}
	done := " checked and stored"
	switch name {
	case braveName:
		done = " stored"
	case metaName:
		err = models.StoreMetaKey(context.Background(), key)
	default:
		err = reachesJev(context.Background(), models.Provider(name), key)
	}
	if err != nil {
		return loginReceipt{}, refusedBy(stored.name, name, err)
	}
	if err := sys.SaveKey(stored.Variable, key); err != nil {
		return loginReceipt{}, failure(err.Error(), "tofu login "+name)
	}
	return loginReceipt{text: stored.name + " key " + stored.Key + done, data: stored}, nil
}

func promptKey(in io.Reader, prompts io.Writer, name string) (string, error) {
	_, _ = io.WriteString(prompts, "paste the "+name+" key, it is not echoed, then press enter:\n")
	file, isFile := in.(*os.File)
	if !isFile || !term.IsTerminal(file.Fd()) {
		line, err := readLine(in)
		return strings.TrimSpace(line), err
	}
	typed, err := term.ReadPassword(file.Fd())
	_, _ = io.WriteString(prompts, "\n")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(typed)), nil
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
	return jev.Locate(sys.CredentialFileName)
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
