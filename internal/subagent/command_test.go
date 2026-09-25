package subagent

import (
	"strings"
	"testing"
)

func TestBoundaryCommandDriven(t *testing.T) {
	for _, driven := range []struct {
		owns    []string
		cmd     string
		refused bool
	}{
		{[]string{"internal/subagent/command.go"}, "go test ./...", true},
		{[]string{"internal/subagent/command.go"}, "go vet ./internal/subagent/command.go", false},
		{[]string{"internal/recall/**"}, "go test ./internal/recall/...", false},
		{[]string{"cmd/tofu/**"}, "go test ./cmd/tofu/", false},
		{[]string{"internal/subagent/command.go"}, "gofmt -l .", true},
		{[]string{"internal/subagent/command.go"}, "golangci-lint run", true},
		{[]string{"internal/subagent/command.go"}, "cd internal/subagent && gofmt -l .", true},
	} {
		err := (&Boundary{Ticket: "TOFU-682-driven", Owns: driven.owns}).Command(driven.cmd)
		t.Logf("owns=%v cmd=%q -> %v", driven.owns, driven.cmd, err)
		switch {
		case driven.refused && err == nil:
			t.Errorf("owns=%v cmd=%q: want refused, got allowed", driven.owns, driven.cmd)
		case driven.refused && !strings.Contains(err.Error(), "is a tree-wide command"):
			t.Errorf("owns=%v cmd=%q: refused for the wrong reason: %v", driven.owns, driven.cmd, err)
		case !driven.refused && err != nil:
			t.Errorf("owns=%v cmd=%q: want allowed, got %v", driven.owns, driven.cmd, err)
		}
	}
}
