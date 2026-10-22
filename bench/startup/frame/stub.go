package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type stub struct {
	url    string
	mu     sync.Mutex
	asked  []stubRequest
	tokens atomic.Int64
}

type stubRequest struct {
	At   time.Time
	What string
}

func startStub(delay time.Duration) (*stub, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	s := &stub{url: "http://" + listener.Addr().String()}
	go func() { _ = http.Serve(listener, s.handler(delay)) }()
	return s, nil
}

func (s *stub) handler(delay time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.asked = append(s.asked, stubRequest{At: time.Now(), What: r.Method + " " + r.URL.Path})
		s.mu.Unlock()
		time.Sleep(delay)
		switch {
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/token"):
			n := s.tokens.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "mock-access-token", "refresh_token": "mock-refresh-token", "expires_in": 86400,
				"account":      map[string]any{"uuid": fmt.Sprintf("mock-account-%d", n), "email_address": fmt.Sprintf("mock%d@example.invalid", n)},
				"organization": map[string]any{"uuid": "mock-org", "name": "Mock"}})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/usage"):
			_ = json.NewEncoder(w).Encode(map[string]any{"five_hour": map[string]any{"utilization": 10, "resets_at": "2099-01-01T00:00:00Z"},
				"seven_day": map[string]any{"utilization": 20, "resets_at": "2099-01-01T00:00:00Z"}})
		default:
			http.NotFound(w, r)
		}
	})
}

func (s *stub) env() []string {
	return []string{"TOFU_CLAUDE_TOKEN_URL=" + s.url + "/token", "TOFU_CLAUDE_USAGE_URL=" + s.url + "/usage", "TOFU_CLAUDE_BASE_URL=" + s.url,
		"TOFU_CODEX_TOKEN_URL=" + s.url + "/token", "TOFU_CODEX_USAGE_URL=" + s.url + "/usage", "TOFU_UPDATE_API=" + s.url + "/update",
		"TOFU_MODELS_REGISTRY_URL=" + s.url + "/models", "TOFU_NPM_REGISTRY_URL=" + s.url + "/npm", "TOFU_JUDGE_ENDPOINT=" + s.url + "/judge",
		"TOFU_META_BASE_URL=" + s.url + "/meta", "TYPESAFE_API_KEY=mock-not-a-key"}
}

func (s *stub) between(from, to time.Time) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var seen []string
	for _, asked := range s.asked {
		if !asked.At.Before(from) && asked.At.Before(to) {
			seen = append(seen, asked.What)
		}
	}
	return seen
}
