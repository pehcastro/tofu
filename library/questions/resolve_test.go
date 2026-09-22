package questions_test

import (
	"strings"
	"testing"

	"tofu/internal/judge/question"
	"tofu/library/questions"
)

func TestBareToolGateIsRefusedWithTwoVersionsOnDisk(t *testing.T) {
	layers, err := question.DefaultLayers(questions.Files())
	if err != nil {
		t.Fatalf("layers: %v", err)
	}
	_, _, err = question.Resolve("tool_gate", layers)
	if err == nil {
		t.Fatal("resolve accepted a bare name with two versions on disk")
	}
	if !strings.Contains(err.Error(), "tool_gate@1") || !strings.Contains(err.Error(), "tool_gate@2") {
		t.Fatalf("error does not name both versions: %v", err)
	}
}

func TestExactToolGateVersionResolvesWithTwoVersionsOnDisk(t *testing.T) {
	layers, err := question.DefaultLayers(questions.Files())
	if err != nil {
		t.Fatalf("layers: %v", err)
	}
	set, _, err := question.Resolve("tool_gate@1", layers)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if set.Version != 1 {
		t.Fatalf("tool_gate@1 resolved to version %d, want 1", set.Version)
	}
}
