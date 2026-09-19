package cred

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"syscall"
)

const (
	ipv4Loopback          = "127.0.0.1"
	ipv6Loopback          = "::1"
	callbackHost          = "localhost"
	ipv6CompanionAttempts = 4
	windowsAddressInUse   = syscall.Errno(10048)
)

type callbackReason string

const (
	reasonProviderRefused callbackReason = "The provider refused the sign-in and sent no authorization code."
	reasonStateMismatch   callbackReason = "The callback state did not match the state sent, so the code was refused."
	reasonMissingCode     callbackReason = "The callback carried no authorization code."
)

const pageHead = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="referrer" content="no-referrer">
<title>boji</title>
<style>
:root { color-scheme: light dark; --bg: #f7f7f5; --card: #ffffff; --ink: #1b1b19; --muted: #6d6a62; --line: #e5e3dd; --ok: #2f7d4f; --bad: #b03a2e; }
@media (prefers-color-scheme: dark) { :root { --bg: #131312; --card: #1c1c1a; --ink: #f0eee6; --muted: #9b978c; --line: #2d2c29; --ok: #79c894; --bad: #e8806f; } }
body { margin: 0; min-height: 100vh; display: flex; align-items: center; justify-content: center; padding: 2rem; background: var(--bg); color: var(--ink); font: 16px/1.6 ui-sans-serif, system-ui, "Segoe UI", sans-serif; }
main { max-width: 32rem; border: 1px solid var(--line); border-radius: 14px; background: var(--card); padding: 2rem 2.25rem; }
h1 { margin: 0 0 0.75rem; font-size: 1.35rem; letter-spacing: -0.01em; }
p { margin: 0 0 0.75rem; }
p.foot { margin-bottom: 0; color: var(--muted); font-size: 0.9rem; }
main.ok h1 { color: var(--ok); }
main.bad h1 { color: var(--bad); }
</style>
</head>
<body>
<main class="`

func callbackDocument(reason callbackReason) string {
	class, heading, detail := "ok", "Signed in", "boji holds the credential now, and the terminal has gone on without you."
	foot := "You can close this window."
	if reason != "" {
		class, heading, detail = "bad", "Sign-in failed", string(reason)
		foot = "The terminal says the same thing. Return to it and run the login again."
	}
	return pageHead + class + `">
<h1>` + heading + `</h1>
<p>` + detail + `</p>
<p class="foot">` + foot + `</p>
</main>
</body>
</html>
`
}

type callbackResult struct {
	code  string
	state string
	err   error
}

type callbackServer struct {
	port      int
	addresses []string
	results   chan callbackResult
	server    *http.Server
}

func startCallback(path, expectedState string, port int, portFallback, ipv6 bool) (*callbackServer, error) {
	listeners, err := listenLoopback(port, ipv6)
	if err != nil && portFallback {
		listeners, err = listenLoopback(0, ipv6)
	}
	if err != nil {
		return nil, fmt.Errorf("cred: callback port %d is unavailable and this provider allowlists only that port: %w", port, err)
	}
	cb := &callbackServer{
		port:    listeners[0].Addr().(*net.TCPAddr).Port,
		results: make(chan callbackResult, 1),
	}
	for _, listener := range listeners {
		cb.addresses = append(cb.addresses, listener.Addr().String())
	}
	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		cb.deliver(w, r, expectedState)
	})
	cb.server = &http.Server{Handler: mux}
	for _, listener := range listeners {
		go func(l net.Listener) { _ = cb.server.Serve(l) }(listener)
	}
	return cb, nil
}

func (c *callbackServer) deliver(w http.ResponseWriter, r *http.Request, expectedState string) {
	query := r.URL.Query()
	result := callbackResult{code: query.Get("code"), state: query.Get("state")}
	var reason callbackReason
	switch {
	case query.Get("error") != "":
		detail := query.Get("error_description")
		if detail == "" {
			detail = query.Get("error")
		}
		result.err = errors.New("cred: authorization failed: " + detail)
		reason = reasonProviderRefused
	case expectedState != "" && result.state != expectedState:
		result.err = errors.New("cred: callback state did not match the state sent, refusing the code")
		reason = reasonStateMismatch
	case result.code == "":
		result.err = errors.New("cred: callback carried no authorization code")
		reason = reasonMissingCode
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if result.err != nil {
		w.WriteHeader(http.StatusBadRequest)
	}
	_, _ = io.WriteString(w, callbackDocument(reason))
	select {
	case c.results <- result:
	default:
	}
}

func (c *callbackServer) close() {
	_ = c.server.Close()
}

func listenLoopback(port int, ipv6 bool) ([]net.Listener, error) {
	for attempt := 0; ; attempt++ {
		primary, err := net.Listen("tcp4", net.JoinHostPort(ipv4Loopback, strconv.Itoa(port)))
		if err != nil {
			return nil, err
		}
		if !ipv6 {
			return []net.Listener{primary}, nil
		}
		bound := primary.Addr().(*net.TCPAddr).Port
		companion, err := net.Listen("tcp6", net.JoinHostPort(ipv6Loopback, strconv.Itoa(bound)))
		if err == nil {
			return []net.Listener{primary, companion}, nil
		}
		if !addressInUse(err) {
			return []net.Listener{primary}, nil
		}
		_ = primary.Close()
		if port != 0 || attempt >= ipv6CompanionAttempts {
			return nil, err
		}
	}
}

func addressInUse(err error) bool {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno == syscall.EADDRINUSE || errno == windowsAddressInUse
	}
	return strings.Contains(err.Error(), "in use")
}

func ipv6LoopbackAvailable() bool {
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	for _, address := range addresses {
		network, ok := address.(*net.IPNet)
		if ok && network.IP.IsLoopback() && network.IP.To4() == nil {
			return true
		}
	}
	return false
}
