package boardy

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t", "-c", "core.autocrlf=false"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return string(out)
}

func TestEveryMoveToDoneNeedsTheFlowsStamp(t *testing.T) {
	t.Setenv("TOFU_STAMP_DIR", "")
	repo := t.TempDir()
	gitIn(t, repo, "init", "-q")
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte(".local/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "add", "-A")
	gitIn(t, repo, "commit", "-q", "-m", "work")
	m := managedBoard(t, "DEMO")
	m.Store.Root = repo
	turn, err := m.Create("DEMO", ticketWith("turn", "internal/turn/**"), manager)
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.Move(turn.ID, Done, "", person)
	refused(t, err, "go run ./bench/flows")
	if _, err := m.Check("move", turn.ID, Done, person); err == nil {
		t.Fatal("check allowed a move to done that the move refuses")
	}
	library, err := m.Create("DEMO", ticketWith("library", "library/**"), manager)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Move(library.ID, Done, "", person); err != nil {
		t.Fatalf("a ticket outside the gated areas was refused: %v", err)
	}
	unrooted := managedBoard(t, "DEMO")
	desk, err := unrooted.Create("DEMO", ticketWith("desk", "cmd/tofu/main.go"), manager)
	if err != nil {
		t.Fatal(err)
	}
	_, err = unrooted.Move(desk.ID, Done, "", person)
	refused(t, err, "repository")
	tree := gitIn(t, repo, "rev-parse", "HEAD^{tree}")[:40]
	stamp := filepath.Join(repo, ".local", "flows", tree)
	if err := os.MkdirAll(filepath.Dir(stamp), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stamp, []byte("tree "+tree+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if moved, err := m.Move(turn.ID, Done, "", person); err != nil || moved.Status != Done {
		t.Fatalf("a stamped tree was refused: %v", err)
	}
}
