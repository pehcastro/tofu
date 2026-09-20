package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"boji/internal/llm/cred"
)

const (
	testAccess     = "sk-ant-oat-not-a-real-token"
	testEmail      = "person@example.test"
	testKey        = "sk-or-v1-not-a-real-key"
	jevNoulAnswer  = `{"model":"jev-2026-09-01","answers":{"reachable":{"type":"noul","noul":0.97}},"usage":{"input_tokens":41,"output_tokens":1,"cost":0.00004}}`
	jevRefusesAKey = `{"error":{"message":"No auth credentials found","code":401}}`
)

func runLogin(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := loginVerb(args, strings.NewReader(""), &out, &errOut)
	return out.String(), errOut.String(), code
}

func emptyHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	t.Setenv("OPENROUTER_KEY", "")
}

func jevStub(t *testing.T, status int, body string, seen *string) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if seen != nil {
			*seen = r.Header.Get("Authorization")
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	t.Setenv(judgeEndpointEnvar, server.URL)
}

func TestLoginVerbRefusesWhatItCannotRun(t *testing.T) {
	cases := [][]string{{}, {"gemini"}, {"anthropic", "--browser"}, {"openrouter", "a-key-on-the-command-line"}}
	for _, args := range cases {
		_, errOut, code := runLogin(t, args...)
		if code != exitUsage {
			t.Errorf("loginVerb(%v) = %d, want %d", args, code, exitUsage)
		}
		if !strings.HasPrefix(errOut, "boji login:") {
			t.Errorf("loginVerb(%v) said %q", args, errOut)
		}
	}
}

func TestLoginStatusReportsNoneAndThenTheStoredCredential(t *testing.T) {
	emptyHome(t)

	out, _, code := runLogin(t, "--status")
	if code != exitOK || !strings.HasPrefix(out, "credentials: none\n") {
		t.Fatalf("status without a credential = %q, code %d", out, code)
	}
	if !strings.Contains(out, "boji login openrouter") {
		t.Fatalf("status without a key does not name the command: %q", out)
	}

	path, err := cred.Path()
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	store, err := cred.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	authorized := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	err = store.Save(cred.Credential{
		Provider:   cred.Anthropic,
		Kind:       cred.KindOAuth,
		Access:     testAccess,
		Refresh:    "refresh-not-a-real-token",
		Expires:    authorized.Add(8 * time.Hour),
		Identity:   cred.Identity{AccountID: "account-uuid-1", Email: testEmail},
		Authorized: authorized,
	}, authorized)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	out, _, code = runLogin(t, "--status")
	if code != exitOK {
		t.Fatalf("status = %d", code)
	}
	for _, want := range []string{"anthropic", "oauth", "expires 2026-09-18T20:00:00Z", "re-login by 2026-10-18"} {
		if !strings.Contains(out, want) {
			t.Errorf("status %q is missing %q", out, want)
		}
	}
	for _, secret := range []string{testAccess, "refresh-not-a-real-token", testEmail, "account-uuid-1"} {
		if strings.Contains(out, secret) {
			t.Fatalf("status leaked %q", secret)
		}
	}
}

func TestLoginOpenRouterStoresAKeyThatReachedJev(t *testing.T) {
	emptyHome(t)
	var sent string
	jevStub(t, http.StatusOK, jevNoulAnswer, &sent)

	var out, errOut bytes.Buffer
	code := loginVerb([]string{"openrouter"}, strings.NewReader(testKey+"\n"), &out, &errOut)
	if code != exitOK {
		t.Fatalf("login openrouter = %d, stderr %q", code, errOut.String())
	}
	if sent != "Bearer "+testKey {
		t.Fatalf("the check sent %q", sent)
	}
	if !strings.Contains(out.String(), "jev: ready") {
		t.Fatalf("login said %q", out.String())
	}
	if strings.Contains(out.String(), testKey) || strings.Contains(errOut.String(), testKey) {
		t.Fatal("login echoed the key")
	}

	path, err := cred.OpenRouterPath()
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(raw) != "OPENROUTER_KEY="+testKey+"\n" {
		t.Fatalf("stored file is %q", string(raw))
	}
}

func TestLoginOpenRouterWritesNothingWhenTheKeyCannotReachJev(t *testing.T) {
	emptyHome(t)
	jevStub(t, http.StatusUnauthorized, jevRefusesAKey, nil)

	var out, errOut bytes.Buffer
	code := loginVerb([]string{"openrouter"}, strings.NewReader("a-key-that-is-refused\n"), &out, &errOut)
	if code != exitVerdict {
		t.Fatalf("login openrouter = %d, want %d", code, exitVerdict)
	}
	if !strings.Contains(errOut.String(), "No auth credentials found") {
		t.Fatalf("the refusal does not name what the endpoint said: %q", errOut.String())
	}
	path, err := cred.OpenRouterPath()
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("a refused key wrote %s", path)
	}
}

func TestAProjectEnvWinsOverTheStoredKey(t *testing.T) {
	emptyHome(t)
	stored, err := cred.OpenRouterPath()
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	if err := cred.SaveOpenRouter(stored, "stored-key"); err != nil {
		t.Fatalf("save: %v", err)
	}
	project := t.TempDir()
	projectEnv := filepath.Join(project, ".env")
	if err := os.WriteFile(projectEnv, []byte("OPENROUTER_KEY=project-key\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	t.Chdir(project)

	key, err := gateKey()
	if err != nil || key != "project-key" {
		t.Fatalf("gateKey with a project .env = %q, %v", key, err)
	}
	if err := os.Remove(projectEnv); err != nil {
		t.Fatalf("remove: %v", err)
	}
	key, err = gateKey()
	if err != nil || key != "stored-key" {
		t.Fatalf("gateKey with only the stored key = %q, %v", key, err)
	}
}
