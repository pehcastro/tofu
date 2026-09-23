package subagent

import (
	"strings"
	"testing"
)

func TestBoundaryCommandRefusesEveryWayAShellReachesOutsideTheHeldPaths(t *testing.T) {
	owns := []string{"internal/subagent/**"}
	refused := []struct {
		command string
		names   string
	}{
		{"rm -rf internal/judge", "internal/judge"},
		{"rm ../secrets.txt", "../secrets.txt"},
		{"gofmt -w /etc/passwd", "/etc/passwd"},
		{"echo stolen > notes.txt", "notes.txt"},
		{"echo stolen >> ../notes.txt", "../notes.txt"},
		{"cat internal/subagent/owns.go > internal/turn/spawn.go", "internal/turn/spawn.go"},
		{"gofmt -w \"internal/turn/spawn.go\"", "internal/turn/spawn.go"},
		{"cp internal/subagent/owns.go $HOME/owns.go", "$HOME/owns.go"},
		{"cd internal/subagent && rm ../../go.mod", "../../go.mod"},
		{"cd internal/subagent && rm -rf ../judge", "../judge"},
	}
	for _, refusal := range refused {
		t.Run(refusal.command, func(t *testing.T) {
			err := (&Boundary{Owns: owns}).Command(refusal.command)
			if err == nil {
				t.Fatalf("the command was allowed to reach outside %v", owns)
			}
			if !strings.Contains(err.Error(), refusal.names) {
				t.Fatalf("the refusal does not name %q: %v", refusal.names, err)
			}
		})
	}
}

func TestBoundaryCommandLetsACommandInsideTheHeldPathsThrough(t *testing.T) {
	owns := []string{"internal/subagent/**", "internal/konst/konst.go"}
	allowed := []string{
		"go test ./internal/subagent/...",
		"gofmt -l -w internal/subagent/owns.go internal/konst/konst.go",
		"grep -n \"Allow\" internal/subagent/owns.go | head -3",
		"echo package subagent > internal/subagent/command.go",
		"echo hello",
		"go version",
	}
	for _, command := range allowed {
		t.Run(command, func(t *testing.T) {
			if err := (&Boundary{Owns: owns}).Command(command); err != nil {
				t.Fatalf("a command inside the held paths was refused: %v", err)
			}
		})
	}
}
