package rule

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"unicode"

	"tofu/internal/subagent"
	"tofu/internal/sys"
)

const emDashRune = rune(0x2014)

type Checker struct {
	Subject Subject
	Check   func(Rule, Artifact) ([]Finding, error)
}

func Builtins() map[string]Checker {
	return map[string]Checker{
		"comments":            {Subject: SubjectGoFile, Check: checkComments},
		"em_dash":             {Subject: SubjectTextFile, Check: checkEmDash},
		"ownership":           {Subject: SubjectOwnsWrite, Check: checkOwnership},
		"no_worktree":         {Subject: SubjectShellCommand, Check: checkNoWorktree},
		"test_assertion":      {Subject: SubjectGoPackage, Check: checkTestAssertion},
		"test_mock_boundary":  {Subject: SubjectGoPackage, Check: checkTestMockBoundary},
		"test_boundary_cases": {Subject: SubjectGoPackage, Check: checkTestBoundaryCases},
		CheckerCommand:        {Subject: SubjectBoardEvent, Check: checkCommand},
		CheckerPersonApproves: {Subject: SubjectBoardEvent, Check: checkPersonApproves},
	}
}

const (
	CheckerCommand        = "command"
	CheckerPersonApproves = "person_approves"
)

func checkCommand(r Rule, a Artifact) ([]Finding, error) {
	be, ok := a.(BoardEvent)
	if !ok {
		return nil, artifactMismatch(SubjectBoardEvent, a)
	}
	argv := commandWords(r.Command)
	for i := range argv {
		argv[i] = strings.ReplaceAll(argv[i], "{ticket}", be.Path)
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = append(os.Environ(), "TOFU_BOARD_EVENT="+string(be.Event), "TOFU_TICKET="+be.Path, "TOFU_TICKET_ID="+be.Ticket,
		"TOFU_FROM="+be.From, "TOFU_TO="+be.To, "TOFU_ACTOR="+be.Actor)
	output, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		return nil, fmt.Errorf("rule %q runs %q: %w", r.ID, argv[0], err)
	}
	if err == nil {
		return nil, nil
	}
	detail := strings.TrimSpace(strings.Join([]string{r.Text, strings.TrimSpace(string(output))}, "\n"))
	return []Finding{{Target: be.Ticket, Detail: cmp.Or(detail, fmt.Sprintf("%s exited %d", argv[0], exit.ExitCode()))}}, nil
}

func commandWords(command string) []string {
	var words []string
	var word strings.Builder
	quote, open := rune(0), false
	for _, r := range command {
		switch {
		case quote != 0 && r == quote:
			quote = 0
		case quote == 0 && (r == '"' || r == '\''):
			quote, open = r, true
		case quote == 0 && unicode.IsSpace(r):
			if open {
				words, open = append(words, word.String()), false
				word.Reset()
			}
		default:
			word.WriteRune(r)
			open = true
		}
	}
	if open {
		words = append(words, word.String())
	}
	return words
}

func checkPersonApproves(r Rule, a Artifact) ([]Finding, error) {
	be, ok := a.(BoardEvent)
	if !ok {
		return nil, artifactMismatch(SubjectBoardEvent, a)
	}
	if be.ByPerson {
		return nil, nil
	}
	return []Finding{{Target: be.Ticket, Detail: cmp.Or(r.Text, "this waits for the person to approve it")}}, nil
}

func checkComments(_ Rule, a Artifact) ([]Finding, error) {
	gf, ok := a.(GoFile)
	if !ok {
		return nil, artifactMismatch(SubjectGoFile, a)
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
		return nil, artifactMismatch(SubjectTextFile, a)
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
		return nil, artifactMismatch(SubjectOwnsWrite, a)
	}
	matched, err := subagent.Matches(ow.Path, ow.Owns)
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
		return nil, artifactMismatch(SubjectShellCommand, a)
	}
	for i, arg := range sc.Argv {
		if i > 0 && sc.Argv[i-1] == "git" && arg == "worktree" {
			return []Finding{{Target: strings.Join(sc.Argv, " "), Detail: "git worktree is refused"}}, nil
		}
	}
	return nil, nil
}

func artifactMismatch(want Subject, got Artifact) error {
	return fmt.Errorf("rule: checker expects a %s artifact, got %T", want, got)
}
