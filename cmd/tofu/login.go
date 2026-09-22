package main

import (
	"bufio"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"

	"tofu/internal/judge/jev"
	jevwire "tofu/internal/judge/jev/wire/openrouter"
	"tofu/internal/konst"
	"tofu/internal/llm/cred"
	"tofu/internal/sys"
	"tofu/internal/transport"
	"tofu/internal/widget"
)

const (
	loginUsage = "usage: tofu login <claude-sub|codex-sub|openrouter> [--paste], " +
		"tofu login --status [--json] [--redact], or tofu login --disable|--enable <number>"
	setAsideCause    = "set aside by hand, run tofu login --enable to bring it back"
	openRouterName   = "openrouter"
	openRouterFix    = "run tofu login openrouter and paste the key when it asks"
	keyIsNeverTyped  = "the key is read from a prompt and never from an argument: run tofu login openrouter on its own"
	envFileName      = ".env"
	accountMarkBytes = 2
)

func loginVerb(args []string, in io.Reader, out, errOut io.Writer) int {
	if len(args) == 0 {
		return loginFail(errOut, errors.New(loginUsage))
	}
	switch args[0] {
	case "--status":
		return statusVerb(args[1:], out, errOut, paletteOf(out), time.Now(), nil)
	case "--disable", "--enable":
		if len(args) != 2 {
			return loginFail(errOut, errors.New(loginUsage))
		}
		cause := setAsideCause
		if args[0] == "--enable" {
			cause = ""
		}
		line, err := setCredentialAside(args[1], cause, time.Now())
		if err != nil {
			return loginRefused(errOut, "%v", err)
		}
		_, _ = fmt.Fprintln(out, line)
		return exitOK
	case openRouterName:
		if len(args) > 1 {
			return loginFail(errOut, errors.New(keyIsNeverTyped))
		}
		return loginOpenRouter(context.Background(), in, out, errOut)
	}
	spec, err := cred.Lookup(args[0])
	if err != nil {
		return loginFail(errOut, fmt.Errorf("%v, %s", err, loginUsage))
	}
	paste := false
	for _, arg := range args[1:] {
		if arg != "--paste" {
			return loginFail(errOut, fmt.Errorf("unknown flag %q, %s", arg, loginUsage))
		}
		paste = true
	}
	if args[0] != string(spec.Provider) {
		_, _ = fmt.Fprintf(out, "tofu login %s is now tofu login %s\n", args[0], spec.Provider)
	}
	if err := login(context.Background(), spec, paste, in, out); err != nil {
		return loginRefused(errOut, "%v", err)
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

func credentialLine(row cred.Row, now time.Time) string {
	return fmt.Sprintf("#%d %s %s", row.ID, maskedAccount(row.Credential.Identity), row.State(now))
}

func credentialByID(rows []cred.Row, id int64) (cred.Row, bool) {
	for _, row := range rows {
		if row.ID == id {
			return row, true
		}
	}
	return cred.Row{}, false
}

func setCredentialAside(number, cause string, now time.Time) (string, error) {
	id, err := strconv.ParseInt(number, 10, 64)
	if err != nil {
		return "", fmt.Errorf("want a credential number as tofu login --status prints it, not %q", number)
	}
	store, err := openStoredCredentials()
	if err != nil {
		return "", err
	}
	missing := fmt.Errorf("no stored credential is numbered %d, tofu login --status lists them", id)
	if store == nil {
		return "", missing
	}
	defer func() { _ = store.Close() }()
	rows, err := store.List()
	if err != nil {
		return "", err
	}
	if _, held := credentialByID(rows, id); !held {
		return "", missing
	}
	if cause == "" {
		err = store.Enable(id, now)
	} else {
		err = store.Disable(id, cause, now)
	}
	if err != nil {
		return "", err
	}
	rows, err = store.List()
	if err != nil {
		return "", err
	}
	row, _ := credentialByID(rows, id)
	return credentialLine(row, now), nil
}

func loginFail(errOut io.Writer, err error) int {
	_, _ = fmt.Fprintf(errOut, "tofu login: %v\n", err)
	return exitUsage
}

func loginRefused(errOut io.Writer, format string, args ...any) int {
	_, _ = fmt.Fprintf(errOut, "tofu login: "+format+"\n", args...)
	return exitVerdict
}

func login(ctx context.Context, spec cred.Spec, paste bool, in io.Reader, out io.Writer) error {
	path, err := cred.Path()
	if err != nil {
		return err
	}
	store, err := cred.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	options := cred.LoginOptions{
		Spec:     spec,
		Announce: func(line string) { _, _ = fmt.Fprintln(out, line) },
		Open:     cred.OpenBrowser,
	}
	if paste {
		options.Open = nil
		options.Paste = func() (string, error) { return promptPaste(in, out) }
	}
	credential, err := cred.Login(ctx, options)
	if err != nil {
		return err
	}
	if err := store.Save(credential, time.Now()); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "stored at %s\n", path)
	_, _ = fmt.Fprintf(out, "credentials: %s\n", cred.Report([]cred.Row{{Credential: credential}}, time.Now()))
	return nil
}

func loginOpenRouter(ctx context.Context, in io.Reader, out, errOut io.Writer) int {
	key, err := promptKey(in, out)
	if err != nil {
		return loginFail(errOut, err)
	}
	if err := reachesJev(ctx, key); err != nil {
		return loginRefused(errOut, "the key did not reach jev, so nothing was written: %v", err)
	}
	path, err := cred.OpenRouterPath()
	if err != nil {
		return loginRefused(errOut, "%v", err)
	}
	if err := cred.SaveOpenRouter(path, key); err != nil {
		return loginRefused(errOut, "%v", err)
	}
	_, _ = fmt.Fprintf(out, "openrouter: key stored in %s, mode 600 where the platform honours it\n", path)
	_, _ = fmt.Fprintln(out, "jev: ready")
	return exitOK
}

func promptKey(in io.Reader, out io.Writer) (string, error) {
	_, _ = fmt.Fprintln(out, "paste the openrouter key, it is not echoed, then press enter:")
	file, isFile := in.(*os.File)
	if !isFile || !term.IsTerminal(file.Fd()) {
		line, err := readLine(in)
		return strings.TrimSpace(line), err
	}
	typed, err := term.ReadPassword(file.Fd())
	_, _ = fmt.Fprintln(out)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(typed)), nil
}

func reachesJev(ctx context.Context, key string) error {
	wire, err := jevwire.New(jevwire.Config{
		Key:      key,
		Endpoint: os.Getenv(judgeEndpointEnvar),
		Transport: transport.Config{
			AttemptTimeout: time.Duration(konst.JudgeTimeoutMillis) * time.Millisecond,
			Retries:        konst.JudgeRetries,
			Backoff:        time.Duration(konst.JudgeBackoffMillis) * time.Millisecond,
			Concurrency:    1,
		},
	})
	if err != nil {
		return err
	}
	client, err := jev.NewClient(jev.Config{Wire: wire})
	if err != nil {
		return err
	}
	_, err = client.Ask(ctx, jev.Request{
		State: map[string]any{"check": "tofu login openrouter"},
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
	located, err := jev.Locate(envFileName)
	if err == nil {
		return located, nil
	}
	home, homeErr := sys.HomeConfigDir()
	if homeErr != nil {
		return located, err
	}
	stored, storedErr := jev.Locate(sys.Join(home, envFileName))
	if storedErr != nil {
		return located, err
	}
	return stored, nil
}

func openRouterStatus() string {
	located, err := locateGateKey()
	if err != nil {
		return "no key, so the jev gate is off: " + openRouterFix
	}
	if located.Source == jev.SourceEnvironment {
		return "key set in the environment"
	}
	return "key set in " + located.Path
}

func promptPaste(in io.Reader, out io.Writer) (string, error) {
	_, _ = fmt.Fprintln(out, "paste the final redirect URL or the authorization code, then press enter:")
	return readLine(in)
}

func readLine(in io.Reader) (string, error) {
	line, err := bufio.NewReader(in).ReadString('\n')
	if line == "" {
		return "", err
	}
	return line, nil
}
