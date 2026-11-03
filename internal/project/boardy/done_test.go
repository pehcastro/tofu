package boardy

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"tofu/internal/judge/ledger"
	"tofu/internal/models/classifiers/core"
	"tofu/internal/sys"
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
	jevSaysNothingOpen(t)
	repo := t.TempDir()
	gitIn(t, repo, "init", "-q")
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte(".local/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "add", "-A")
	gitIn(t, repo, "commit", "-q", "-m", "work")
	m := managedBoard(t, "DEMO")
	m.Store.Root = repo
	turn, err := m.Create("DEMO", ticketWith("turn", "internal/agent/loop/**"), manager)
	if err != nil {
		t.Fatal(err)
	}
	proved(t, m, turn.ID)
	_, err = m.Move(turn.ID, Done, closedReason, person)
	refused(t, err, "go run ./bench/flows")
	if _, err := m.Check("move", turn.ID, Done, person); err == nil {
		t.Fatal("check allowed a move to done that the move refuses")
	}
	for owns, says := range map[string]string{"apps/desk/**": "apps/desk tree could not be read", "internal/store/session/**": "go run ./bench/flows"} {
		gated, err := m.Create("DEMO", ticketWith(owns, owns), manager)
		if err != nil {
			t.Fatal(err)
		}
		proved(t, m, gated.ID)
		_, err = m.Move(gated.ID, Done, closedReason, person)
		refused(t, err, says)
	}
	library, err := m.Create("DEMO", ticketWith("library", "library/**"), manager)
	if err != nil {
		t.Fatal(err)
	}
	proved(t, m, library.ID)
	if _, err := m.Move(library.ID, Done, closedReason, person); err != nil {
		t.Fatalf("a ticket outside the gated areas was refused: %v", err)
	}
	unrooted := managedBoard(t, "DEMO")
	desk, err := unrooted.Create("DEMO", ticketWith("desk", "apps/cli/main.go"), manager)
	if err != nil {
		t.Fatal(err)
	}
	_, err = unrooted.Move(desk.ID, Done, closedReason, person)
	refused(t, err, "repository")
	tree := gitIn(t, repo, "rev-parse", "HEAD^{tree}")[:40]
	stamp := filepath.Join(repo, ".local", "flows", tree)
	if err := os.MkdirAll(filepath.Dir(stamp), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stamp, []byte("tree "+tree+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if moved, err := m.Move(turn.ID, Done, closedReason, person); err != nil || moved.Status != Done {
		t.Fatalf("a stamped tree was refused: %v", err)
	}
}

func TestADeskTicketNeedsTheDeskStampOfItsTree(t *testing.T) {
	t.Setenv("TOFU_STAMP_DIR", "")
	jevSaysNothingOpen(t)
	repo := t.TempDir()
	gitIn(t, repo, "init", "-q")
	commit := func(path, body string) {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(repo, path)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repo, path), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		gitIn(t, repo, "add", "-A")
		gitIn(t, repo, "commit", "-q", "-m", path)
	}
	stampAs := func(kind, tree string) {
		path := filepath.Join(repo, ".local", kind, tree)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("tree "+tree+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	deskTree := func() string { return strings.TrimSpace(gitIn(t, repo, "rev-parse", "HEAD:apps/desk")) }
	commit(".gitignore", ".local/\n")
	commit("apps/desk/src/main.rs", "fn main() {}\n")
	goTree, _, err := HeadTree(repo)
	if err != nil {
		t.Fatal(err)
	}
	stampAs(FlowsStamps, goTree)
	m := managedBoard(t, "DEMO")
	m.Store.Root = repo
	desk, err := m.Create("DEMO", ticketWith("desk", "apps/desk/**"), manager)
	if err != nil {
		t.Fatal(err)
	}
	proved(t, m, desk.ID)
	_, err = m.Move(desk.ID, Done, closedReason, person)
	refused(t, err, "apps/desk tree "+deskTree()+" has no passing desk stamp")
	stampAs(DeskStamps, deskTree())
	commit("apps/desk/src/main.rs", "fn main() { println!(\"changed\"); }\n")
	if after, _, _ := HeadTree(repo); after != goTree {
		t.Fatalf("a commit under apps/desk moved the Go tree from %s to %s", goTree, after)
	}
	_, err = m.Move(desk.ID, Done, closedReason, person)
	refused(t, err, "apps/desk tree "+deskTree()+" has no passing desk stamp")
	both, err := m.Create("DEMO", ticketWith("both", "internal/agent/loop/**", "apps/desk/**"), manager)
	if err != nil {
		t.Fatal(err)
	}
	proved(t, m, both.ID)
	stampAs(DeskStamps, deskTree())
	if err := os.Remove(filepath.Join(repo, ".local", FlowsStamps, goTree)); err != nil {
		t.Fatal(err)
	}
	_, err = m.Move(both.ID, Done, closedReason, person)
	refused(t, err, "has no passing flows stamp")
	if moved, err := m.Move(desk.ID, Done, closedReason, person); err != nil || moved.Status != Done {
		t.Fatalf("a desk ticket with its desk tree stamped and no Go stamp: %v", err)
	}
	bare := t.TempDir()
	gitIn(t, bare, "init", "-q")
	if err := os.WriteFile(filepath.Join(bare, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, bare, "add", "-A")
	gitIn(t, bare, "commit", "-q", "-m", "no desk")
	m.Store.Root = bare
	unread, err := m.Create("DEMO", ticketWith("no desk tree", "apps/desk/**"), manager)
	if err != nil {
		t.Fatal(err)
	}
	proved(t, m, unread.ID)
	_, err = m.Move(unread.ID, Done, closedReason, person)
	refused(t, err, "apps/desk")
}

func TestADoneMoveInTofusTreeIsRefusedWhileAFlowsAreaIsNotAFolder(t *testing.T) {
	jevSaysNothingOpen(t)
	repo := t.TempDir()
	gitIn(t, repo, "init", "-q")
	for path, body := range map[string]string{".gitignore": ".local/\n", "go.mod": "module tofu\n\ngo 1.25\n"} {
		if err := os.WriteFile(filepath.Join(repo, path), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitIn(t, repo, "add", "-A")
	gitIn(t, repo, "commit", "-q", "-m", "work")
	areas := flowsAreaList()
	lost := areas[1]
	for _, area := range areas {
		if area != lost {
			if err := os.MkdirAll(filepath.Join(repo, area), 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.MkdirAll(filepath.Dir(filepath.Join(repo, lost)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, lost), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	m := managedBoard(t, "DEMO")
	m.Store.Root = repo
	library, err := m.Create("DEMO", ticketWith("library", "library/**"), manager)
	if err != nil {
		t.Fatal(err)
	}
	proved(t, m, library.ID)
	_, err = m.Move(library.ID, Done, closedReason, person)
	refused(t, err, lost+" is not a folder")
	if err := os.Remove(filepath.Join(repo, lost)); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, lost), 0o755); err != nil {
		t.Fatal(err)
	}
	if moved, err := m.Move(library.ID, Done, closedReason, person); err != nil || moved.Status != Done {
		t.Fatalf("a ticket outside the gated areas in a tree holding every area was refused: %v", err)
	}
}

func TestFlowsAreasReachDeskAndSessions(t *testing.T) {
	for glob, want := range map[string]string{
		"apps/desk/**":                     "apps/desk",
		"apps/desk/crates/ui/src/theme.rs": "apps/desk",
		"apps/**":                          "apps/cli, apps/desk",
		"apps/cli/**":                      "apps/cli",
		"internal/cli/**":                  "internal/cli",
		"internal/cli/verbs/lint/lint.go":  "internal/cli",
		"docs/x.md":                        "",
		"internal/store/session/**":        "internal/store/session",
		"apps/other/**":                    "",
		"internal/store/sessions/**":       "",
	} {
		if got := strings.Join(flowsAreas([]string{glob}), ", "); got != want {
			t.Errorf("%s gates %q, want %q", glob, got, want)
		}
	}
}

func TestDoneNeedsEveryAcceptanceLineProved(t *testing.T) {
	jevSaysNothingOpen(t)
	m := managedBoard(t, "DEMO")
	two := ticketWith("two lines", "library/**")
	two.Acceptance = []string{"- the old line", "- first line\n- second line"}
	made, err := m.Create("DEMO", two, manager)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Evidence(made.ID, "go test passed", manager, 1); err != nil {
		t.Fatal(err)
	}
	_, err = m.Move(made.ID, Done, closedReason, manager)
	refused(t, err, "line 2, second line")
	if strings.Contains(err.Error(), "old line") || strings.Contains(err.Error(), "first line") {
		t.Errorf("the refusal names a line that is proved or no longer in force: %v", err)
	}
	if _, err := m.Check("move", made.ID, Done, manager); err == nil {
		t.Error("check allowed a move to done that the move refuses")
	}
	if _, err := m.Store.Edit(made.ID, func(t *Ticket) error { t.Status = Review; return nil }); err != nil {
		t.Fatal(err)
	}
	attention, err := m.Attention("DEMO", time.Now())
	if err != nil || slices.ContainsFunc(attention.Closable, func(n Need) bool { return n.ID == made.ID }) {
		t.Errorf("attention counts a ticket with an unproved line as closable: %+v, %v", attention.Closable, err)
	}
	if _, err := m.Move(made.ID, Dropped, "", manager); err != nil {
		t.Errorf("a drop with an unproved line was refused: %v", err)
	}

	left := assigned(t, m, "left for the lead", agent, Doing)
	if _, err := m.Store.Edit(left.ID, func(t *Ticket) error { t.Acceptance = []string{"- mine\n- the lead's"}; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := m.CaptureBefore(left.ID, "drove it by hand before the change", agent); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Evidence(left.ID, "go test passed", agent, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := m.MoveLeaving(left.ID, Review, "", agent, []int{2}); err != nil {
		t.Fatal(err)
	}
	_, err = m.Move(left.ID, Done, closedReason, manager)
	refused(t, err, "line 2, the lead's")
	if _, err := m.Evidence(left.ID, "drove it by hand", manager, 2); err != nil {
		t.Fatal(err)
	}
	if moved, err := m.Move(left.ID, Done, closedReason, manager); err != nil || moved.Status != Done {
		t.Fatalf("every line proved and still refused: %v", err)
	}
}

func TestDoneNeedsABeforeCapture(t *testing.T) {
	jevSaysNothingOpen(t)
	m := managedBoard(t, "DEMO")
	made, err := m.Create("DEMO", ticketWith("behaviour", "library/**"), manager)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.CaptureBefore(made.ID, "tofu x printed the old row", manager); err != nil {
		t.Fatal(err)
	}
	_, err = m.Move(made.ID, Done, closedReason, manager)
	refused(t, err, "line 1")
	if _, err := m.Evidence(made.ID, "tofu x printed the new row", manager, 1); err != nil {
		t.Fatal(err)
	}
	if moved, err := m.Move(made.ID, Done, closedReason, manager); err != nil || moved.Status != Done {
		t.Fatalf("a ticket with a before capture and every line proved: %v", err)
	}

	evidenced := func(id string) {
		if _, err := m.Evidence(id, "tofu x printed the new row", manager, 1); err != nil {
			t.Fatal(err)
		}
	}
	bare, err := m.Create("DEMO", ticketWith("no before", "library/**"), manager)
	if err != nil {
		t.Fatal(err)
	}
	evidenced(bare.ID)
	_, err = m.CaptureBefore(bare.ID, "written after the change was proved", manager)
	refused(t, err, "late")
	refused(t, err, "evidence")
	if _, err := m.Store.Edit(bare.ID, func(t *Ticket) error { t.Status = Review; return nil }); err != nil {
		t.Fatal(err)
	}
	_, err = m.Move(bare.ID, Done, closedReason, manager)
	refused(t, err, "no before capture")
	refused(t, err, "tofu boardy log "+bare.ID+" --before")
	refused(t, err, "made before")
	if _, err := m.Check("move", bare.ID, Done, manager); err == nil {
		t.Error("check allowed a move to done that the move refuses")
	}
	attention, err := m.Attention("DEMO", time.Now())
	if err != nil || slices.ContainsFunc(attention.Closable, func(n Need) bool { return n.ID == bare.ID }) {
		t.Errorf("attention counts a ticket with no before capture as closable: %+v, %v", attention.Closable, err)
	}

	docs := ticketWith("docs", "docs/**")
	docs.Labels = []string{DocsOnlyLabel}
	marked, err := m.Create("DEMO", docs, manager)
	if err != nil {
		t.Fatal(err)
	}
	evidenced(marked.ID)
	if moved, err := m.Move(marked.ID, Done, closedReason, manager); err != nil || !strings.Contains(moved.Reason, DocsOnlyLabel) {
		t.Fatalf("a docs-only ticket with no before capture: reason %q, %v", moved.Reason, err)
	}

	old, err := m.Create("DEMO", ticketWith("made before the gate", "library/**"), manager)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Store.Edit(old.ID, func(t *Ticket) error { t.Created = time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC); return nil }); err != nil {
		t.Fatal(err)
	}
	evidenced(old.ID)
	if moved, err := m.Move(old.ID, Done, closedReason, manager); err != nil || !strings.Contains(moved.Reason, "made before") {
		t.Fatalf("a ticket made before the gate: reason %q, %v", moved.Reason, err)
	}
}

func TestABeforeAfterTheTicketsFirstCommitIsLate(t *testing.T) {
	repo := t.TempDir()
	commit := func(path string, at time.Time) {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(repo, path)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repo, path), []byte(at.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		gitIn(t, repo, "add", "-A")
		cmd := exec.Command("git", "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", path)
		cmd.Dir, cmd.Env = repo, append(os.Environ(), "GIT_COMMITTER_DATE="+at.Format(time.RFC3339), "GIT_AUTHOR_DATE="+at.Format(time.RFC3339))
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("commit %s: %v %s", path, err, out)
		}
	}
	gitIn(t, repo, "init", "-q")
	m := managedBoard(t, "DEMO")
	m.Store.Root = repo
	empty := ticketWith("no commit yet", "lib/**")
	empty.Status = Doing
	fresh, err := m.Create("DEMO", empty, manager)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.CaptureBefore(fresh.ID, "a repository with no commit has nothing late", manager); err != nil {
		t.Fatalf("a before in a repository with no commit: %v", err)
	}
	if _, err := m.Move(fresh.ID, Dropped, "", manager); err != nil {
		t.Fatal(err)
	}
	commit("lib/a.txt", time.Now().Add(-time.Hour))
	doing := ticketWith("late", "lib/**")
	doing.Status = Doing
	made, err := m.Create("DEMO", doing, manager)
	if err != nil {
		t.Fatal(err)
	}
	commit("elsewhere/b.txt", time.Now().Add(time.Hour))
	if _, err := m.CaptureBefore(made.ID, "a commit outside the owns leaves the before on time", manager); err != nil {
		t.Fatalf("a before after a commit outside the owns: %v", err)
	}
	commit("lib/a.txt", time.Now().Add(2*time.Hour))
	_, err = m.CaptureBefore(made.ID, "too late", manager)
	refused(t, err, "late")
	if thread, _ := m.Store.Thread(made.ID); slices.ContainsFunc(thread, func(e Event) bool { return e.Text == "too late" }) {
		t.Error("a late before was written to the thread")
	}
}

func TestDoneReasonHeldWhenJevCannotJudgeIt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	m := managedBoard(t, "DEMO")
	made, err := m.Create("DEMO", ticketWith("judged", "library/**"), manager)
	if err != nil {
		t.Fatal(err)
	}
	proved(t, m, made.ID)
	places, err := sys.ResolvePlaces(m.Store.Root)
	if err != nil {
		t.Fatal(err)
	}
	rows := func() []ledger.Row {
		var found []ledger.Row
		_, err := ledger.NewReader(places.Log).Each(ledger.Filter{Point: "done_reason"}, func(row ledger.Row) error {
			found = append(found, row)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return found
	}
	var waiting WaitingError
	if _, err := m.Move(made.ID, Done, "all done", manager); !errors.As(err, &waiting) || !strings.Contains(err.Error(), "done_reason@1") {
		t.Fatalf("a lead's done with a reason Jev could not read: %v, want it held for the person", err)
	}
	if got := rows(); len(got) != 1 || got[0].Verdict != ledger.VerdictAsk || !strings.Contains(string(got[0].State), "all done") {
		t.Fatalf("the unread reason left ledger rows %+v, want one ask carrying the reason", got)
	}
	if moved, err := m.Move(made.ID, Done, "all done", person); err != nil || moved.Status != Done {
		t.Fatalf("the person's own done when Jev could not read the reason: %v", err)
	}
	quiet, err := m.Create("DEMO", ticketWith("no reason", "library/**"), manager)
	if err != nil {
		t.Fatal(err)
	}
	proved(t, m, quiet.ID)
	_, err = m.Move(quiet.ID, Done, "  ", manager)
	refused(t, err, "its reason is empty")
	if got := rows(); len(got) != 2 {
		t.Errorf("the ledger holds %d done_reason rows, want 2: a blank reason is refused before Jev is asked", len(got))
	}
}

func TestOpenPartQuotesWhatJevChose(t *testing.T) {
	reason := "all done except the dark theme, follow up later"
	parts := reasonParts(reason)
	if !slices.Equal(parts, []string{"all done except the dark theme", "follow up later"}) {
		t.Fatalf("the reason cuts into %q", parts)
	}
	if got := reasonParts("fixed the v0.5.9 parser. Then the tests; done\nok"); !slices.Equal(got, []string{"fixed the v0.5.9 parser", "Then the tests", "done", "ok"}) {
		t.Errorf("a reason with a version and three breaks cuts into %q", got)
	}
	if got := reasonParts(" , "); len(got) != 0 {
		t.Errorf("a blank reason cuts into %q", got)
	}
	for _, c := range []struct {
		open           float64
		choice, quoted string
		refused        bool
	}{
		{0.97, "part_1", "all done except the dark theme", true},
		{0.5, "part_2", "follow up later", true},
		{0.97, "none", reason, true},
		{0.49, "part_1", "", false},
	} {
		answers := map[string]jev.Answer{"open": {Kind: jev.QuestionNoul, Noul: c.open}, "part": {Kind: jev.QuestionChoice, Choice: c.choice}}
		quoted, refused := openPart(answers, reason, parts, 0.5)
		if quoted != c.quoted || refused != c.refused {
			t.Errorf("open %.2f choosing %s: quoted %q refused %v, want %q %v", c.open, c.choice, quoted, refused, c.quoted, c.refused)
		}
	}
}
