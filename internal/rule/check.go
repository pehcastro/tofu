package rule

import (
	"fmt"
	"strings"

	"boji/internal/crew"
	"boji/internal/sys"
)

const emDashRune = rune(0x2014)

type Checker func(Rule, Artifact) ([]Finding, error)

func Builtins() map[string]Checker {
	return map[string]Checker{
		"comments":    checkComments,
		"em_dash":     checkEmDash,
		"ownership":   checkOwnership,
		"no_worktree": checkNoWorktree,
	}
}

func checkComments(_ Rule, a Artifact) ([]Finding, error) {
	gf, ok := a.(GoFile)
	if !ok {
		return nil, artifactMismatch("go_file", a)
	}
	comments, err := sys.FileCommentViolations(gf.Path)
	if err != nil {
		return nil, err
	}
	findings := make([]Finding, len(comments))
	for i, c := range comments {
		findings[i] = Finding{Target: fmt.Sprintf("%s:%d:%d", c.File, c.Line, c.Column), Detail: c.Text}
	}
	return findings, nil
}

func checkEmDash(r Rule, a Artifact) ([]Finding, error) {
	tf, ok := a.(TextFile)
	if !ok {
		return nil, artifactMismatch("text_file", a)
	}
	data, err := sys.ReadFile(tf.Path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	searched := lines
	if r.Except == ExceptionQuoted {
		searched = withoutQuotations(tf.Path, lines)
	}
	var findings []Finding
	for i, line := range searched {
		if strings.ContainsRune(line, emDashRune) {
			findings = append(findings, Finding{Target: fmt.Sprintf("%s:%d", tf.Path, i+1), Detail: lines[i]})
		}
	}
	return findings, nil
}

func checkOwnership(_ Rule, a Artifact) ([]Finding, error) {
	ow, ok := a.(OwnsWrite)
	if !ok {
		return nil, artifactMismatch("owns_write", a)
	}
	matched, err := crew.Matches(ow.Path, ow.Owns)
	if err != nil {
		return nil, err
	}
	if matched {
		return nil, nil
	}
	return []Finding{{Target: ow.Path, Detail: "not covered by " + strings.Join(ow.Owns, ", ")}}, nil
}

func checkNoWorktree(_ Rule, a Artifact) ([]Finding, error) {
	sc, ok := a.(ShellCommand)
	if !ok {
		return nil, artifactMismatch("shell_command", a)
	}
	for i, arg := range sc.Argv {
		if i > 0 && sc.Argv[i-1] == "git" && arg == "worktree" {
			return []Finding{{Target: strings.Join(sc.Argv, " "), Detail: "git worktree is refused"}}, nil
		}
	}
	return nil, nil
}

func artifactMismatch(want string, got Artifact) error {
	return fmt.Errorf("rule: checker expects a %s artifact, got %T", want, got)
}
