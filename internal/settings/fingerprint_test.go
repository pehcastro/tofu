package settings_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"tofu/internal/llm"
	"tofu/internal/llm/quota"
	"tofu/internal/llm/wire/anthropic"
	"tofu/internal/llm/wire/codex"
	"tofu/internal/settings"
	"tofu/internal/transport"
)

const versionTooOldBody = `{"type":"error","error":{"type":"invalid_request_error","message":"Claude Code %s does not support this model; version %s or newer is required. Update Claude Code.","details":{"error_code":"claude_code_version_too_old"}}}`

type sent struct{ agent, billed string }

type versionServer struct {
	mu       sync.Mutex
	requests []sent
	accepts  string
	requires string
}

func pinPlus(patches int) string {
	cut := strings.LastIndex(anthropic.PinnedClaudeCodeVersion, ".") + 1
	patch, _ := strconv.Atoi(anthropic.PinnedClaudeCodeVersion[cut:])
	return anthropic.PinnedClaudeCodeVersion[:cut] + strconv.Itoa(patch+patches)
}

func (s *versionServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	agent := r.Header.Get("User-Agent")
	billed := regexp.MustCompile(`cc_version=(\d+\.\d+\.\d+)\.`).FindStringSubmatch(string(body))
	s.mu.Lock()
	s.requests = append(s.requests, sent{agent: agent, billed: billed[1]})
	s.mu.Unlock()
	if !strings.Contains(agent, "claude-cli/"+s.accepts+" ") {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprintf(w, versionTooOldBody, billed[1], s.requires)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	for _, event := range []string{
		`{"type":"message_start","message":{"id":"msg_1","model":"claude-test","usage":{"input_tokens":3}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"done"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`,
		`{"type":"message_stop"}`,
	} {
		_, _ = fmt.Fprintf(w, "data: %s\n\n", event)
	}
}

func (s *versionServer) sentVersions() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var versions []string
	for _, request := range s.requests {
		versions = append(versions, request.agent+" billed "+request.billed)
	}
	return versions
}

func claimed(version string) string {
	return anthropic.ClaudeCodeUserAgent(version) + " billed " + version
}

func homeWith(t *testing.T, global, project string) *settings.Store {
	t.Helper()
	dir := t.TempDir()
	globalPath, projectPath := filepath.Join(dir, "home", "settings.json"), filepath.Join(dir, "project", "settings.json")
	for path, content := range map[string]string{globalPath: global, projectPath: project} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if content == "" {
			continue
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	store, err := settings.Open(globalPath, projectPath)
	if err != nil {
		t.Fatalf("opening the settings: %v", err)
	}
	return store
}

func askThrough(t *testing.T, store *settings.Store, accepts string) (*versionServer, *anthropic.Wire) {
	t.Helper()
	server := &versionServer{accepts: accepts, requires: pinPlus(16)}
	listening := httptest.NewServer(server)
	t.Cleanup(listening.Close)
	wire, err := anthropic.New(anthropic.Config{
		BaseURL:           listening.URL,
		Model:             "claude-test",
		Proxy:             true,
		Token:             func(context.Context, string) (string, error) { return "sk-ant-oat01-made-up", nil },
		ClaudeCodeVersion: store.Fingerprint().ClaudeCode,
		AdoptVersion: func(version string) error {
			return store.Save(settings.Global, settings.Write{Kind: settings.WriteFingerprint, Key: settings.FingerprintClaudeCode, Value: version})
		},
	})
	if err != nil {
		t.Fatalf("opening the wire: %v", err)
	}
	return server, wire
}

func ask(wire *anthropic.Wire) error {
	_, _, err := wire.Ask(context.Background(), anthropic.Request{Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}}})
	return err
}

func TestAVersionTooOldRejectionRaisesTheGlobalFileAndRetriesOnceAtTheRequiredVersion(t *testing.T) {
	store := homeWith(t, `{"theme":"light"}`, "")
	required := pinPlus(16)
	server, wire := askThrough(t, store, required)
	if err := ask(wire); err != nil {
		t.Fatalf("the retry at the required version failed: %v", err)
	}
	if got, want := server.sentVersions(), []string{claimed(anthropic.PinnedClaudeCodeVersion), claimed(required)}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("the requests claimed %q, not %q", got, want)
	}
	written, _ := os.ReadFile(store.Path(settings.Global))
	if !strings.Contains(string(written), `"claudeCode": "`+required+`"`) || !strings.Contains(string(written), `"theme": "light"`) {
		t.Fatalf("the global file does not hold the adopted version beside what it had:\n%s", written)
	}
	if err := ask(wire); err != nil {
		t.Fatalf("a second ask on the same wire failed: %v", err)
	}
	if got := server.sentVersions(); len(got) != 3 || got[2] != claimed(required) {
		t.Fatalf("the next ask on the same wire claimed %q rather than going straight to %s", got, required)
	}
	reopened, err := settings.Open(store.Path(settings.Global), store.Path(settings.Project))
	if err != nil || reopened.Fingerprint().ClaudeCode != required {
		t.Fatalf("a fresh read of the global file resolves %q (%v), not %s", reopened.Fingerprint().ClaudeCode, err, required)
	}
}

func TestARejectionAtEveryVersionSendsExactlyTwoRequestsAndReturnsTheError(t *testing.T) {
	server, wire := askThrough(t, homeWith(t, "", ""), "none")
	err := ask(wire)
	if err == nil || !strings.Contains(err.Error(), anthropic.VersionTooOldCode) {
		t.Fatalf("the rejection did not come back as the error: %v", err)
	}
	if got, want := server.sentVersions(), []string{claimed(anthropic.PinnedClaudeCodeVersion), claimed(server.requires)}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("the requests claimed %q, not %q", got, want)
	}
}

func TestTheSentVersionIsTheHigherOfThePinAndTheGlobalFile(t *testing.T) {
	pin := anthropic.PinnedClaudeCodeVersion
	cut := strings.LastIndex(pin, ".") + 1
	olderButLexicallyHigher := pin[:cut] + strings.Repeat("9", len(pin)-cut-1)
	for declared, want := range map[string]string{
		`{"subFingerprint":{"claudeCode":"` + pinPlus(6) + `"}}`:              pinPlus(6),
		`{"subFingerprint":{"claudeCode":"` + pinPlus(-84) + `"}}`:            pin,
		`{"subFingerprint":{"claudeCode":"banana"}}`:                          pin,
		`{"subFingerprint":{"claudeCode":7}}`:                                 pin,
		`{"subFingerprint":{"claudeCode":"` + olderButLexicallyHigher + `"}}`: pin,
	} {
		server, wire := askThrough(t, homeWith(t, declared, ""), want)
		if err := ask(wire); err != nil {
			t.Fatalf("with %s the ask failed: %v", declared, err)
		}
		if got := server.sentVersions(); len(got) != 1 || got[0] != claimed(want) {
			t.Errorf("with %s the requests claimed %q, not %s", declared, got, want)
		}
	}
}

type madeUpToken struct{}

func (madeUpToken) Access(context.Context) (string, error) { return "sk-ant-oat01-made-up", nil }

func TestTheUsagePollClaimsTheRaisedGlobalVersion(t *testing.T) {
	raised := pinPlus(715)
	store := homeWith(t, `{"subFingerprint":{"claudeCode":"`+raised+`"}}`, "")
	var agent string
	listening := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agent = r.Header.Get("User-Agent")
		_, _ = io.WriteString(w, `{}`)
	}))
	t.Cleanup(listening.Close)
	poller, err := quota.NewPoller(nil, time.Now, map[quota.Provider]string{quota.ClaudeSub: listening.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = poller.Poll(context.Background(), quota.Account{Provider: quota.ClaudeSub, Credential: madeUpToken{}, ClientVersion: store.Fingerprint().ClaudeCode})
	if agent != anthropic.ClaudeCodeUserAgent(raised) {
		t.Fatalf("the usage poll claimed %q, not %s", agent, raised)
	}
}

func TestAProjectScopeSubFingerprintIsNotRead(t *testing.T) {
	pin := anthropic.PinnedClaudeCodeVersion
	server, wire := askThrough(t, homeWith(t, "", `{"subFingerprint":{"claudeCode":"`+pinPlus(715)+`"}}`), pin)
	if err := ask(wire); err != nil {
		t.Fatalf("the ask failed: %v", err)
	}
	if got := server.sentVersions(); len(got) != 1 || got[0] != claimed(pin) {
		t.Fatalf("a project file raised the version: the requests claimed %q", got)
	}
}

func npmServing(t *testing.T, claudeCode string) string {
	t.Helper()
	latest := map[string]string{
		"/" + anthropic.ClaudeCodePackage + "/latest": claudeCode,
		"/" + codex.CodexPackage + "/latest":          codex.PinnedCodexClientVersion,
	}
	listening := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		version, known := latest[r.URL.Path]
		if !known {
			http.NotFound(w, r)
			return
		}
		_, _ = fmt.Fprintf(w, `{"name":"made-up","version":%q}`, version)
	}))
	t.Cleanup(listening.Close)
	return listening.URL
}

func latestFrom(t *testing.T, registry string) (settings.Fingerprint, error) {
	t.Helper()
	client, err := transport.New(transport.Config{AttemptTimeout: time.Second, Concurrency: 1})
	if err != nil {
		t.Fatal(err)
	}
	return settings.LatestFingerprint(context.Background(), client, registry)
}

func TestANewerNpmReleaseRaisesTheGlobalFileAndAnyOtherLeavesItAsItIs(t *testing.T) {
	pin := anthropic.PinnedClaudeCodeVersion
	for _, row := range []struct{ held, npm, raised string }{
		{pin, pinPlus(16), pinPlus(16)},
		{pin, pinPlus(-84), ""},
		{pin, pin, ""},
		{pinPlus(6), pinPlus(1), ""},
		{pin, pinPlus(16) + "-beta.1", ""},
		{pin, "latest", ""},
	} {
		held := `{"theme":"light","subFingerprint":{"claudeCode":"` + row.held + `"}}`
		store := homeWith(t, held, "")
		latest, err := latestFrom(t, npmServing(t, row.npm))
		if err != nil {
			t.Fatalf("npm at %s: the fetch failed: %v", row.npm, err)
		}
		raised, err := store.RaiseFingerprint(latest)
		if err != nil {
			t.Fatalf("npm at %s over %s: the raise failed: %v", row.npm, row.held, err)
		}
		written, _ := os.ReadFile(store.Path(settings.Global))
		if row.raised == "" {
			if len(raised) != 0 || string(written) != held {
				t.Errorf("npm at %s over %s raised %v and the file became %s", row.npm, row.held, raised, written)
			}
			continue
		}
		want := []settings.RaisedVersion{{Key: settings.FingerprintClaudeCode, From: row.held, To: row.raised}}
		if fmt.Sprint(raised) != fmt.Sprint(want) || !strings.Contains(string(written), `"claudeCode": "`+row.raised+`"`) || !strings.Contains(string(written), `"theme": "light"`) {
			t.Errorf("npm at %s over %s raised %v, not %v, and the file became\n%s", row.npm, row.held, raised, want, written)
		}
	}
}

func TestARegistryThatIsDownOrMissingAPackageGivesNoVersions(t *testing.T) {
	listening := httptest.NewServer(http.NotFoundHandler())
	down := listening.URL
	listening.Close()
	missing := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(missing.Close)
	for _, registry := range []string{down, missing.URL} {
		if latest, err := latestFrom(t, registry); err == nil {
			t.Errorf("a registry at %s gave %+v rather than an error", registry, latest)
		}
	}
}
