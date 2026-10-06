package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"tofu/internal/host"
	"tofu/internal/llm/models"
	"tofu/internal/sys"
	"tofu/internal/turn"
)

const (
	madeUpMetaKey = "meta-made-up-3c9e71b04d2f"
	metaNotice    = "Meta may train on what you send to this model"
	metaToolCall  = `data: {"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning"}}

data: {"type":"response.reasoning_summary_text.delta","output_index":0,"delta":"the note holds the answer"}

data: {"type":"response.output_item.done","output_index":0,"item":{"id":"rs_1","type":"reasoning","encrypted_content":"opaque"}}

data: {"type":"response.output_item.added","output_index":1,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"read"}}

data: {"type":"response.function_call_arguments.delta","output_index":1,"delta":"{\"path\":"}

data: {"type":"response.function_call_arguments.delta","output_index":1,"delta":"\"note.txt\"}"}

data: {"type":"response.output_item.done","output_index":1,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"read"}}

data: {"type":"response.completed","response":{"id":"resp_1","model":"muse-spark-1.3","status":"completed","usage":{"input_tokens":50,"output_tokens":9,"total_tokens":59}}}

`
	metaAnswer = `data: {"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"msg_1"}}

data: {"type":"response.output_text.delta","output_index":0,"delta":"the note says 42"}

data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"msg_1"}}

data: {"type":"response.completed","response":{"id":"resp_2","model":"muse-spark-1.3","status":"completed","usage":{"input_tokens":80,"output_tokens":5,"total_tokens":85}}}

`
)

type heardMeta struct {
	path, authorization, account, body string
}

func metaHome(t *testing.T) (string, string) {
	t.Helper()
	home, project := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv(sys.MetaMuseKeyName, "")
	t.Chdir(project)
	return home, project
}

func metaStub(t *testing.T, answer func(heardMeta, http.ResponseWriter)) *[]heardMeta {
	t.Helper()
	var mu sync.Mutex
	heard := &[]heardMeta{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		request := heardMeta{path: r.URL.Path, authorization: r.Header.Get("Authorization"), account: r.Header.Get("chatgpt-account-id"), body: string(body)}
		mu.Lock()
		*heard = append(*heard, request)
		mu.Unlock()
		if refusal := refusedAsMetaRefuses(body); refusal != "" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, refusal)
			return
		}
		answer(request, w)
	}))
	t.Cleanup(server.Close)
	t.Setenv(models.MetaBaseURLVariable, server.URL)
	return heard
}

func refusedAsMetaRefuses(body []byte) string {
	var request struct {
		Input []map[string]any `json:"input"`
		Tools []struct {
			Name       string `json:"name"`
			Parameters any    `json:"parameters"`
		} `json:"tools"`
	}
	if json.Unmarshal(body, &request) != nil {
		return ""
	}
	for index, item := range request.Input {
		if _, summarised := item["summary"]; item["type"] == "reasoning" && !summarised {
			return fmt.Sprintf(`{"error":{"message":"`+"`input[%d]`"+` missing required field `+"`summary`"+`","param":"input[%d]"}}`, index, index)
		}
	}
	var holdsNull func(any) bool
	holdsNull = func(value any) bool {
		switch typed := value.(type) {
		case nil:
			return true
		case map[string]any:
			for _, field := range typed {
				if holdsNull(field) {
					return true
				}
			}
		case []any:
			for _, item := range typed {
				if holdsNull(item) {
					return true
				}
			}
		}
		return false
	}
	for _, tool := range request.Tools {
		if holdsNull(tool.Parameters) {
			return `{"error":{"message":"Invalid JSON schema: null is not of type \"array\"","param":"parameters","tool":"` + tool.Name + `"}}`
		}
	}
	return ""
}

func TestAMetaTurnSendsTheKeyAndTheModelAndRunsTheStreamedToolCall(t *testing.T) {
	home, project := metaHome(t)
	if err := os.WriteFile(filepath.Join(project, "note.txt"), []byte("42\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := sys.SaveKey(sys.MetaMuseKeyName, madeUpMetaKey); err != nil {
		t.Fatal(err)
	}
	heard := metaStub(t, func(request heardMeta, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "text/event-stream")
		if strings.Contains(request.body, "function_call_output") {
			_, _ = io.WriteString(w, metaAnswer)
			return
		}
		_, _ = io.WriteString(w, metaToolCall)
	})

	var out, errOut bytes.Buffer
	code := runVerb([]string{"--dir", project, "--model", "meta/muse-spark-1.3", "--no-gate", "--sift", siftFree, "read note.txt"}, &out, &errOut)
	if code != exitOK {
		t.Fatalf("tofu run exited %d\n%s\n%s", code, out.String(), errOut.String())
	}
	if len(*heard) != 2 {
		t.Fatalf("the stub heard %d requests, want the tool call and the answer\n%s", len(*heard), out.String())
	}
	for i, request := range *heard {
		if request.path != "/responses" {
			t.Errorf("request %d went to %s", i, request.path)
		}
		if request.authorization != "Bearer "+madeUpMetaKey {
			t.Errorf("request %d carried an authorization of length %d, want the stored key as a Bearer", i, len(request.authorization))
		}
		if request.account != "" {
			t.Errorf("request %d carried a chatgpt account header", i)
		}
		if !strings.Contains(request.body, `"model":"muse-spark-1.3"`) {
			t.Errorf("request %d named another model: %.200s", i, request.body)
		}
	}
	if !strings.Contains((*heard)[1].body, `"call_id":"call_1"`) || !strings.Contains((*heard)[1].body, `42`) {
		t.Errorf("the second request does not carry the read result for call_1: %.400s", (*heard)[1].body)
	}
	if !strings.Contains(out.String(), "tool=read") || !strings.Contains(out.String(), "spend api key") {
		t.Errorf("the run does not show the streamed read call on a key spend:\n%s", out.String())
	}

	events := ""
	for _, root := range []string{home, project} {
		_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err == nil && entry.Name() == "events.jsonl" {
				body, _ := os.ReadFile(path)
				events += string(body)
			}
			return nil
		})
	}
	if !strings.Contains(events, `"wire":"meta"`) {
		t.Errorf("the recorded session does not name the meta wire:\n%.600s", events)
	}
	for name, text := range map[string]string{"stdout": out.String(), "stderr": errOut.String(), "events.jsonl": events} {
		if strings.Contains(text, madeUpMetaKey) {
			t.Errorf("%s carries the key", name)
		}
	}
	t.Log("\n" + out.String())
}

func TestABoundMetaOrchestratorRunsOnMetaWithNoModelAndTheAppListsItsWire(t *testing.T) {
	_, project := metaHome(t)
	if wires := keyWires(); len(wires) != 0 {
		t.Errorf("with no meta key the app lists %+v", wires)
	}
	if err := sys.SaveKey(sys.MetaMuseKeyName, madeUpMetaKey); err != nil {
		t.Fatal(err)
	}
	if err := models.BindRole(sys.StateDir(project), models.RoleOrchestrator, "meta/muse-spark-1.3-contributor"); err != nil {
		t.Fatal(err)
	}
	heard := metaStub(t, func(_ heardMeta, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, metaAnswer)
	})
	var out, errOut bytes.Buffer
	if code := runVerb([]string{"--dir", project, "--no-gate", "--sift", siftFree, "--tools", toolSetThree, "say ok"}, &out, &errOut); code != exitOK {
		t.Fatalf("tofu run with the orchestrator bound to meta exited %d\n%s", code, errOut.String())
	}
	if len(*heard) == 0 || !strings.Contains((*heard)[0].body, `"model":"muse-spark-1.3-contributor"`) {
		t.Errorf("the bound meta model was not what the run asked for: %d requests", len(*heard))
	}
	if opts := pickedOpts(project, host.Turn{Task: "say ok", Pick: host.Pick{Wire: wireSubscription}}, 1); opts.wire != wireMeta || opts.model != "meta/muse-spark-1.3-contributor" {
		t.Errorf("the app runs a bound meta orchestrator on wire %q model %q", opts.wire, opts.model)
	}
	wires := keyWires()
	if len(wires) != 1 || wires[0].Name != wireMeta || wires[0].Model != "muse-spark-1.3" {
		t.Errorf("with a meta key the app lists %+v, want the meta wire on muse-spark-1.3", wires)
	}
}

func TestAMetaTurnWithNoKeySaysHowToStoreOne(t *testing.T) {
	_, project := metaHome(t)
	heard := metaStub(t, func(heardMeta, http.ResponseWriter) {})
	var out, errOut bytes.Buffer
	code := runVerb([]string{"--dir", project, "--model", "meta/muse-spark-1.3", "--no-gate", "--sift", siftFree, "--tools", toolSetThree, "say ok"}, &out, &errOut)
	if code == exitOK || !strings.Contains(errOut.String(), "tofu login llm meta") || len(*heard) != 0 {
		t.Fatalf("with no key the run exited %d after %d requests and said:\n%s", code, len(*heard), errOut.String())
	}
}

func TestAMetaSlugPicksItsOwnWireAndRefusesAnother(t *testing.T) {
	opts, err := parseRunArgs([]string{"--dir", ".", "--model", "meta/muse-spark-1.3-contributor", "task"})
	if err != nil || opts.wire != wireMeta || wireSpend(opts.wire) != turn.SpendAPIKey {
		t.Fatalf("meta/muse-spark-1.3-contributor parsed to wire %q spend %q (%v)", opts.wire, wireSpend(opts.wire), err)
	}
	if bound, err := boundSubAgent(opts); bound != "" || err != nil {
		t.Errorf("an unnamed sub-agent under a meta orchestrator bound %q (%v)", bound, err)
	}
	if _, err := parseRunArgs([]string{"--dir", ".", "--wire", wireSubscription, "--model", "meta/muse-spark-1.3", "task"}); err == nil {
		t.Errorf("--wire anthropic with a meta model was taken")
	}
	library, err := modelLibrary("")
	if err != nil {
		t.Fatal(err)
	}
	model, err := library.Select("meta/muse-spark-1.2")
	if err != nil || library.WireOf(model) != wireMeta {
		t.Errorf("a sub-agent named meta/muse-spark-1.2 opens the %q wire (%v)", library.WireOf(model), err)
	}
}

func TestLoginMetaChecksTheKeyBeforeStoringItAndNeverPrintsIt(t *testing.T) {
	metaHome(t)
	status := http.StatusUnauthorized
	heard := metaStub(t, func(request heardMeta, w http.ResponseWriter) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, `{"error":"bad key `+strings.TrimPrefix(request.authorization, "Bearer ")+`"}`)
	})

	var out, errOut bytes.Buffer
	if code := loginVerb([]string{"llm", "meta"}, strings.NewReader(madeUpMetaKey+"\r\n"), &out, &errOut); code == exitOK {
		t.Fatalf("a refused key was stored:\n%s%s", out.String(), errOut.String())
	}
	stored, _ := sys.StoredKeys()
	if stored[sys.MetaMuseKeyName] != "" {
		t.Errorf("a refused key reached the store")
	}
	if strings.Contains(out.String()+errOut.String(), madeUpMetaKey) {
		t.Errorf("the refusal printed the key:\n%s", errOut.String())
	}

	status = http.StatusOK
	out.Reset()
	errOut.Reset()
	if code := loginVerb([]string{"llm", "meta"}, strings.NewReader(madeUpMetaKey+"\n"), &out, &errOut); code != exitOK {
		t.Fatalf("login meta exited %d:\n%s", code, errOut.String())
	}
	stored, _ = sys.StoredKeys()
	if stored[sys.MetaMuseKeyName] != madeUpMetaKey {
		t.Errorf("the store holds a value of length %d", len(stored[sys.MetaMuseKeyName]))
	}
	if strings.Contains(out.String()+errOut.String(), madeUpMetaKey) || !strings.Contains(out.String(), madeUpMetaKey[len(madeUpMetaKey)-4:]) {
		t.Errorf("login meta printed more than the last four, or not even those:\n%s", out.String())
	}
	last := (*heard)[len(*heard)-1]
	if last.path != "/models" || last.authorization != "Bearer "+madeUpMetaKey {
		t.Errorf("the check went to %s with an authorization of length %d", last.path, len(last.authorization))
	}
	t.Log("\n" + out.String())
}

func TestAMetaKeyOnlyInTheEnvironmentIsRedacted(t *testing.T) {
	metaHome(t)
	t.Setenv(sys.MetaMuseKeyName, madeUpMetaKey)
	said := sys.LoadKeyRedactor().Redact("env printed META_MUSE_API_KEY=" + madeUpMetaKey + " and " + madeUpMetaKey)
	if strings.Contains(said, madeUpMetaKey) {
		t.Fatalf("the redactor let the key through: %s", said)
	}
}

func TestModelsListsTheFiveMetaModelsWithTheirWindowAndTheContributorNotice(t *testing.T) {
	home, _ := metaHome(t)
	listed := func() map[string]modelReport {
		var out bytes.Buffer
		if code := modelsVerb([]string{jsonFlag}, &out, io.Discard); code != exitOK {
			t.Fatalf("tofu models --json exited %d", code)
		}
		var envelope struct {
			Data modelsReport `json:"data"`
		}
		if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		meta := map[string]modelReport{}
		for _, model := range envelope.Data.Models {
			if model.Provider == string(models.Meta) {
				meta[model.Slug] = model
			}
		}
		return meta
	}
	check := func(when string) {
		meta := listed()
		if len(meta) != 5 {
			t.Fatalf("%s: tofu models --json lists %d meta models", when, len(meta))
		}
		for slug, model := range meta {
			wantNotice := ""
			if strings.HasSuffix(slug, "-contributor") {
				wantNotice = metaNotice
			}
			if model.Notice != wantNotice || model.Pays != "key" || model.Use != "allowed" || model.ContextTokens != 1048576 {
				t.Errorf("%s: %s notice %q pays %s use %s window %d from %q", when, slug, model.Notice, model.Pays, model.Use, model.ContextTokens, model.WindowFrom)
			}
		}
	}
	check("with the shipped table")
	writeFile(t, home, ".tofu/model-windows.json", homeRegistryOfOneWindow)
	check("after a reload wrote a table with no meta row")
}
