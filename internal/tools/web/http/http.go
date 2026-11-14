package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"tofu/internal/agent/loop"
	"tofu/internal/models/llm/core"
	"tofu/internal/platform/outside"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/data/shape"
	"tofu/internal/tools/shell/process"
)

const Name = "http"

type Limits struct {
	Read, Shown, Items int
	Call               time.Duration
	ShapeContext       int
}

type HTTP struct {
	shells *process.Registry
	shaper contract.Tool
	limits Limits
}

type target struct {
	url   *url.URL
	tried string
}

func New(dir string, shells *process.Registry, limits Limits) (contract.Tool, error) {
	shaper, err := shape.New(dir, shape.Limits{Bytes: limits.Read, Context: limits.ShapeContext})
	return HTTP{shells: shells, shaper: shaper, limits: limits}, err
}

func (HTTP) Name() string { return Name }

func (h HTTP) Definition() core.Tool {
	return core.Tool{
		Name: Name,
		Description: "calls this project's own running server and says what it answered: status, ms, the headers worth reading and the body, with a JSON body given as its type in the project's language plus its first " + strconv.Itoa(h.limits.Items) + " items, so a large answer costs a few lines instead of the whole body. " +
			"give path as /orders/1/total and the server is found from the shell that listens: the one listening shell, or the shell named, or port. a full http://localhost:N/... address works too. " +
			"only this machine is called, localhost, 127.0.0.1 and ::1, and a redirect is shown rather than followed. use fetch for a page on the web, and this instead of curl in bash, which prints everything",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"method":  map[string]any{"type": "string", "description": "GET when left out"},
				"path":    map[string]any{"type": "string", "description": "the path and query, such as /orders?status=paid, or a full local address"},
				"body":    map[string]any{"type": "string", "description": "the request body, sent as JSON when it parses as JSON and no content-type is given"},
				"headers": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}, "description": "request headers by name"},
				"shell":   map[string]any{"type": "string", "description": "the shell running the server, as bash named it, when more than one listens"},
				"port":    map[string]any{"type": "integer", "description": "the local port, instead of finding it from a shell"},
			},
			"required": []string{"path"},
		},
	}
}

func (h HTTP) Run(ctx context.Context, raw json.RawMessage) (contract.Result, error) {
	var args struct {
		Method  string            `json:"method"`
		Path    string            `json:"path"`
		Body    string            `json:"body"`
		Headers map[string]string `json:"headers"`
		Shell   string            `json:"shell"`
		Port    int               `json:"port"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return contract.Result{}, fmt.Errorf("http: arguments are not the expected shape: %w", err)
	}
	method := strings.ToUpper(strings.TrimSpace(args.Method))
	if method == "" {
		method = http.MethodGet
	}
	if strings.TrimSpace(args.Path) == "" {
		return refused("path is required, such as /orders/1"), nil
	}
	to, refusal := h.target(ctx, args.Path, args.Shell, args.Port)
	if refusal != "" {
		return refused(refusal), nil
	}
	ctx, cancel := context.WithTimeout(ctx, h.limits.Call)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, to.url.String(), strings.NewReader(args.Body))
	if err != nil {
		return refused(err.Error()), nil
	}
	for key, value := range args.Headers {
		req.Header.Set(key, value)
	}
	if args.Body != "" && req.Header.Get("Content-Type") == "" && json.Valid([]byte(args.Body)) {
		req.Header.Set("Content-Type", "application/json")
	}
	client := outside.Over(outside.Network, nil, &http.Transport{DialContext: (&net.Dialer{Control: loopbackOnly}).DialContext})
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	began := time.Now()
	resp, err := client.Do(req)
	if dial := (*net.OpError)(nil); errors.As(err, &dial) && dial.Op == "dial" {
		return refused("no listener on " + to.url.Host + ", " + to.tried + ": " + strings.TrimSuffix(dial.Err.Error(), ".") + ". start the server with bash background: true, or wait for it with shell wait for port:" + to.url.Port()), nil
	}
	if err != nil {
		return refused(method + " " + to.url.String() + ", " + to.tried + ": " + err.Error()), nil
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(h.limits.Read)+1))
	if err != nil {
		return refused(method + " " + to.url.String() + ": reading the body: " + err.Error()), nil
	}
	var out strings.Builder
	fmt.Fprintf(&out, "http: %s %s, %s: %s in %d ms\n", method, to.url.String(), to.tried, resp.Status, time.Since(began).Milliseconds())
	for _, key := range []string{"Content-Type", "Content-Length", "Location", "Retry-After", "Www-Authenticate", "Set-Cookie", "Cache-Control", "Etag", "Content-Encoding", "Access-Control-Allow-Origin"} {
		if values := resp.Header.Values(key); len(values) > 0 {
			fmt.Fprintf(&out, "%s: %s\n", key, strings.Join(values, ", "))
		}
	}
	out.WriteString(h.digest(ctx, body))
	return contract.Result{Content: out.String(), Command: method + " " + to.url.Path}, nil
}

func (h HTTP) target(ctx context.Context, path, shell string, port int) (target, string) {
	if strings.Contains(path, "://") {
		parsed, err := url.Parse(path)
		if err != nil {
			return target{}, err.Error()
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return target{}, "only http and https are called, not " + parsed.Scheme
		}
		if !local(parsed.Hostname()) {
			return target{}, parsed.Hostname() + " is not this machine: http calls only a local server, on localhost, 127.0.0.1 or ::1. use fetch for a page on the web"
		}
		return target{url: parsed, tried: "as given"}, ""
	}
	at := func(port int, tried string) (target, string) {
		parsed, err := url.Parse("http://localhost:" + strconv.Itoa(port) + "/" + strings.TrimPrefix(path, "/"))
		if err != nil {
			return target{}, err.Error()
		}
		return target{url: parsed, tried: tried}, ""
	}
	if port != 0 {
		return at(port, "port as given")
	}
	registry := loop.ShellRegistryFrom(ctx)
	if registry == nil {
		registry = h.shells
	}
	listed, err := registry.List()
	if err != nil {
		return target{}, "reading the shells: " + err.Error()
	}
	running := slices.DeleteFunc(listed, func(one process.Shell) bool { return one.State != process.Running })
	listening := registry.Listening(ctx, running)
	names := make([]string, len(running))
	for i, one := range running {
		names[i] = one.Name
	}
	if shell != "" {
		found := slices.IndexFunc(running, func(one process.Shell) bool { return one.Name == shell })
		switch {
		case found < 0:
			return target{}, "no listener: no running shell is named " + shell + ", running: " + contract.Listed(names)
		case listening[shell] != 0:
			return at(listening[shell], "from shell "+shell)
		case running[found].Port != 0:
			return at(running[found].Port, "from shell "+shell+", which named port "+strconv.Itoa(running[found].Port)+" and listens on none")
		}
		return target{}, "no listener: shell " + shell + " (" + running[found].Command + ") is running and listens on no port yet: wait for it with shell wait for port:N, or give port"
	}
	serving := slices.DeleteFunc(slices.Clone(names), func(name string) bool { return listening[name] == 0 })
	switch len(serving) {
	case 0:
		return target{}, "no listener: no shell of this project listens on a port, running: " + contract.Listed(names) + ". start the server with bash background: true, or give port"
	case 1:
		return at(listening[serving[0]], "from shell "+serving[0])
	}
	for i, name := range serving {
		serving[i] = name + " on " + strconv.Itoa(listening[name])
	}
	return target{}, "more than one shell listens, " + strings.Join(serving, ", ") + ": name one as shell, or give port"
}

func (h HTTP) digest(ctx context.Context, body []byte) string {
	if len(body) == 0 {
		return "body: empty\n"
	}
	if len(body) > h.limits.Read {
		return "body: over " + strconv.Itoa(h.limits.Read) + " bytes, not read further; the first " + strconv.Itoa(h.limits.Shown) + ":\n" + cut(string(body), h.limits.Shown) + "\n"
	}
	size := "body: " + strconv.Itoa(len(body)) + " bytes"
	if !json.Valid(body) {
		return size + ", not JSON; the first " + strconv.Itoa(min(len(body), h.limits.Shown)) + ":\n" + cut(string(body), h.limits.Shown) + "\n"
	}
	sample, _ := json.Marshal(map[string]string{"sample": string(body)})
	typed, _ := h.shaper.Run(ctx, sample)
	var items []json.RawMessage
	var shown bytes.Buffer
	if json.Unmarshal(body, &items) == nil {
		for _, item := range items[:min(len(items), h.limits.Items)] {
			_ = json.Compact(&shown, item)
			shown.WriteByte('\n')
		}
		size += ", an array of " + strconv.Itoa(len(items)) + "; the first " + strconv.Itoa(min(len(items), h.limits.Items))
	} else {
		_ = json.Compact(&shown, body)
	}
	return size + ":\n" + cut(strings.TrimRight(shown.String(), "\n"), h.limits.Shown) + "\n" + typed.Content
}

func cut(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	end := limit
	for end > 0 && !utf8.RuneStart(text[end]) {
		end--
	}
	return text[:end] + " ... " + strconv.Itoa(len(text)-end) + " more bytes"
}

func local(host string) bool {
	if strings.EqualFold(host, "localhost") || strings.HasSuffix(strings.ToLower(host), ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip.IsLoopback() || ip.IsUnspecified()
}

func loopbackOnly(_, address string, _ syscall.RawConn) error {
	if host, _, err := net.SplitHostPort(address); err != nil || !local(host) {
		return errors.New(address + " is not this machine, and http calls only a local server: use fetch for a page on the web")
	}
	return nil
}

func refused(said string) contract.Result {
	return contract.Failure("http: " + said + "\n")
}
