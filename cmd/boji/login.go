package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"

	"boji/internal/judge/jev"
	jevwire "boji/internal/judge/jev/wire/openrouter"
	"boji/internal/konst"
	"boji/internal/llm/cred"
	"boji/internal/sys"
	"boji/internal/transport"
)

const (
	loginUsage      = "usage: boji login <anthropic|codex|openrouter> [--paste], or boji login --status"
	openRouterName  = "openrouter"
	openRouterFix   = "run boji login openrouter and paste the key when it asks"
	keyIsNeverTyped = "the key is read from a prompt and never from an argument: run boji login openrouter on its own"
	envFileName     = ".env"
)

func loginVerb(args []string, in io.Reader, out, errOut io.Writer) int {
	if len(args) == 0 {
		return loginFail(errOut, errors.New(loginUsage))
	}
	if args[0] == "--status" {
		_, _ = fmt.Fprintf(out, "credentials: %s\n", cred.DoctorState())
		_, _ = fmt.Fprintf(out, "openrouter: %s\n", openRouterStatus())
		return exitOK
	}
	if args[0] == openRouterName {
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
	if err := login(context.Background(), spec, paste, in, out); err != nil {
		return loginRefused(errOut, "%v", err)
	}
	return exitOK
}

func loginFail(errOut io.Writer, err error) int {
	_, _ = fmt.Fprintf(errOut, "boji login: %v\n", err)
	return exitUsage
}

func loginRefused(errOut io.Writer, format string, args ...any) int {
	_, _ = fmt.Fprintf(errOut, "boji login: "+format+"\n", args...)
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
	_, _ = fmt.Fprintf(out, "credentials: %s\n", cred.Report([]cred.Row{{Credential: credential}}))
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
		State: map[string]any{"check": "boji login openrouter"},
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
