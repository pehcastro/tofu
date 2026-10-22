package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"strings"
)

type idleHome string

const (
	homeFresh idleHome = "fresh"
	homeStub  idleHome = "stub"
	homeLogin idleHome = "login"
)

const mockLoginCode = "mock-login-code"

func startStub() (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	go func() {
		_ = http.Serve(listener, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/token"):
				_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "mock-access-token", "refresh_token": "mock-refresh-token", "expires_in": 86400,
					"account":      map[string]any{"uuid": "mock-account", "email_address": "mock@example.invalid"},
					"organization": map[string]any{"uuid": "mock-org", "name": "Mock"}})
			case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/usage"):
				_ = json.NewEncoder(w).Encode(map[string]any{"five_hour": map[string]any{"utilization": 10, "resets_at": "2099-01-01T00:00:00Z"},
					"seven_day": map[string]any{"utilization": 20, "resets_at": "2099-01-01T00:00:00Z"}})
			default:
				http.NotFound(w, r)
			}
		}))
	}()
	return "http://" + listener.Addr().String(), nil
}

func stubEnv(home idleHome, url string) ([]string, error) {
	urls := []string{"TOFU_CLAUDE_TOKEN_URL=" + url + "/token", "TOFU_CLAUDE_USAGE_URL=" + url + "/usage", "TOFU_CLAUDE_BASE_URL=" + url,
		"TOFU_CODEX_TOKEN_URL=" + url + "/token", "TOFU_CODEX_USAGE_URL=" + url + "/usage", "TOFU_UPDATE_API=" + url + "/update",
		"TOFU_MODELS_REGISTRY_URL=" + url + "/models", "TOFU_NPM_REGISTRY_URL=" + url + "/npm", "TOFU_JUDGE_ENDPOINT=" + url + "/judge",
		"TOFU_META_BASE_URL=" + url + "/meta"}
	switch home {
	case homeFresh:
		return nil, nil
	case homeStub:
		return urls, nil
	case homeLogin:
		return append(urls, "TYPESAFE_API_KEY=mock-not-a-key"), nil
	}
	return nil, fmt.Errorf("-home is fresh, stub or login, not %q", home)
}

func login(tofu string, env []string) error {
	cmd := exec.Command(tofu, "login", "llm", "claude-sub", "--paste")
	cmd.Env, cmd.Stdin = env, strings.NewReader(mockLoginCode+"\n")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("mock login: %v: %s", err, out)
	}
	return nil
}
