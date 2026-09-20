package crew

import (
	"strings"
	"testing"
)

func TestBoundaryCommandRefusesEveryWayAShellReachesOutsideTheHeldPaths(t *testing.T) {
	owns := []string{"internal/crew/**"}
	refused := []struct {
		command string
		names   string
	}{
		{"rm -rf internal/judge", "internal/judge"},
		{"rm ../secrets.txt", "../secrets.txt"},
		{"gofmt -w /etc/passwd", "/etc/passwd"},
		{"echo stolen > notes.txt", "notes.txt"},
		{"echo stolen >> ../notes.txt", "../notes.txt"},
		{"cat internal/crew/owns.go > internal/turn/spawn.go", "internal/turn/spawn.go"},
		{"gofmt -w \"internal/turn/spawn.go\"", "internal/turn/spawn.go"},
		{"cp internal/crew/owns.go $HOME/owns.go", "$HOME/owns.go"},
		{"cd internal/crew && rm ../../go.mod", "../../go.mod"},
		{"cd internal/crew && rm -rf ../judge", "../judge"},
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
	owns := []string{"internal/crew/**", "internal/konst/konst.go"}
	allowed := []string{
		"go test ./internal/crew/...",
		"gofmt -l -w internal/crew/owns.go internal/konst/konst.go",
		"grep -n \"Allow\" internal/crew/owns.go | head -3",
		"echo package crew > internal/crew/command.go",
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
