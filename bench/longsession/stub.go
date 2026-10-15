package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

const (
	leadMarker     = "LONGSESSION-LEAD"
	taskMarker     = "MOCKTASK"
	workMarker     = "MOCKWORK"
	workDeltas     = 60
	shellLines     = 40
	leadWait       = 5 * time.Minute
	unstartedPolls = 5
	pollPause      = time.Second
)

type mode int32

const (
	seeding mode = iota
	idling
	working
)

type stub struct {
	url       string
	agents    int
	workers   int
	batch     int
	workStep  time.Duration
	mode      atomic.Int32
	spawned   atomic.Int64
	started   atomic.Int64
	reported  atomic.Int64
	polls     atomic.Int64
	workSpawn atomic.Bool
	workSteps atomic.Int64
	asked     atomic.Int64
	ids       atomic.Int64
}

type wireMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type wireBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func startStub(agents, batch, workers int, workStep time.Duration) (*stub, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	s := &stub{url: "http://" + listener.Addr().String(), agents: agents, batch: batch, workers: workers, workStep: workStep}
	go func() { _ = http.Serve(listener, http.HandlerFunc(s.serve)) }()
	return s, nil
}

func (s *stub) env() []string {
	return []string{"TOFU_CLAUDE_TOKEN_URL=" + s.url + "/token", "TOFU_CLAUDE_USAGE_URL=" + s.url + "/usage", "TOFU_CLAUDE_BASE_URL=" + s.url,
		"TOFU_CODEX_TOKEN_URL=" + s.url + "/token", "TOFU_CODEX_USAGE_URL=" + s.url + "/usage", "TOFU_UPDATE_API=" + s.url + "/update",
		"TOFU_MODELS_REGISTRY_URL=" + s.url + "/models", "TOFU_NPM_REGISTRY_URL=" + s.url + "/npm", "TOFU_JUDGE_ENDPOINT=" + s.url + "/judge",
		"TOFU_META_BASE_URL=" + s.url + "/meta", "OPENROUTER_KEY=mock-not-a-key"}
}

func (s *stub) serve(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/token"):
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "mock-access-token", "refresh_token": "mock-refresh-token", "expires_in": 86400,
			"account":      map[string]any{"uuid": "mock-account-1", "email_address": "mock1@example.invalid"},
			"organization": map[string]any{"uuid": "mock-org", "name": "Mock"}})
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/usage"):
		_ = json.NewEncoder(w).Encode(map[string]any{"five_hour": map[string]any{"utilization": 10, "resets_at": "2099-01-01T00:00:00Z"},
			"seven_day": map[string]any{"utilization": 20, "resets_at": "2099-01-01T00:00:00Z"}})
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/count_tokens"):
		_ = json.NewEncoder(w).Encode(map[string]any{"input_tokens": 1000})
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/judge"):
		judge(w, r)
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/v1/messages"):
		s.asked.Add(1)
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
			var levels []string
			_ = json.Unmarshal(question.Criteria, &levels)
			spread := map[string]float64{"0": 1}
			for level := 1; level < len(levels); level++ {
				spread[fmt.Sprint(level)] = 0
			}
			answers[id] = map[string]any{"type": "score", "score": 0, "probabilities": spread, "confidence": 1}
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

func (s *stub) answer(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Messages []wireMessage     `json:"messages"`
		Tools    []json.RawMessage `json:"tools"`
	}
	_ = json.NewDecoder(r.Body).Decode(&request)
	said, steps := userText(request.Messages)
	w.Header().Set("Content-Type", "text/event-stream")
	reply := sse{w: w, flusher: w.(http.Flusher), id: s.ids.Add(1)}
	reply.start()
	switch {
	case len(request.Tools) == 0:
		reply.text("mock summary line")
		reply.stop("end_turn")
	case strings.Contains(said, leadMarker):
		s.lead(&reply)
	case strings.Contains(said, workMarker):
		s.work(&reply)
	case strings.Contains(said, taskMarker):
		s.task(&reply, said, steps)
	default:
		reply.text("ok")
		reply.stop("end_turn")
	}
}

func (s *stub) lead(reply *sse) {
	switch mode(s.mode.Load()) {
	case seeding:
		started := s.started.Load()
		unstarted := s.spawned.Load() - started
		if started > s.reported.Load() || unstarted > 0 && s.polls.Add(1) <= unstartedPolls {
			time.Sleep(pollPause)
			waited := s.ids.Load()
			reply.text(fmt.Sprintf("waiting for the mock batch to report, poll %d", waited))
			reply.tool("read", map[string]any{"path": fmt.Sprintf("mock%d.txt", waited%10), "offset": waited%7 + 1})
			reply.stop("tool_use")
			return
		}
		s.spawned.Store(started)
		s.polls.Store(0)
		left := s.agents - int(started)
		if left <= 0 {
			reply.text("every mock sub-agent reported")
			reply.stop("end_turn")
			return
		}
		reply.text("spawning the next mock batch")
		for range min(left, s.batch) {
			n := s.spawned.Add(1)
			reply.tool("spawn", map[string]any{"task": fmt.Sprintf("%s-%d: read mock%d.txt and mock%d.txt and answer with their first lines", taskMarker, n, n%10, (n+1)%10)})
		}
		reply.stop("tool_use")
	case working:
		if s.workSpawn.CompareAndSwap(false, true) {
			reply.text("the mock workers")
			for k := range s.workers {
				reply.tool("spawn", map[string]any{"task": fmt.Sprintf("%s-%d: keep reading the mock files, one step at a time", workMarker, k+1)})
			}
		}
		reply.text("the lead waits on the worker, as a lead waiting on a shell does")
		reply.tool("bash", map[string]any{"command": fmt.Sprintf("sleep %d; echo lead wait %d over", int(leadWait.Seconds()), s.ids.Load())})
		reply.stop("tool_use")
	case idling:
		reply.text("noted")
		reply.stop("end_turn")
	}
}

func (s *stub) task(reply *sse, said string, steps int) {
	if steps == 0 && mode(s.mode.Load()) == seeding {
		s.started.Add(1)
	}
	switch steps {
	case 0:
		reply.text("reading a mock file")
		reply.tool("read", map[string]any{"path": "mock0.txt"})
		reply.stop("tool_use")
		return
	case 1:
		reply.text("running a mock command, which leaves a shell behind as every bash call does")
		reply.tool("bash", map[string]any{"command": fmt.Sprintf("echo %s output %d; seq 1 %d", taskMarker, s.ids.Load(), shellLines)})
		reply.stop("tool_use")
		return
	}
	reply.text(taskMarker + " done: the first line is mock line 0 and the command printed its lines")
	reply.stop("end_turn")
	if mode(s.mode.Load()) == seeding && !strings.Contains(said, "did not believe you") {
		s.reported.Add(1)
	}
}

func (s *stub) work(reply *sse) {
	step := s.workSteps.Add(1)
	for k := range workDeltas {
		reply.think(fmt.Sprintf("mock worker step %d weighs part %d of the mock files before it reads the next one. ", step, k))
		time.Sleep(s.workStep / workDeltas)
	}
	reply.closeBlock()
	reply.text(fmt.Sprintf("mock worker step %d reads the next mock file", step))
	reply.tool("read", map[string]any{"path": fmt.Sprintf("mock%d.txt", step%10)})
	reply.stop("tool_use")
}

func userText(messages []wireMessage) (string, int) {
	var said strings.Builder
	steps := 0
	for _, message := range messages {
		if message.Role == "assistant" {
			steps++
			continue
		}
		var plain string
		if json.Unmarshal(message.Content, &plain) == nil {
			said.WriteString(plain + "\n")
			continue
		}
		var blocks []wireBlock
		_ = json.Unmarshal(message.Content, &blocks)
		for _, block := range blocks {
			if block.Type == "text" {
				said.WriteString(block.Text + "\n")
			}
		}
	}
	return said.String(), steps
}

type sse struct {
	w       io.Writer
	flusher http.Flusher
	id      int64
	index   int
	open    string
}

func (e *sse) event(kind string, payload map[string]any) {
	payload["type"] = kind
	line, _ := json.Marshal(payload)
	fmt.Fprintf(e.w, "event: %s\ndata: %s\n\n", kind, line)
	e.flusher.Flush()
}

func (e *sse) start() {
	e.event("message_start", map[string]any{"message": map[string]any{"id": fmt.Sprintf("msg_mock_%d", e.id), "model": "claude-sonnet-5",
		"usage": map[string]any{"input_tokens": 12, "output_tokens": 1, "cache_read_input_tokens": 2000, "cache_creation_input_tokens": 0}}})
}

func (e *sse) think(thought string) {
	if e.open == "" {
		e.event("content_block_start", map[string]any{"index": e.index, "content_block": map[string]any{"type": "thinking", "thinking": ""}})
		e.open = "thinking"
	}
	e.event("content_block_delta", map[string]any{"index": e.index, "delta": map[string]any{"type": "thinking_delta", "thinking": thought}})
}

func (e *sse) text(said string) {
	if e.open == "" {
		e.event("content_block_start", map[string]any{"index": e.index, "content_block": map[string]any{"type": "text", "text": ""}})
		e.open = "text"
	}
	e.event("content_block_delta", map[string]any{"index": e.index, "delta": map[string]any{"type": "text_delta", "text": said}})
}

func (e *sse) closeBlock() {
	if e.open == "thinking" {
		e.event("content_block_delta", map[string]any{"index": e.index, "delta": map[string]any{"type": "signature_delta", "signature": "mock-signature"}})
	}
	if e.open != "" {
		e.event("content_block_stop", map[string]any{"index": e.index})
		e.index++
		e.open = ""
	}
}

func (e *sse) tool(name string, args map[string]any) {
	e.closeBlock()
	raw, _ := json.Marshal(args)
	e.event("content_block_start", map[string]any{"index": e.index, "content_block": map[string]any{"type": "tool_use", "id": fmt.Sprintf("toolu_mock_%d_%d", e.id, e.index), "name": name, "input": map[string]any{}}})
	e.event("content_block_delta", map[string]any{"index": e.index, "delta": map[string]any{"type": "input_json_delta", "partial_json": string(raw)}})
	e.event("content_block_stop", map[string]any{"index": e.index})
	e.index++
}

func (e *sse) stop(reason string) {
	e.closeBlock()
	e.event("message_delta", map[string]any{"delta": map[string]any{"stop_reason": reason}, "usage": map[string]any{"output_tokens": 40}})
	e.event("message_stop", map[string]any{})
}
