package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"tofu/internal/judge/jev"
	"tofu/internal/judge/question"
	"tofu/internal/llm/models"
	"tofu/internal/sys"
)

type heardRequest struct {
	authorization string
	body          string
}

func TestEachBindingOpensItsOwnWireWithItsOwnClassifierKey(t *testing.T) {
	keys := map[string]string{
		sys.OpenRouterKeyName:           "or-v1-made-up-llm-5b7e21c9d0a4",
		sys.OpenRouterClassifierKeyName: "or-v1-made-up-classifier-71c0e3",
		sys.TypeSafeKeyName:             "ts-made-up-8f3c60a2e1b7",
	}
	for _, bound := range []struct{ slug, wire, key, model string }{
		{"typesafe/jev-latest", "typesafe", sys.TypeSafeKeyName, `"model":"jev-latest"`},
		{"openrouter/jev-latest", "openrouter", sys.OpenRouterClassifierKeyName, `"model":"~typesafe/jev-latest"`},
	} {
		t.Run(bound.wire, func(t *testing.T) {
			home, project := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			t.Chdir(project)
			heard := make(chan heardRequest, 8)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				heard <- heardRequest{authorization: r.Header.Get("Authorization"), body: string(body)}
				w.WriteHeader(http.StatusBadRequest)
			}))
			t.Cleanup(server.Close)
			t.Setenv(judgeEndpointEnvar, server.URL)
			for name, value := range keys {
				t.Setenv(name, "")
				if err := sys.SaveKey(name, value); err != nil {
					t.Fatal(err)
				}
			}
			if err := models.BindRole(sys.StateDir(project), models.RoleClassifier, bound.slug); err != nil {
				t.Fatal(err)
			}

			client, err := newJevClient()
			if err != nil {
				t.Fatalf("newJevClient: %v", err)
			}
			if name := client.Caps().Name; name != bound.wire {
				t.Errorf("the bound %s opened the %s wire", bound.slug, name)
			}
			_, _ = client.Ask(context.Background(), jev.Request{
				State:     map[string]string{"check": "which wire"},
				Questions: []jev.Question{{ID: "reachable", Kind: jev.QuestionNoul, Instructions: "?", True: "t", False: "f"}},
			})
			request := <-heard
			if request.authorization != "Bearer "+keys[bound.key] {
				t.Errorf("the request carried an authorization of length %d, and the %s key is %d", len(request.authorization), bound.key, len("Bearer "+keys[bound.key]))
			}
			if !strings.Contains(request.body, bound.model) {
				t.Errorf("the request named another model than %s: %s", bound.model, request.body)
			}

			set := battery{SetName: "stop_check", QuestionsVersion: 1, Kinds: map[string]question.Kind{"work_remains": question.KindNoul}}
			row, err := appendRow(map[string]string{"task": "write the note"}, set, rowInput{})
			if err != nil {
				t.Fatal(err)
			}
			if row.Model != bound.slug {
				t.Errorf("the ledger row records %q, want the bound %s", row.Model, bound.slug)
			}
		})
	}
}
