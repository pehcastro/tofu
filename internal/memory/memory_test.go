package memory

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"tofu/internal/settings"
	"tofu/internal/sys"
)

const (
	firstAuthor  = "first.author@example.test"
	secondAuthor = "second.author@example.test"
)

func freshHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "no-gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

func gitAs(t *testing.T, repo, email string) {
	t.Helper()
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", email}} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
}

func opened(t *testing.T, project string) Memory {
	t.Helper()
	m, err := Open(project)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func sent(t *testing.T, m Memory) string {
	t.Helper()
	block, err := m.Block()
	if err != nil {
		t.Fatal(err)
	}
	return block
}

func TestFourScopesKeepTheIdentityOutOfTheRepositoryAndShowAnotherAuthorOnlyWhenAsked(t *testing.T) {
	freshHome(t)
	repo := t.TempDir()
	gitAs(t, repo, firstAuthor)
	m := opened(t, repo)
	at := time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)
	for _, scope := range []Scope{UserLocal, ProjectLocal, Project, Global} {
		if _, err := m.Add(Entry{Scope: scope, Kind: KindProject, Text: "tickets before code in " + string(scope), At: at, By: ByPerson}, ""); err != nil {
			t.Fatalf("add to %s: %v", scope, err)
		}
	}
	again := opened(t, repo)
	ids := map[string]bool{}
	for _, shelf := range again.Shelves() {
		if len(shelf.Entries) != 1 || shelf.Entries[0].Scope != shelf.Scope || !shelf.Entries[0].Yours {
			t.Fatalf("the %s shelf reads back %+v, want its one entry, written by you", shelf.Scope, shelf.Entries)
		}
		ids[shelf.Entries[0].ID] = true
	}
	if len(ids) != 4 {
		t.Errorf("four entries got the ids %v, want four distinct", ids)
	}
	local := again.UserLocal.Entries[0]
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(local.Author) {
		t.Errorf("the user local author is %q, want an opaque 32 hex digit id", local.Author)
	}
	salt, err := os.ReadFile(filepath.Join(repo, sys.StateDirName, "memory", "salt"))
	if err != nil || len(strings.TrimSpace(string(salt))) == 0 {
		t.Fatalf("no salt in the repository: %v", err)
	}
	gitAs(t, repo, secondAuthor)
	firstHome := os.Getenv("USERPROFILE")
	freshHome(t)
	other := opened(t, repo)
	if len(other.UserLocal.Entries) != 0 || len(other.ProjectLocal.Entries) != 1 {
		t.Fatalf("another author sees %d user local and %d project local entries, want none of the first author's user local and the project local one", len(other.UserLocal.Entries), len(other.ProjectLocal.Entries))
	}
	if strings.Contains(sent(t, other), local.Ref()) {
		t.Errorf("another author's block carries %s with memoryFromAllUsers off", local.Ref())
	}
	theirs, err := other.Add(Entry{Scope: UserLocal, Kind: KindPerson, Text: "another home, the same counter", At: at, By: ByPerson}, "")
	if err != nil || theirs.ID == local.ID || !regexp.MustCompile(`^m\d+-[0-9a-f]{4}$`).MatchString(theirs.ID) {
		t.Fatalf("a second home wrote %q (%v) beside %q, want a repository id tagged so another home's counter cannot equal it", theirs.ID, err, local.ID)
	}
	t.Setenv("USERPROFILE", firstHome)
	t.Setenv("HOME", firstHome)
	walked := 0
	err = filepath.WalkDir(filepath.Join(repo, sys.StateDirName), func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		walked++
		if entry.Name() != entriesFile && entry.Name() != saltFile {
			t.Errorf("%s is in the repository, and only entries and the salt belong there", path)
		}
		data, err := os.ReadFile(path)
		if strings.Contains(string(data)+path, "author@example") {
			t.Errorf("%s carries the identity", path)
		}
		return err
	})
	if err != nil || walked != 4 {
		t.Fatalf("walked %d files under the repository, want two user local, one project local and the salt: %v", walked, err)
	}
	home, _ := sys.HomeConfigDir()
	store, err := settings.Open(filepath.Join(home, settings.FileName), filepath.Join(repo, sys.StateDirName, settings.FileName))
	if err == nil {
		err = store.SetText(settings.Project, settings.MemoryFromAllUsers, settings.AllUsersOn)
	}
	if err != nil {
		t.Fatal(err)
	}
	all := opened(t, repo)
	first, err := all.Find(UserLocal, local.ID)
	if len(all.UserLocal.Entries) != 2 || err != nil || first.Yours || first.Author != local.Author {
		t.Fatalf("with memoryFromAllUsers on the user local shelf holds %+v, want the first author's entry, not yours, beside your own", all.UserLocal.Entries)
	}
	block := sent(t, all)
	order := []int{}
	for _, scope := range []Scope{Global, Project, UserLocal, ProjectLocal} {
		order = append(order, strings.Index(block, "\n"+string(scope)))
	}
	if order[0] < 0 || order[0] > order[1] || order[1] > order[2] || order[2] > order[3] || !strings.Contains(block, local.Ref()) {
		t.Errorf("the block is %q, want user global, project global, user local, project local in that order with %s", block, local.Ref())
	}
}

func TestOldShelvesMoveIntoTheHomeScopesOnceAndNothingIntoTheRepository(t *testing.T) {
	freshHome(t)
	repo := t.TempDir()
	gitAs(t, repo, firstAuthor)
	home, _ := sys.HomeConfigDir()
	state, _ := sys.ProjectStateDirAt(repo)
	old := map[string]string{
		filepath.Join(home, "memory", "m1.yaml"):  "id: m1\nkind: person\ntext: replies stay short\nsaid: \"keep it short\"\nat: 2026-09-01T10:00:00Z\nby: person\n",
		filepath.Join(state, "memory", "m2.yaml"): "id: m2\nkind: person\ntext: one ticket at a time\nat: 2026-09-02T10:00:00Z\nby: lead\n",
		filepath.Join(state, "memory", "m3.yaml"): "id: m3\nkind: project\ntext: the canvas uses gpui\nat: 2026-09-03T10:00:00Z\nby: person\n",
		filepath.Join(state, "memory", "m4.yaml"): "id: m4\nkind: reference\ntext: the bench lives in bench\nat: 2026-09-04T10:00:00Z\nby: person\n",
	}
	for file, body := range old {
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	moved := opened(t, repo)
	if len(moved.Notices) != 4 {
		t.Fatalf("the first open said %q, want one line per moved entry", moved.Notices)
	}
	want := map[Scope][]string{Global: {"m1", "m2"}, Project: {"m3", "m4"}}
	for scope, ids := range want {
		var got []string
		for _, e := range moved.shelf(scope).Entries {
			got = append(got, e.ID)
		}
		if strings.Join(got, ",") != strings.Join(ids, ",") {
			t.Errorf("%s holds %v after the move, want %v", scope, got, ids)
		}
	}
	if first := moved.Global.Entries[0]; first.Said != "keep it short" || first.At.Day() != 1 || first.Kind != KindPerson {
		t.Errorf("m1 moved as %+v, want its words, date and kind kept", first)
	}
	for file := range old {
		if _, err := os.Stat(file); err == nil {
			t.Errorf("%s is still there after the move", file)
		}
	}
	if _, err := os.Stat(filepath.Join(repo, sys.StateDirName)); err == nil {
		t.Errorf("the move wrote into the repository")
	}
	second := opened(t, repo)
	if len(second.Notices) != 0 || len(second.Global.Entries) != 2 || len(second.Project.Entries) != 2 {
		t.Errorf("the second open said %q with %d and %d entries, want nothing moved again", second.Notices, len(second.Global.Entries), len(second.Project.Entries))
	}
	added, err := second.Add(Entry{Scope: Project, Kind: KindProject, Text: "a new one", At: time.Now(), By: ByPerson}, "")
	if err != nil || added.ID != "m5" {
		t.Errorf("the next entry is %q (%v), want m5 after the moved m4", added.ID, err)
	}
}

func TestAFullScopeKeepsEveryWriteRemovesByAppendingAndSurvivesATornLine(t *testing.T) {
	freshHome(t)
	repo := t.TempDir()
	m := opened(t, repo)
	var kept Entry
	for i := range 80 {
		e, err := m.Add(Entry{Scope: Global, Kind: KindPerson, Text: strings.Repeat("every reply stays short and plain ", 4) + strings.Repeat("x", i), At: time.Now(), By: ByPerson}, "")
		if err != nil {
			t.Fatalf("write %d into a full scope was refused: %v", i, err)
		}
		kept = e
	}
	if kept.Author != UnknownAuthor || len(m.Notices) != 1 {
		t.Errorf("with no identity the author is %q and the notices are %q, want %s said once", kept.Author, m.Notices, UnknownAuthor)
	}
	if _, err := m.Remove(Global, "m1"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Add(Entry{Scope: Global, Kind: KindPerson, Text: "replaced text", At: time.Now(), By: ByPerson}, "m2"); err != nil {
		t.Fatal(err)
	}
	log, err := os.OpenFile(kept.File, os.O_APPEND|os.O_WRONLY, 0o644)
	if err == nil {
		_, err = log.WriteString(`{"id":"m99","kind":"per`)
	}
	if err = errors.Join(err, log.Close()); err != nil {
		t.Fatal(err)
	}
	back := opened(t, repo)
	if len(back.Global.Entries) != 79 || back.Global.Entries[0].ID != "m2" || back.Global.Entries[0].Text != "replaced text" {
		t.Fatalf("after a removal, a replace and a torn line the scope holds %d entries starting %+v, want 79 starting with the replaced m2", len(back.Global.Entries), back.Global.Entries[0])
	}
	block := sent(t, back)
	if lines := strings.Count(block, "\n"); lines >= 80 || !strings.Contains(block, "replaced text") {
		t.Errorf("a scope of 80 entries over its budget sent %d lines, want it coarser than one line per entry and the newest text whole", lines)
	}
}

func TestALeadRuleIsOneLineOfAtMost160BytesNamingNoOne(t *testing.T) {
	at160 := strings.Repeat("a", 159) + "."
	cases := []struct {
		statement, rule, refusal string
	}{
		{"  Use the shared components; never build a parallel copy.  ", "Use the shared components; never build a parallel copy.", ""},
		{at160, at160, ""},
		{"  " + at160 + "\t", at160, ""},
		{at160 + "b", "", "161 bytes"},
		{strings.Repeat("€", 54), "", "162 bytes"},
		{"Use the shared components.\nAlso keep replies short.", "", "one line"},
		{"Use the shared components.\r\nAlso keep replies short.", "", "one line"},
		{"   ", "", "empty"},
		{"The user wants the shared components used.", "", "names or describes the person"},
		{"She prefers the shared components.", "", "names or describes the person"},
	}
	for _, c := range cases {
		rule, err := Rule(c.statement)
		switch {
		case c.refusal == "" && (err != nil || rule != c.rule):
			t.Errorf("Rule(%q) = %q, %v, want %q kept", c.statement, rule, err, c.rule)
		case c.refusal != "" && (err == nil || !strings.Contains(err.Error(), c.refusal)):
			t.Errorf("Rule(%q) = %q, %v, want a refusal saying %q", c.statement, rule, err, c.refusal)
		}
	}
}
