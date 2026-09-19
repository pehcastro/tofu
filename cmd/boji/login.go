package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"boji/internal/llm/cred"
)

const loginUsage = "usage: boji login <anthropic|codex> [--paste], or boji login --status"

func loginVerb(args []string, in io.Reader, out, errOut io.Writer) int {
	if len(args) == 0 {
		return loginFail(errOut, errors.New(loginUsage))
	}
	if args[0] == "--status" {
		_, _ = fmt.Fprintf(out, "credentials: %s\n", cred.DoctorState())
		return exitOK
	}
	spec, err := cred.Lookup(args[0])
	if err != nil {
		return loginFail(errOut, err)
	}
	paste := false
	for _, arg := range args[1:] {
		if arg != "--paste" {
			return loginFail(errOut, fmt.Errorf("unknown flag %q, %s", arg, loginUsage))
		}
		paste = true
	}
	if err := login(context.Background(), spec, paste, in, out); err != nil {
		_, _ = fmt.Fprintf(errOut, "boji login: %v\n", err)
		return exitVerdict
	}
	return exitOK
}

func loginFail(errOut io.Writer, err error) int {
	_, _ = fmt.Fprintf(errOut, "boji login: %v\n", err)
	return exitUsage
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

func promptPaste(in io.Reader, out io.Writer) (string, error) {
	_, _ = fmt.Fprintln(out, "paste the final redirect URL or the authorization code, then press enter:")
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return line, nil
}
