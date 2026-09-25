package subagent

import (
	"errors"
	"strings"
	"testing"
)

func TestBoundaryCommandOwnsItsDirectoryToRead(t *testing.T) {
	for _, driven := range []struct {
		owns    []string
		cmd     string
		refused bool
		reason  string
	}{
		{[]string{"internal/subagent/command.go"}, "go test ./internal/subagent/...", false, ""},
		{[]string{"internal/recall/**"}, "go test ./internal/recall/...", false, ""},
		{[]string{"internal/subagent/command.go"}, "go test ./...", true, "is a tree-wide command"},
		{[]string{"internal/recall/**"}, "go test ./...", true, "is a tree-wide command"},
		{[]string{"internal/subagent/command.go"}, "go test ./internal/...", true, "is outside the paths this agent holds"},
		{[]string{"internal/subagent/command.go"}, "go test ./internal/recall/...", true, "is outside the paths this agent holds"},
	} {
		err := (&Boundary{Ticket: "TOFU-690-driven", Owns: driven.owns}).Command(driven.cmd)
		t.Logf("owns=%v cmd=%q -> %v", driven.owns, driven.cmd, err)
		switch {
		case driven.refused && err == nil:
			t.Errorf("owns=%v cmd=%q: want refused, got allowed", driven.owns, driven.cmd)
		case driven.refused && !strings.Contains(err.Error(), driven.reason):
			t.Errorf("owns=%v cmd=%q: refused for the wrong reason: %v", driven.owns, driven.cmd, err)
		case !driven.refused && err != nil:
			t.Errorf("owns=%v cmd=%q: want allowed, got %v", driven.owns, driven.cmd, err)
		}
	}
}

func TestBoundaryWriteStillRefusesAFileItDoesNotOwn(t *testing.T) {
	boundary := &Boundary{Ticket: "TOFU-690-driven", Owns: []string{"internal/subagent/command.go"}}
	err := boundary.Write("internal/subagent/owns.go")
	t.Logf("owns=%v write=%q -> %v", boundary.Owns, "internal/subagent/owns.go", err)
	var denied DeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("write of an unowned file: want DeniedError, got %v", err)
	}
}
