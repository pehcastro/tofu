package main

import (
	"bufio"
	"cmp"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

type tokens struct {
	FreshIn    int `json:"fresh_in"`
	CacheRead  int `json:"cache_read"`
	CacheWrite int `json:"cache_write"`
	Out        int `json:"out"`
}

func (t tokens) in() int {
	return t.FreshIn + t.CacheRead + t.CacheWrite
}

func (t *tokens) add(fresh, read, write, out int) {
	t.FreshIn, t.CacheRead, t.CacheWrite, t.Out = t.FreshIn+fresh, t.CacheRead+read, t.CacheWrite+write, t.Out+out
}

type call struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	MS     int64  `json:"ms"`
	Failed bool   `json:"failed"`
}

type requestTime struct {
	AtMS    int64 `json:"at_ms"`
	MS      int64 `json:"ms"`
	FirstMS int64 `json:"first_ms"`
	Out     int   `json:"out"`
}

type requestClock struct {
	origin, sent, first time.Time
}

func (c *requestClock) done(at time.Time, out int) requestTime {
	c.origin = cmp.Or(c.origin, c.sent)
	first := cmp.Or(c.first, at)
	timed := requestTime{AtMS: c.sent.Sub(c.origin).Milliseconds(), MS: at.Sub(c.sent).Milliseconds(), FirstMS: first.Sub(c.sent).Milliseconds(), Out: out}
	c.sent, c.first = time.Time{}, time.Time{}
	return timed
}

type transcript struct {
	model, credential string
	models            []string
	requests          int
	tokens            tokens
	calls             []call
	timed             []requestTime
	notes             []string
}

func (t *transcript) saw(model string) {
	if model != "" && !slices.Contains(t.models, model) {
		t.models = append(t.models, model)
	}
}

func parse(chosen arm, lines []stamped, dirs runDirs, env []string) transcript {
	switch chosen.Tool {
	case "claude":
		return parseClaude(lines)
	case "codex":
		t := parseCodex(lines)
		t.credential = firstLine(toolOutput(dirs.repo, env, "codex", "login", "status"))
		return t
	case "omp":
		return parseOmp(lines, dirs.sessions)
	case "tofu":
		return parseTofu(lines, dirs.repo, env, strings.HasPrefix(chosen.AskedModel, "codex-sub/"))
	}
	panic("unknown tool " + chosen.Tool)
}

func retried(calls []call) int {
	n := 0
	lastFailed := map[string]bool{}
	for _, c := range calls {
		if lastFailed[c.Kind] {
			n++
		}
		lastFailed[c.Kind] = c.Failed
	}
	return n
}

type openCalls struct {
	calls []call
	start map[string]int64
	index map[string]int
}

func newOpenCalls() *openCalls {
	return &openCalls{start: map[string]int64{}, index: map[string]int{}}
}

func (o *openCalls) begin(id, name, kind string, ms int64) {
	o.start[id], o.index[id] = ms, len(o.calls)
	o.calls = append(o.calls, call{Kind: kind, Name: name, MS: -1})
}

func (o *openCalls) end(id string, ms int64, failed bool) {
	i, known := o.index[id]
	if !known {
		return
	}
	o.calls[i].MS, o.calls[i].Failed = ms-o.start[id], failed
}

func decode(text string) map[string]any {
	var v map[string]any
	_ = json.Unmarshal([]byte(text), &v)
	return v
}

func str(v any, path ...string) string {
	for _, key := range path {
		m, _ := v.(map[string]any)
		v = m[key]
	}
	s, _ := v.(string)
	return s
}

func num(v any, path ...string) int {
	for _, key := range path {
		m, _ := v.(map[string]any)
		v = m[key]
	}
	f, _ := v.(float64)
	return int(f)
}

func list(v any, key string) []any {
	m, _ := v.(map[string]any)
	l, _ := m[key].([]any)
	return l
}

func claudeKind(name string) string {
	switch name {
	case "Read":
		return "read"
	case "Edit", "MultiEdit", "NotebookEdit":
		return "edit"
	case "Write":
		return "write"
	case "Grep", "Glob":
		return "search"
	case "Bash", "PowerShell":
		return "bash"
	case "Task", "Agent":
		return "spawn"
	}
	return "other"
}

func parseClaude(lines []stamped) transcript {
	t := transcript{}
	open := newOpenCalls()
	messages := map[string]bool{}
	for _, line := range lines {
		event := decode(line.text)
		switch str(event, "type") {
		case "system":
			if str(event, "subtype") == "init" {
				t.model = str(event, "model")
				t.credential = "apiKeySource " + str(event, "apiKeySource")
			}
		case "assistant":
			messages[str(event, "message", "id")] = true
			for _, block := range list(event["message"], "content") {
				if str(block, "type") == "tool_use" {
					open.begin(str(block, "id"), str(block, "name"), claudeKind(str(block, "name")), line.ms)
				}
			}
		case "user":
			for _, block := range list(event["message"], "content") {
				if str(block, "type") == "tool_result" {
					failed, _ := block.(map[string]any)["is_error"].(bool)
					open.end(str(block, "tool_use_id"), line.ms, failed)
				}
			}
		case "result":
			usage, _ := event["modelUsage"].(map[string]any)
			for model, u := range usage {
				t.saw(model)
				t.tokens.add(num(u, "inputTokens"), num(u, "cacheReadInputTokens"), num(u, "cacheCreationInputTokens"), num(u, "outputTokens"))
			}
		}
	}
	t.requests, t.calls = len(messages), open.calls
	return t
}

func parseCodex(lines []stamped) transcript {
	t := transcript{}
	open := newOpenCalls()
	thread := ""
	for _, line := range lines {
		event := decode(line.text)
		item, _ := event["item"].(map[string]any)
		id, kind := str(item, "id"), codexKind(item)
		switch str(event, "type") {
		case "thread.started":
			thread = str(event, "thread_id")
		case "item.started":
			if kind != "" {
				open.begin(id, str(item, "type"), kind, line.ms)
			}
		case "item.completed":
			if kind == "" {
				continue
			}
			exit, hasExit := item["exit_code"].(float64)
			failed := (hasExit && exit != 0) || str(item, "status") == "failed" || str(item, "status") == "declined"
			if _, began := open.index[id]; !began {
				open.calls = append(open.calls, call{Kind: kind, Name: str(item, "type"), MS: -1, Failed: failed})
				continue
			}
			open.end(id, line.ms, failed)
		case "turn.completed":
			usage := event["usage"]
			t.tokens = tokens{FreshIn: num(usage, "input_tokens") - num(usage, "cached_input_tokens"), CacheRead: num(usage, "cached_input_tokens"),
				CacheWrite: num(usage, "cache_write_input_tokens"), Out: num(usage, "output_tokens")}
		}
	}
	t.calls = open.calls
	t.model, t.requests = codexRollout(thread, &t)
	return t
}

func codexKind(item map[string]any) string {
	switch str(item, "type") {
	case "command_execution":
		return "bash"
	case "file_change":
		for _, change := range list(item, "changes") {
			if str(change, "kind") != "add" {
				return "edit"
			}
		}
		return "write"
	case "agent_message", "reasoning", "todo_list", "error", "":
		return ""
	}
	return "other"
}

func codexRollout(thread string, t *transcript) (string, int) {
	home, _ := os.UserHomeDir()
	var found string
	_ = filepath.WalkDir(filepath.Join(home, ".codex", "sessions"), func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && thread != "" && strings.Contains(d.Name(), thread) {
			found = path
			return fs.SkipAll
		}
		return nil
	})
	if found == "" {
		t.notes = append(t.notes, "codex rollout for thread "+thread+" not found, so requests and the model build are unknown")
		return "", 0
	}
	file, err := os.Open(found)
	if err != nil {
		t.notes = append(t.notes, err.Error())
		return "", 0
	}
	defer func() { _ = file.Close() }()
	model, totals := "", map[int]bool{}
	var clock requestClock
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1<<20), 64<<20)
	for scanner.Scan() {
		event := decode(scanner.Text())
		payload := event["payload"]
		if str(event, "type") == "turn_context" {
			model = str(payload, "model")
			t.saw(model)
		}
		if str(payload, "type") == "token_count" {
			if total := num(payload, "info", "total_token_usage", "total_tokens"); total > 0 {
				totals[total] = true
			}
		}
		at, err := time.Parse(time.RFC3339Nano, str(event, "timestamp"))
		if err != nil {
			continue
		}
		kind, role, item := str(payload, "type"), str(payload, "role"), str(payload, "item", "type")
		switch {
		case str(event, "type") == "token_usage_record" && !clock.sent.IsZero():
			t.timed = append(t.timed, clock.done(at, num(payload, "usage", "output_tokens")))
		case kind == "custom_tool_call_output" || kind == "function_call_output" || kind == "message" && role == "user" && clock.first.IsZero():
			clock.sent = at
		case clock.first.IsZero() && !clock.sent.IsZero() && (slices.Contains([]string{"reasoning", "custom_tool_call", "function_call"}, kind) ||
			kind == "message" && role == "assistant" || kind == "item_completed" && (item == "Reasoning" || item == "AgentMessage")):
			clock.first = at
		}
	}
	return model, len(totals)
}

func ompKind(name string) string {
	switch name {
	case "read":
		return "read"
	case "edit", "ast_edit":
		return "edit"
	case "write":
		return "write"
	case "grep", "find", "glob", "search", "ast_grep", "lsp":
		return "search"
	case "bash":
		return "bash"
	case "task":
		return "spawn"
	}
	return "other"
}

func parseOmp(lines []stamped, sessions string) transcript {
	t := transcript{}
	open := newOpenCalls()
	seen := map[string]bool{}
	addAssistant := func(message any) {
		if str(message, "role") != "assistant" || seen[str(message, "responseId")] {
			return
		}
		seen[str(message, "responseId")] = true
		t.model = cmp.Or(str(message, "upstreamModel"), str(message, "model"), t.model)
		t.saw(cmp.Or(str(message, "upstreamModel"), str(message, "model")))
		t.credential = fmt.Sprintf("provider %s through omp's own login, credential %d", str(message, "provider"), num(message, "credentialId"))
		t.tokens.add(num(message, "usage", "input"), num(message, "usage", "cacheRead"), num(message, "usage", "cacheWrite"), num(message, "usage", "output"))
	}
	for _, line := range lines {
		event := decode(line.text)
		switch str(event, "type") {
		case "message_end":
			addAssistant(event["message"])
		case "tool_execution_start":
			open.begin(str(event, "toolCallId"), str(event, "toolName"), ompKind(str(event, "toolName")), line.ms)
		case "tool_execution_end":
			failed, _ := event["isError"].(bool)
			open.end(str(event, "toolCallId"), line.ms, failed)
		}
	}
	fromStream := len(seen)
	_ = filepath.WalkDir(sessions, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		for _, line := range strings.Split(string(body), "\n") {
			if entry := decode(line); entry != nil {
				addAssistant(entry["message"])
			}
		}
		return nil
	})
	if len(seen) > fromStream {
		t.notes = append(t.notes, fmt.Sprintf("%d model requests came from sub-agent session files, their tool calls are not timed", len(seen)-fromStream))
	}
	t.requests, t.calls = len(seen), open.calls
	return t
}

var tofuTurn = regexp.MustCompile(`^turn \S+ outcome (\S+) model (\S+) asked_as (\S+) spend (.+) wall_clock_ms`)

func tofuKind(name string) string {
	switch name {
	case "read":
		return "read"
	case "edit":
		return "edit"
	case "write":
		return "write"
	case "glob", "search", "reference":
		return "search"
	case "bash":
		return "bash"
	case "spawn", "message", "subagents":
		return "spawn"
	}
	return "other"
}

func parseTofu(lines []stamped, repo string, env []string, codexWire bool) transcript {
	t := transcript{}
	for _, line := range lines {
		if m := tofuTurn.FindStringSubmatch(line.text); m != nil && t.model == "" {
			t.model, t.credential = m[2], "tofu's own "+strings.SplitN(m[3], "/", 2)[0]+" login, spend "+m[4]
			t.notes = append(t.notes, "turn outcome "+m[1])
		}
	}
	listed := decode(toolOutput(repo, env, "tofu", "session", "list", "--json"))
	sessions := list(listed["data"], "sessions")
	if len(sessions) == 0 {
		t.notes = append(t.notes, "no tofu session found in the run folder")
		return t
	}
	id := str(sessions[0], "id")
	trace := decode(toolOutput(repo, env, "tofu", "session", "trace", id, "--json"))
	var clock requestClock
	for _, request := range list(trace["data"], "requests") {
		in, read := num(request, "usage", "input_tokens"), num(request, "usage", "cache_read_tokens")
		if codexWire {
			in -= read
		}
		t.requests++
		t.saw(str(request, "model"))
		t.tokens.add(in, read, num(request, "usage", "cache_write_tokens"), num(request, "usage", "output_tokens"))
		detail := decode(toolOutput(repo, env, "tofu", "session", "request", id, str(request, "request"), "--json"))["data"]
		if sent, err := time.Parse(time.RFC3339Nano, str(detail, "at")); err == nil {
			clock.sent = sent
			if first := num(detail, "response", "FirstTokenMS"); first > 0 {
				clock.first = sent.Add(time.Duration(first) * time.Millisecond)
			}
			t.timed = append(t.timed, clock.done(sent.Add(time.Duration(num(detail, "duration_ms"))*time.Millisecond), num(request, "usage", "output_tokens")))
		}
	}
	for _, c := range list(trace["data"], "calls") {
		ms := int64(num(c, "duration_ms"))
		if _, timed := c.(map[string]any)["duration_ms"]; !timed && str(c, "outcome") != "ran" {
			ms = -1
		}
		t.calls = append(t.calls, call{Kind: tofuKind(str(c, "tool")), Name: str(c, "tool"), MS: ms, Failed: str(c, "outcome") != "ran"})
	}
	if agents := list(trace["data"], "agents"); len(agents) > 0 {
		t.notes = append(t.notes, fmt.Sprintf("%d sub-agents", len(agents)))
	}
	return t
}
