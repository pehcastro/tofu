package models

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"tofu/internal/sys"
)

const madeUpMetaKey = "meta-made-up-9b3e71c0"

func refusedBy(t *testing.T, handler http.HandlerFunc) *KeyRefused {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv(sys.MetaMuseKeyName, "")
	server := httptest.NewServer(handler)
	if handler == nil {
		server.Close()
	} else {
		t.Cleanup(server.Close)
	}
	t.Setenv(MetaBaseURLVariable, server.URL)
	err := StoreMetaKey(context.Background(), madeUpMetaKey)
	var refused *KeyRefused
	if !errors.As(err, &refused) {
		t.Fatalf("the check returned %T %v, want a *KeyRefused", err, err)
	}
	if stored, _ := sys.StoredKeys(); stored[sys.MetaMuseKeyName] != "" {
		t.Fatal("a refused key was written")
	}
	if strings.Contains(refused.Error(), madeUpMetaKey) || strings.Contains(refused.Brief(), madeUpMetaKey) {
		t.Fatalf("the refusal carries the key: %q", refused.Error())
	}
	return refused
}

func TestARefusedMetaKeyNamesMetaAndTheStatusInOneLine(t *testing.T) {
	refused := refusedBy(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad key "+strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), http.StatusUnauthorized)
	})
	if refused.Provider != Meta || refused.Status != http.StatusUnauthorized {
		t.Fatalf("the refusal says %s answered %d", refused.Provider, refused.Status)
	}
	if got, want := refused.Brief(), "Meta refused the key (401)"; got != want {
		t.Fatalf("the short line is %q, want %q", got, want)
	}
}

func TestAMetaKeyThatReachesNobodyHasNoStatus(t *testing.T) {
	refused := refusedBy(t, nil)
	if refused.Status != 0 {
		t.Fatalf("a closed server answered %d", refused.Status)
	}
	if got, want := refused.Brief(), "could not reach Meta"; got != want {
		t.Fatalf("the short line is %q, want %q", got, want)
	}
}

func TestEveryKeyProviderHasADisplayName(t *testing.T) {
	for provider, want := range map[Provider]string{OpenRouter: "OpenRouter", TypeSafe: "TypeSafe", Meta: "Meta", Anthropic: "Anthropic", OpenAI: "OpenAI"} {
		if got := provider.Display(); got != want {
			t.Errorf("%s displays as %q, want %q", provider, got, want)
		}
	}
}
