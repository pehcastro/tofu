package main

import (
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const (
	checkTests = "visible tests pass"
	checkGofmt = "gofmt clean"
	checkVet   = "go vet clean"
	checkGone  = "old name gone"
	checkStray = "no stray files"
)

var harnessDirs = []string{".tofu/", ".omp/", ".claude/", ".codex/", ".agents/"}

type score struct {
	Hidden       map[string]bool `json:"hidden"`
	Rubric       map[string]bool `json:"rubric"`
	FilesChanged int             `json:"files_changed"`
	Added        int             `json:"lines_added"`
	Deleted      int             `json:"lines_deleted"`
	Stray        []string        `json:"stray,omitempty"`
	HarnessFiles []string        `json:"harness_files,omitempty"`
	Failures     []string        `json:"failures,omitempty"`
}

func (s score) hiddenPassed() int {
	return countTrue(s.Hidden)
}

func (s score) rubricPassed() int {
	return countTrue(s.Rubric)
}

func (s score) quality() (int, int) {
	return s.hiddenPassed() + s.rubricPassed(), len(s.Hidden) + len(s.Rubric)
}

func countTrue(checks map[string]bool) int {
	n := 0
	for _, passed := range checks {
		if passed {
			n++
		}
	}
	return n
}

func (s score) text() string {
	var b strings.Builder
	for _, part := range slices.Sorted(maps.Keys(s.Hidden)) {
		fmt.Fprintf(&b, "  hidden %-10s %s\n", part, verdict(s.Hidden[part]))
	}
	for _, check := range []string{checkTests, checkGofmt, checkVet, checkGone, checkStray} {
		if passed, scored := s.Rubric[check]; scored {
			fmt.Fprintf(&b, "  rubric %-18s %s\n", check, verdict(passed))
		}
	}
	fmt.Fprintf(&b, "  hidden %d of %d, rubric %d of %d\n", s.hiddenPassed(), len(s.Hidden), s.rubricPassed(), len(s.Rubric))
	for _, failure := range s.Failures {
		fmt.Fprintf(&b, "  %s\n", failure)
	}
	return b.String()
}

func verdict(passed bool) string {
	if passed {
		return "pass"
	}
	return "FAIL"
}

func scoreRepo(repo string, env []string, t task) score {
	s := score{Hidden: map[string]bool{}, Rubric: map[string]bool{}}
	run := func(name string, args ...string) (string, bool) {
		cmd := exec.Command(name, args...)
		cmd.Dir, cmd.Env = repo, env
		out, err := cmd.CombinedOutput()
		return string(out), err == nil
	}
	_, _ = run("git", "add", "-A")
	numstat, _ := run("git", "diff", "--cached", "--numstat")
	for _, line := range strings.Split(strings.TrimSpace(numstat), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		s.FilesChanged++
		if isHarness(fields[2]) {
			s.HarnessFiles = append(s.HarnessFiles, fields[2])
			continue
		}
		added, _ := strconv.Atoi(fields[0])
		deleted, _ := strconv.Atoi(fields[1])
		s.Added, s.Deleted = s.Added+added, s.Deleted+deleted
		if !t.expects(fields[2]) {
			s.Stray = append(s.Stray, fields[2])
		}
	}
	out, passed := run("go", "test", "-count=1", "./...")
	s.Rubric[checkTests] = passed
	keepFailure(&s, "visible tests", out, passed)
	unformatted, _ := run("gofmt", "-l", ".")
	s.Rubric[checkGofmt] = strings.TrimSpace(unformatted) == ""
	out, passed = run("go", "vet", "./...")
	s.Rubric[checkVet] = passed
	keepFailure(&s, "go vet", out, passed)
	if len(t.Gone) > 0 {
		s.Rubric[checkGone] = !anyGoFileMatches(repo, t.Gone)
	}
	s.Rubric[checkStray] = len(s.Stray) == 0
	for _, h := range t.Hidden {
		s.Hidden[h.Part] = false
		body, err := os.ReadFile(filepath.Join(t.dir(), "hidden", h.Part+".go.txt"))
		if err != nil {
			s.Failures = append(s.Failures, err.Error())
			continue
		}
		dir := filepath.Join(repo, h.Into)
		_, statErr := os.Stat(dir)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			s.Failures = append(s.Failures, err.Error())
			continue
		}
		placed := filepath.Join(dir, "hidden_"+h.Part+"_test.go")
		if err := os.WriteFile(placed, body, 0o644); err != nil {
			s.Failures = append(s.Failures, err.Error())
			continue
		}
		out, passed := run("go", "test", "-count=1", "-run", "TestHidden", "./"+filepath.ToSlash(h.Into))
		s.Hidden[h.Part] = passed
		keepFailure(&s, "hidden "+h.Part, out, passed)
		_ = os.Remove(placed)
		if os.IsNotExist(statErr) {
			_ = os.RemoveAll(dir)
		}
	}
	return s
}

func keepFailure(s *score, label, out string, passed bool) {
	if passed {
		return
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "FAIL") && !strings.HasPrefix(line, "---") && !strings.HasPrefix(line, "ok") && !strings.HasPrefix(line, "#") {
			s.Failures = append(s.Failures, label+": "+line)
			return
		}
	}
	s.Failures = append(s.Failures, label+": failed")
}

func isHarness(path string) bool {
	return slices.ContainsFunc(harnessDirs, func(dir string) bool { return strings.HasPrefix(path, dir) })
}

func anyGoFileMatches(repo string, patterns []string) bool {
	gone := regexp.MustCompile(strings.Join(patterns, "|"))
	found := false
	_ = filepath.WalkDir(repo, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".git" {
			return fs.SkipDir
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		body, err := os.ReadFile(path)
		found = err == nil && gone.Match(body)
		if found {
			return fs.SkipAll
		}
		return nil
	})
	return found
}
