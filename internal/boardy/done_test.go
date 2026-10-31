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

	"tofu/internal/judge/jev"
	"tofu/internal/judge/ledger"
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
	proved(t, m, turn.ID)
	_, err = m.Move(turn.ID, Done, "", person)
	refused(t, err, "go run ./bench/flows")
	if _, err := m.Check("move", turn.ID, Done, person); err == nil {
		t.Fatal("check allowed a move to done that the move refuses")
	}
	for _, owns := range []string{"apps/desk/**", "internal/session/**"} {
		gated, err := m.Create("DEMO", ticketWith(owns, owns), manager)
		if err != nil {
			t.Fatal(err)
		}
		proved(t, m, gated.ID)
		_, err = m.Move(gated.ID, Done, "", person)
		refused(t, err, "go run ./bench/flows")
	}
	library, err := m.Create("DEMO", ticketWith("library", "library/**"), manager)
	if err != nil {
		t.Fatal(err)
	}
	proved(t, m, library.ID)
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

func TestFlowsAreasReachDeskAndSessions(t *testing.T) {
	for glob, want := range map[string]string{
		"apps/desk/**":                     "apps/desk",
		"apps/desk/crates/ui/src/theme.rs": "apps/desk",
		"apps/**":                          "apps/desk",
		"internal/session/**":              "internal/session",
		"apps/other/**":                    "",
		"internal/sessions/**":             "",
	} {
		if got := flowsArea([]string{glob}); got != want {
			t.Errorf("%s gates %q, want %q", glob, got, want)
		}
	}
}

func TestDoneNeedsEveryAcceptanceLineProved(t *testing.T) {
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
	_, err = m.Move(made.ID, Done, "", manager)
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
	if _, err := m.Evidence(left.ID, "go test passed", agent, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := m.MoveLeaving(left.ID, Review, "", agent, []int{2}); err != nil {
		t.Fatal(err)
	}
	_, err = m.Move(left.ID, Done, "", manager)
	refused(t, err, "line 2, the lead's")
	if _, err := m.Evidence(left.ID, "drove it by hand", manager, 2); err != nil {
		t.Fatal(err)
	}
	if moved, err := m.Move(left.ID, Done, "", manager); err != nil || moved.Status != Done {
		t.Fatalf("every line proved and still refused: %v", err)
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
	if _, err := m.Move(quiet.ID, Done, "  ", manager); err != nil {
		t.Fatalf("a done with no reason was refused: %v", err)
	}
	if got := rows(); len(got) != 2 {
		t.Errorf("the ledger holds %d done_reason rows, want 2: a blank reason asks nothing", len(got))
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
