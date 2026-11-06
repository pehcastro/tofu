package record

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"tofu/internal/models/llm/core"
	"tofu/internal/platform/scratch"
	"tofu/internal/tools/contract"
)

const (
	Name      = "record"
	nameRunes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-"
)

type mode string

const (
	start  mode = "start"
	replay mode = "replay"
)

type Limits struct {
	Run         time.Duration
	Body, Lines int
}

type Record struct {
	root   contract.Root
	limits Limits
}

type exchange struct {
	Method string      `json:"method"`
	URL    string      `json:"url"`
	Status int         `json:"status"`
	Header http.Header `json:"header"`
	Body   []byte      `json:"body"`
}

type tape struct {
	mu        sync.Mutex
	mode      mode
	exchanges []exchange
	served    map[string]int
	misses    []string
	bodyCap   int
	upstream  *http.Transport
}

func New(dir string, limits Limits) (contract.Tool, error) {
	root, err := contract.NewRoot(dir)
	return Record{root: root, limits: limits}, err
}

func (Record) Name() string { return Name }

func (Record) Definition() core.Tool {
	return core.Tool{
		Name: Name,
		Description: "records the plain http a command makes and replays it later without the network, so a test that calls an api runs with the api down and without a hand written fake. " +
			"mode start runs the command once under a proxy on 127.0.0.1 that only that command sees, through HTTP_PROXY and http_proxy, saves every request and its answer under the name, and stops the proxy when the command exits. " +
			"mode replay runs the command again under a proxy that answers only from that recording; a request with no recording fails and is named by its url. " +
			"https is not intercepted: a CONNECT is refused, so record against http urls. a client that skips the proxy for localhost, like Go's net/http, is not seen; curl and python requests are. " +
			"give command as words, like [\"curl\",\"-s\",\"http://127.0.0.1:8080/orders/1\"]. commands outside record have no proxy set",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"mode":    map[string]any{"type": "string", "enum": []string{string(start), string(replay)}, "description": "start to record the command's http under name, replay to serve it back"},
				"name":    map[string]any{"type": "string", "description": "the recording's name, letters, digits, dash and underscore"},
				"command": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "the command as words"},
			},
			"required": []string{"mode", "name", "command"},
		},
	}
}

func (r Record) Run(ctx context.Context, raw json.RawMessage) (contract.Result, error) {
	var args struct {
		Mode    mode     `json:"mode"`
		Name    string   `json:"name"`
		Command []string `json:"command"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return contract.Result{}, fmt.Errorf("record: arguments are not the expected shape: %w", err)
	}
	if args.Mode != start && args.Mode != replay {
		return refused("mode must be start or replay, not " + strconv.Quote(string(args.Mode))), nil
	}
	if args.Name == "" || strings.ContainsFunc(args.Name, func(c rune) bool { return !strings.ContainsRune(nameRunes, c) }) {
		return refused("name must be letters, digits, dash and underscore, not " + strconv.Quote(args.Name)), nil
	}
	if len(args.Command) == 0 {
		return refused("command is required, as words"), nil
	}
	program, err := exec.LookPath(args.Command[0])
	if err != nil {
		return refused(args.Command[0] + " is not on PATH"), nil
	}
	folder, err := r.folder(ctx)
	if err != nil {
		return contract.Result{}, fmt.Errorf("record: %w", err)
	}
	file := filepath.Join(folder, args.Name+".json")
	recorded := &tape{mode: args.Mode, served: map[string]int{}, bodyCap: r.limits.Body, upstream: &http.Transport{}}
	if args.Mode == replay {
		body, err := os.ReadFile(file)
		if errors.Is(err, fs.ErrNotExist) {
			return refused("no recording named " + args.Name + " at " + file + ": run mode start first"), nil
		}
		if err == nil {
			err = json.Unmarshal(body, &recorded.exchanges)
		}
		if err != nil {
			return contract.Result{}, fmt.Errorf("record: reading %s: %w", file, err)
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return contract.Result{}, fmt.Errorf("record: %w", err)
	}
	proxy := &http.Server{Handler: recorded}
	go func() { _ = proxy.Serve(listener) }()
	ctx, cancel := context.WithTimeout(ctx, r.limits.Run)
	defer cancel()
	cmd := exec.CommandContext(ctx, program, args.Command[1:]...)
	cmd.Dir = string(r.root)
	cmd.Env = proxiedEnv("http://" + listener.Addr().String())
	out, runErr := cmd.CombinedOutput()
	_ = proxy.Close()
	recorded.upstream.CloseIdleConnections()
	recorded.mu.Lock()
	defer recorded.mu.Unlock()
	lines := strings.Split(strings.TrimRight(string(out), "\r\n"), "\n")
	shown := strings.Join(lines[max(0, len(lines)-r.limits.Lines):], "\n") + "\n"
	if len(recorded.misses) > 0 {
		return refused("replay " + args.Name + ": no recording for " + strings.Join(recorded.misses, ", ") + "\n" + shown), nil
	}
	if runErr != nil {
		return refused(strings.Join(args.Command, " ") + " failed, " + runErr.Error() + ":\n" + shown), nil
	}
	if args.Mode == replay {
		answered := 0
		for _, n := range recorded.served {
			answered += n
		}
		return contract.Result{Content: shown + fmt.Sprintf("record: answered %d requests from the %d recorded in %s\n", answered, len(recorded.exchanges), file), Command: Name}, nil
	}
	body, err := json.MarshalIndent(recorded.exchanges, "", "  ")
	if err == nil {
		err = os.MkdirAll(folder, 0o755)
	}
	if err == nil {
		err = os.WriteFile(file, body, 0o644)
	}
	if err != nil {
		return contract.Result{}, fmt.Errorf("record: saving %s: %w", file, err)
	}
	var saved strings.Builder
	fmt.Fprintf(&saved, "record: saved %d requests as %s at %s\n", len(recorded.exchanges), args.Name, file)
	for _, e := range recorded.exchanges {
		fmt.Fprintf(&saved, "%s %s %d, %d bytes\n", e.Method, e.URL, e.Status, len(e.Body))
	}
	return contract.Result{Content: shown + saved.String(), Command: Name}, nil
}

func (r Record) folder(ctx context.Context) (string, error) {
	if place, found := scratch.ScratchOf(ctx); found {
		return filepath.Join(place.SessionDir(), Name), nil
	}
	root, err := scratch.ScratchRootAt(string(r.root))
	return filepath.Join(root, scratch.ScratchShared, Name), err
}

func proxiedEnv(proxy string) []string {
	var env []string
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		switch strings.ToUpper(key) {
		case "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY":
		default:
			env = append(env, kv)
		}
	}
	return append(env, "HTTP_PROXY="+proxy, "http_proxy="+proxy)
}

func (t *tape) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method == http.MethodConnect || !req.URL.IsAbs() {
		http.Error(w, "record: only plain http through the proxy is recorded, https is not intercepted", http.StatusNotImplemented)
		return
	}
	key := req.Method + " " + req.URL.String()
	if t.mode == replay {
		t.answer(w, key)
		return
	}
	out, err := http.NewRequestWithContext(req.Context(), req.Method, req.URL.String(), req.Body)
	if err != nil {
		http.Error(w, "record: "+err.Error(), http.StatusBadGateway)
		return
	}
	out.Header = req.Header.Clone()
	out.Header.Del("Proxy-Connection")
	resp, err := t.upstream.RoundTrip(out)
	if err != nil {
		http.Error(w, "record: "+key+": "+err.Error(), http.StatusBadGateway)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(t.bodyCap)+1))
	if err == nil && len(body) > t.bodyCap {
		err = fmt.Errorf("the answer is larger than %d bytes", t.bodyCap)
	}
	if err != nil {
		http.Error(w, "record: "+key+": "+err.Error(), http.StatusBadGateway)
		return
	}
	header := resp.Header.Clone()
	for _, hop := range []string{"Content-Length", "Transfer-Encoding", "Connection"} {
		header.Del(hop)
	}
	t.mu.Lock()
	t.exchanges = append(t.exchanges, exchange{Method: req.Method, URL: req.URL.String(), Status: resp.StatusCode, Header: header, Body: body})
	t.mu.Unlock()
	write(w, header, resp.StatusCode, body)
}

func (t *tape) answer(w http.ResponseWriter, key string) {
	t.mu.Lock()
	var matching []exchange
	for _, e := range t.exchanges {
		if e.Method+" "+e.URL == key {
			matching = append(matching, e)
		}
	}
	if len(matching) == 0 {
		t.misses = append(t.misses, key)
		t.mu.Unlock()
		http.Error(w, "record: no recording for "+key, http.StatusBadGateway)
		return
	}
	e := matching[min(t.served[key], len(matching)-1)]
	t.served[key]++
	t.mu.Unlock()
	write(w, e.Header, e.Status, e.Body)
}

func write(w http.ResponseWriter, header http.Header, status int, body []byte) {
	for key, values := range header {
		w.Header()[key] = values
	}
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func refused(said string) contract.Result {
	return contract.Failure("record: " + said + "\n")
}
