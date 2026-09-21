package main

import (
	"strings"
	"testing"
)

func TestTheKeyWireCannotSendAPromptOnAJevProvider(t *testing.T) {
	t.Setenv("OPENROUTER_KEY", "a-secret-that-is-not-real")
	_, err := keyModel(openRouterDefaultModel)
	if err == nil {
		t.Fatal("the openrouter key built a turn model, and it is registered to buy typed decisions only")
	}
	if !strings.Contains(err.Error(), "jev-provider") || !strings.Contains(err.Error(), "may not send a prompt") {
		t.Fatalf("the refusal reads %q and must name the role", err)
	}
	t.Logf("tofu run --wire openrouter now fails with: %v", err)
}
