package cred

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const loginTimeout = 5 * time.Minute

type LoginOptions struct {
	Spec     Spec
	Announce func(line string)
	Open     func(target string) error
	Paste    func() (string, error)
	Client   *http.Client
	Now      func() time.Time
}

func Login(ctx context.Context, opt LoginOptions) (Credential, error) {
	if opt.Client == nil {
		opt.Client = &http.Client{}
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	verifier, challenge, err := pkce()
	if err != nil {
		return Credential{}, err
	}
	state, err := randomURLSafe(stateBytes)
	if err != nil {
		return Credential{}, err
	}

	port := opt.Spec.CallbackPort
	var server *callbackServer
	if opt.Paste == nil {
		server, err = startCallback(opt.Spec.CallbackPath, state, port, opt.Spec.PortFallback, ipv6LoopbackAvailable())
		if err != nil {
			return Credential{}, err
		}
		defer server.close()
		port = server.port
	}
	redirect := fmt.Sprintf("http://%s:%d%s", callbackHost, port, opt.Spec.CallbackPath)
	target := authorizeURL(opt.Spec, redirect, challenge, state)

	announce(opt, opt.Spec.Instructions)
	announce(opt, target)
	if opt.Open != nil {
		if err := opt.Open(target); err != nil {
			announce(opt, "could not open a browser, open the link above by hand")
		}
	}

	code, err := awaitCode(ctx, server, opt.Paste, state)
	if err != nil {
		return Credential{}, err
	}
	return exchange(ctx, opt.Client, opt.Spec, code, state, redirect, verifier, opt.Now())
}

func announce(opt LoginOptions, line string) {
	if opt.Announce != nil && line != "" {
		opt.Announce(line)
	}
}

func authorizeURL(spec Spec, redirect, challenge, state string) string {
	query := url.Values{}
	query.Set("client_id", spec.ClientID)
	query.Set("response_type", "code")
	query.Set("redirect_uri", redirect)
	query.Set("scope", strings.Join(spec.Scopes, " "))
	query.Set("code_challenge", challenge)
	query.Set("code_challenge_method", "S256")
	query.Set("state", state)
	for key, value := range spec.AuthorizeParams {
		query.Set(key, value)
	}
	return spec.AuthorizeURL + "?" + query.Encode()
}

func awaitCode(ctx context.Context, server *callbackServer, paste func() (string, error), state string) (string, error) {
	if paste != nil {
		pasted, err := paste()
		if err != nil {
			return "", err
		}
		return codeFromPaste(pasted, state)
	}
	timer := time.NewTimer(loginTimeout)
	defer timer.Stop()
	select {
	case result := <-server.results:
		return result.code, result.err
	case <-ctx.Done():
		return "", ctx.Err()
	case <-timer.C:
		return "", errors.New("cred: no authorization callback arrived within five minutes")
	}
}

func codeFromPaste(pasted, state string) (string, error) {
	pasted = strings.TrimSpace(pasted)
	if pasted == "" {
		return "", errors.New("cred: nothing was pasted")
	}
	if !strings.HasPrefix(pasted, "http://") && !strings.HasPrefix(pasted, "https://") {
		return pasted, nil
	}
	parsed, err := url.Parse(pasted)
	if err != nil {
		return "", fmt.Errorf("cred: the pasted redirect URL did not parse: %w", err)
	}
	query := parsed.Query()
	if detail := query.Get("error_description"); detail != "" {
		return "", errors.New("cred: authorization failed: " + detail)
	}
	if query.Get("state") != state {
		return "", errors.New("cred: the pasted redirect URL carried a state this login did not send")
	}
	code := query.Get("code")
	if code == "" {
		return "", errors.New("cred: the pasted redirect URL carried no authorization code")
	}
	return code, nil
}

func OpenBrowser(target string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", target).Start()
	case "darwin":
		return exec.Command("open", target).Start()
	default:
		return exec.Command("xdg-open", target).Start()
	}
}
