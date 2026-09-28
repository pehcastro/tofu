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

const (
	madeUpOpenRouterKey = "or-v1-made-up-5b7e21c9d0a4"
	madeUpTypeSafeKey   = "ts-made-up-8f3c60a2e1b7"
)

type heardRequest struct {
	authorization string
	body          string
}

func TestATypeSafeBindingOpensTheTypeSafeWireWithTheTypeSafeKey(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv(sys.OpenRouterKeyName, "")
	t.Setenv(sys.TypeSafeKeyName, "")
	t.Chdir(project)
	heard := make(chan heardRequest, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		heard <- heardRequest{authorization: r.Header.Get("Authorization"), body: string(body)}
		w.WriteHeader(http.StatusBadRequest)
	}))
	t.Cleanup(server.Close)
	t.Setenv(judgeEndpointEnvar, server.URL)
	for name, value := range map[string]string{sys.OpenRouterKeyName: madeUpOpenRouterKey, sys.TypeSafeKeyName: madeUpTypeSafeKey} {
		if err := sys.SaveKey(name, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := models.BindRole(sys.StateDir(project), models.RoleClassifier, "typesafe/jev-latest"); err != nil {
		t.Fatal(err)
	}

	client, err := newJevClient(oneCallAtATime)
	if err != nil {
		t.Fatalf("newJevClient: %v", err)
	}
	if name := client.Caps().Name; name != "typesafe" {
		t.Errorf("the bound typesafe/jev-latest opened the %s wire", name)
	}
	_, _ = client.Ask(context.Background(), jev.Request{
		State:     map[string]string{"check": "which wire"},
		Questions: []jev.Question{{ID: "reachable", Kind: jev.QuestionNoul, Instructions: "?", True: "t", False: "f"}},
	})
	request := <-heard
	if request.authorization != "Bearer "+madeUpTypeSafeKey {
		t.Errorf("the request carried an authorization of length %d, and the typesafe key is %d", len(request.authorization), len("Bearer "+madeUpTypeSafeKey))
	}
	if !strings.Contains(request.body, `"model":"jev-latest"`) {
		t.Errorf("the request named another model: %s", request.body)
	}

	set := battery{SetName: "stop_check", QuestionsVersion: 1, Kinds: map[string]question.Kind{"work_remains": question.KindNoul}}
	row, err := appendRow(map[string]string{"task": "write the note"}, set, rowInput{})
	if err != nil {
		t.Fatal(err)
	}
	if row.Model != "typesafe/jev-latest" {
		t.Errorf("the ledger row records %q, want the bound typesafe/jev-latest", row.Model)
	}
}
