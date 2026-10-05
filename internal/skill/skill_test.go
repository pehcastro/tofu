package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func names(skills []Skill) string {
	var listed []string
	for _, one := range skills {
		listed = append(listed, one.Name+"="+filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(one.File))))))
	}
	return strings.Join(listed, " ")
}

func TestDiscoverWalksToTheRepositoryRootClosestFirst(t *testing.T) {
	root := t.TempDir()
	outside, repo, home := filepath.Join(root, "outside"), filepath.Join(root, "outside", "repo"), filepath.Join(root, "home")
	project := filepath.Join(repo, "sub")
	write(t, filepath.Join(repo, ".git", "HEAD"), "ref")
	write(t, filepath.Join(outside, ".claude", "skills", "beyond", "SKILL.md"), "---\ndescription: past the repository root\n---\nno")
	write(t, filepath.Join(project, ".claude", "skills", "commit", "SKILL.md"), "---\nname: commit\ndescription: near claude\n---\nnear")
	write(t, filepath.Join(repo, ".tofu", "skills", "commit", "SKILL.md"), "---\nname: commit\ndescription: far tofu\n---\nfar")
	write(t, filepath.Join(repo, ".agents", "skills", "header", "SKILL.md"), "---\r\ndescription: >\r\n  sets the\r\n  file header\r\n---\r\nbody\r\n")
	write(t, filepath.Join(project, ".claude", "skills", "bare", "SKILL.md"), "---\nname: bare\n---\nno description")
	write(t, filepath.Join(project, ".claude", "skills", ".hidden", "SKILL.md"), "---\ndescription: dot folder\n---\n")
	write(t, filepath.Join(project, ".claude", "skills", "stray.md"), "---\ndescription: a file\n---\n")
	write(t, filepath.Join(project, ".claude", "skills", "quiet", "SKILL.md"), "---\ndescription: \"not listed\"\ndisable-model-invocation: true\n---\nquiet body")
	write(t, filepath.Join(home, ".tofu", "skills", "tests", "SKILL.md"), "---\ndescription: 'home tests'\n---\n")
	write(t, filepath.Join(home, ".tofu", "skills", "header", "SKILL.md"), "---\ndescription: home header\n---\n")

	found := Discover(project, home)
	if got, want := names(found.Skills), "commit=repo header=repo quiet=sub tests=home"; got != want {
		t.Fatalf("skills %q, want %q: .tofu before .claude, the repository before the home, no dot folder, no stray file, nothing past the root", got, want)
	}
	for _, one := range found.Skills {
		if one.Name == "header" && one.Description != "sets the file header" {
			t.Fatalf("a folded CRLF description read as %q", one.Description)
		}
		if one.Name == "quiet" && !one.Hidden {
			t.Fatal("disable-model-invocation is not hidden")
		}
	}
	warned := strings.Join(found.Warnings, "\n")
	for _, want := range []string{"bare", "commit", "header"} {
		if !strings.Contains(warned, want) {
			t.Fatalf("no warning names %s: %s", want, warned)
		}
	}
}

func TestDiscoverWithNoRepositoryReadsOnlyTheWorkingFolder(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	write(t, filepath.Join(root, ".claude", "skills", "above", "SKILL.md"), "---\ndescription: above\n---\n")
	write(t, filepath.Join(project, ".claude", "skills", "here", "SKILL.md"), "---\ndescription: here\n---\n")
	if got := names(Discover(project, filepath.Join(root, "home")).Skills); got != "here=project" {
		t.Fatalf("skills %q, want only the working folder's", got)
	}
}

func TestTheHomeGivesOnlyTofuSkills(t *testing.T) {
	root := t.TempDir()
	project, home := filepath.Join(root, "project"), filepath.Join(root, "home")
	write(t, filepath.Join(project, "README.md"), "a project")
	write(t, filepath.Join(home, ".claude", "skills", "x", "SKILL.md"), "---\ndescription: claude home\n---\n")
	write(t, filepath.Join(home, ".agents", "skills", "z", "SKILL.md"), "---\ndescription: agents home\n---\n")
	if found := Discover(project, home); len(found.Skills) != 0 {
		t.Fatalf("another harness's home gave skills %q", names(found.Skills))
	}
	write(t, filepath.Join(home, ".tofu", "skills", "s", "SKILL.md"), "---\ndescription: tofu home\n---\n")
	if got := names(Discover(project, home).Skills); got != "s=home" {
		t.Fatalf("skills %q, want only ~/.tofu/skills/s", got)
	}
}

func TestTheHomeIsReadOnceWhenItIsAnAncestor(t *testing.T) {
	home := t.TempDir()
	project := filepath.Join(home, "repo")
	write(t, filepath.Join(home, ".git", "HEAD"), "ref")
	write(t, filepath.Join(home, ".tofu", "skills", "once", "SKILL.md"), "---\ndescription: once\n---\n")
	found := Discover(project, home)
	if len(found.Skills) != 1 || len(found.Warnings) != 0 {
		t.Fatalf("skills %v warnings %v, want one skill and no collision", found.Skills, found.Warnings)
	}
}

func TestListing(t *testing.T) {
	if got := Listing([]Skill{{Name: "quiet", Description: "d", Hidden: true}}, 0); got != "" {
		t.Fatalf("only hidden skills listed %q", got)
	}
	long := strings.Repeat("x", 2000)
	got := Listing([]Skill{{Name: "a", Description: long}}, 0)
	if !strings.HasPrefix(got, "If a skill matches the task, load it first.\n- a: ") || strings.Contains(got, strings.Repeat("x", 1025)) {
		t.Fatalf("listing %q is not capped at 1024 characters a description", got)
	}
	var many []Skill
	for _, name := range []string{"one", "two", "three", "four", "five"} {
		many = append(many, Skill{Name: name, Description: strings.Repeat("y", 1000)})
	}
	unknown := Listing(many, 0)
	if !strings.Contains(unknown, "- five: y") {
		t.Fatalf("an unknown window dropped descriptions under 8000 characters: %q", unknown)
	}
	small := Listing(many, 10000)
	if strings.Contains(small, "yyy") || !strings.Contains(small, "- five\n") || !strings.Contains(small, "5 descriptions") {
		t.Fatalf("a 10000 token window, 800 bytes, kept a description or lost a name or the count: %q", small)
	}
}

func TestLoad(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "commit")
	write(t, filepath.Join(dir, "SKILL.md"), "---\r\ndescription: d\r\n---\r\n\r\nthe body\r\n")
	write(t, filepath.Join(dir, "refs", "format.md"), "the format")
	write(t, filepath.Join(root, "secret.txt"), "secret")
	skills := []Skill{{Name: "commit", File: filepath.Join(dir, "SKILL.md"), Dir: dir}}
	if body, err := Load(skills, "commit", ""); err != nil || strings.TrimSpace(body) != "the body" {
		t.Fatalf("body %q err %v, want the body without front matter", body, err)
	}
	if body, err := Load(skills, "commit", "refs/format.md"); err != nil || body != "the format" {
		t.Fatalf("a file inside the folder: %q %v", body, err)
	}
	for _, escape := range []string{"../secret.txt", "refs/../../secret.txt", filepath.Join(root, "secret.txt"), "/secret.txt"} {
		if body, err := Load(skills, "commit", escape); err == nil {
			t.Fatalf("%s read outside the skill: %q", escape, body)
		}
	}
	if _, err := Load(skills, "nope", ""); err == nil || !strings.Contains(err.Error(), "commit") {
		t.Fatalf("an unknown name fails without naming what exists: %v", err)
	}
}
