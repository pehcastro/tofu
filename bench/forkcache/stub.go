package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"tofu/internal/llm/wire/anthropic"
)

const (
	reportedModel   = "claude-sonnet-5"
	asideReplyBytes = 1200
	projectInBody   = "W:\\\\mock\\\\project"
	leastFileBytes  = 400
	fillerLine      = "mock filler %05d\n"
)

var markerPattern = regexp.MustCompile(`MOCKHELD-\d{6}`)

type exchange struct {
	step     bool
	billed   usage
	look     reading
	choice   string
	head     int
	messages int
	body     string
	blocks   []block
}

type lookup struct {
	reply          int
	source, marker string
}

type stand struct {
	*httptest.Server
	project       string
	projectQuoted string
	played        script
	held          sync.Mutex
	cache         promptCache
	ratio         bytesPerThousandTokens
	sent          []exchange
	lookups       []lookup
	steps         int
	markers       int
	failed        error
}

func startStand(project string, played script, ratio bytesPerThousandTokens) *stand {
	absolute, _ := filepath.Abs(project)
	quoted, _ := json.Marshal(absolute)
	s := &stand{project: project, projectQuoted: strings.Trim(string(quoted), `"`), played: played, cache: promptCache{}, ratio: ratio}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	return s
}

func (s *stand) env() []string {
	env := []string{"TYPESAFE_API_KEY=mock-not-a-key", "TOFU_CLAUDE_BASE_URL=" + s.URL}
	for _, pointed := range []string{"CLAUDE_TOKEN_URL=/token", "CLAUDE_USAGE_URL=/usage", "CODEX_TOKEN_URL=/token", "CODEX_USAGE_URL=/usage", "UPDATE_API=/update",
		"MODELS_REGISTRY_URL=/models", "NPM_REGISTRY_URL=/npm", "JUDGE_ENDPOINT=/judge", "META_BASE_URL=/meta"} {
		name, path, _ := strings.Cut(pointed, "=")
		env = append(env, "TOFU_"+name+"="+s.URL+path)
	}
	return env
}

func (s *stand) serve(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/token"):
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": anthropic.OAuthTokenMarker + "-mock-access-token", "refresh_token": "mock-refresh-token", "expires_in": 86400,
			"account":      map[string]any{"uuid": "mock-account-1", "email_address": "mock1@example.invalid"},
			"organization": map[string]any{"uuid": "mock-org", "name": "Mock"}})
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/usage"):
		_ = json.NewEncoder(w).Encode(map[string]any{"five_hour": map[string]any{"utilization": 10, "resets_at": "2099-01-01T00:00:00Z"},
			"seven_day": map[string]any{"utilization": 20, "resets_at": "2099-01-01T00:00:00Z"}})
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/count_tokens"):
		body, _ := io.ReadAll(r.Body)
		_ = json.NewEncoder(w).Encode(map[string]any{"input_tokens": s.ratio.tokens(len(body))})
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/judge"):
		judge(w, r)
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/v1/messages"):
		s.answer(w, r)
	default:
		http.NotFound(w, r)
	}
}

func judge(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Questions map[string]struct {
			Type     string          `json:"type"`
			Criteria json.RawMessage `json:"criteria"`
		} `json:"questions"`
	}
	_ = json.NewDecoder(r.Body).Decode(&request)
	answers := map[string]any{}
	for id, question := range request.Questions {
		switch question.Type {
		case "score":
			var levels []json.RawMessage
			_ = json.Unmarshal(question.Criteria, &levels)
			lowest := map[string]float64{"0": 1}
			for level := 1; level < len(levels); level++ {
				lowest[fmt.Sprint(level)] = 0
			}
			answers[id] = map[string]any{"type": "score", "score": 0, "probabilities": lowest, "confidence": 1}
		case "noul":
			answers[id] = map[string]any{"type": "noul", "noul": 0}
		case "choice":
			var options map[string]string
			_ = json.Unmarshal(question.Criteria, &options)
			first := ""
			for option := range options {
				first = max(first, option)
			}
			answers[id] = map[string]any{"type": "choice", "choice": first, "probabilities": map[string]float64{first: 1}, "confidence": 1}
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"model": "typesafe/jev-mock", "provider": "stub", "id": "mock-judge", "answers": answers,
		"usage": map[string]any{"input_tokens": 100, "output_tokens": 1, "cost": 0}})
}

func (s *stand) answer(w http.ResponseWriter, r *http.Request) {
	read, _ := io.ReadAll(r.Body)
	raw := []byte(strings.ReplaceAll(string(read), s.projectQuoted, projectInBody))
	var body wireBody
	_ = json.Unmarshal(raw, &body)
	s.held.Lock()
	defer s.held.Unlock()
	blocks := blocksOf(body)
	billed, look := s.cache.bill(blocks, s.ratio)
	sent := exchange{step: len(body.Tools) > 0 && body.ToolChoice["type"] != "none", billed: billed, look: look, choice: body.choice(),
		head: len(body.Tools) + len(body.System), messages: len(body.Messages), body: string(raw), blocks: blocks}
	s.sent = append(s.sent, sent)
	reply := sse{w: w, flusher: w.(http.Flusher), ratio: s.ratio}
	reply.start(len(s.sent), sent.billed)
	if !sent.step {
		reply.text("MOCK state " + strings.Repeat("x", asideReplyBytes))
		reply.stop("end_turn")
		return
	}
	at := s.steps
	s.steps++
	if at >= len(s.played.steps) {
		reply.text("mock done")
		reply.stop("end_turn")
		return
	}
	step := s.played.steps[at]
	reply.text("mock " + strings.Repeat("y", step.saidBytes))
	for _, call := range step.calls {
		args, err := s.called(call)
		if err != nil {
			s.failed = cmp.Or(s.failed, err)
			continue
		}
		reply.tool(call.tool, args)
	}
	reason := "tool_use"
	if len(step.calls) == 0 {
		reason = "end_turn"
	}
	reply.stop(reason)
}

func (s *stand) called(call mockCall) (map[string]any, error) {
	switch call.tool {
	case "glob":
		return map[string]any{"pattern": "mock/*"}, nil
	case "grep":
		return map[string]any{"pattern": fmt.Sprintf(strings.TrimSpace(fillerLine), 1), "glob": "mock/*"}, nil
	case "write":
		return map[string]any{"path": call.target, "content": s.content(call.bytes)}, nil
	}
	path := filepath.Join(s.project, filepath.FromSlash(call.target))
	if _, err := os.Stat(path); err != nil {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, []byte(s.content(call.bytes)), 0o644); err != nil {
			return nil, err
		}
	}
	held, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	args, source := map[string]any{"path": call.target}, call.target
	if call.tool == "bash" {
		source = "cat " + call.target
		args = map[string]any{"command": source}
	}
	s.lookups = append(s.lookups, lookup{reply: len(s.sent) - 1, source: source, marker: markerPattern.FindString(string(held))})
	return args, nil
}

func (s *stand) content(size int) string {
	s.markers++
	marker := fmt.Sprintf("MOCKHELD-%06d\n", s.markers)
	var filler strings.Builder
	for line := 1; filler.Len() < max(size, leastFileBytes)-len(marker); line++ {
		fmt.Fprintf(&filler, fillerLine, line)
	}
	return filler.String() + marker
}

type sse struct {
	w       io.Writer
	flusher http.Flusher
	id      int
	index   int
	open    bool
	said    int
	ratio   bytesPerThousandTokens
}

func (e *sse) event(kind string, payload map[string]any) {
	payload["type"] = kind
	line, _ := json.Marshal(payload)
	_, _ = fmt.Fprintf(e.w, "event: %s\ndata: %s\n\n", kind, line)
	e.flusher.Flush()
}

func (e *sse) start(id int, billed usage) {
	e.id = id
	e.event("message_start", map[string]any{"message": map[string]any{"id": fmt.Sprintf("msg_mock_%d", id), "model": reportedModel,
		"usage": map[string]any{"input_tokens": billed.input, "output_tokens": 1, "cache_read_input_tokens": billed.read, "cache_creation_input_tokens": billed.written(),
			"cache_creation": map[string]any{"ephemeral_5m_input_tokens": billed.write5m, "ephemeral_1h_input_tokens": billed.write1h}}}})
}

func (e *sse) text(said string) {
	if !e.open {
		e.event("content_block_start", map[string]any{"index": e.index, "content_block": map[string]any{"type": "text", "text": ""}})
		e.open = true
	}
	e.said += len(said)
	e.event("content_block_delta", map[string]any{"index": e.index, "delta": map[string]any{"type": "text_delta", "text": said}})
}

func (e *sse) closeText() {
	if e.open {
		e.event("content_block_stop", map[string]any{"index": e.index})
		e.index++
		e.open = false
	}
}

func (e *sse) tool(name string, args map[string]any) {
	e.closeText()
	raw, _ := json.Marshal(args)
	e.said += len(raw)
	e.event("content_block_start", map[string]any{"index": e.index, "content_block": map[string]any{"type": "tool_use", "id": fmt.Sprintf("toolu_mock_%d_%d", e.id, e.index), "name": name, "input": map[string]any{}}})
	e.event("content_block_delta", map[string]any{"index": e.index, "delta": map[string]any{"type": "input_json_delta", "partial_json": string(raw)}})
	e.event("content_block_stop", map[string]any{"index": e.index})
	e.index++
}

func (e *sse) stop(reason string) {
	e.closeText()
	e.event("message_delta", map[string]any{"delta": map[string]any{"stop_reason": reason}, "usage": map[string]any{"output_tokens": max(1, e.ratio.tokens(e.said))}})
	e.event("message_stop", map[string]any{})
}
