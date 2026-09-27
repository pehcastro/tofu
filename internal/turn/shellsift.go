package turn

import (
	"context"
	"fmt"
	"strings"

	"tofu/internal/judge/method"
	"tofu/internal/sift"
	shipped "tofu/library"
)

const shellSiftPoint = sift.ShellSchema

const shellSiftRuleRef = shellSiftPoint + "@1"

type ShellScores interface {
	Score(ctx context.Context, shell sift.Shell, units []sift.Unit, task string) (map[int]float64, error)
}

type ShellSift struct {
	Methods method.Table
	KeepAt  float64
	Mode    sift.Mode
	Scores  ShellScores
}

type RuleContradictsTheTableError struct {
	Method method.Method
	Mode   sift.Mode
	Rule   string
	Table  string
}

func (e RuleContradictsTheTableError) Error() string {
	return fmt.Sprintf("%s names %s for %s and %s declares mode %s. the method and the mode are one state: flip the rule or unwire the point",
		e.Table, e.Method, shellSiftPoint, e.Rule, e.Mode)
}

func NewShellSift(scores ShellScores) (ShellSift, error) {
	table, err := method.Load(shipped.Files())
	if err != nil {
		return ShellSift{}, err
	}
	rule, err := sift.LoadShellRule(shipped.Files(), shellSiftRuleRef)
	if err != nil {
		return ShellSift{}, err
	}
	if err := ruleAgreesWithTheTable(table, rule); err != nil {
		return ShellSift{}, err
	}
	return ShellSift{Methods: table, KeepAt: rule.KeepAt, Mode: rule.Mode, Scores: scores}, nil
}

func ruleAgreesWithTheTable(table method.Table, rule sift.ShellRule) error {
	chosen, err := table.Of(shellSiftPoint)
	if err != nil {
		return err
	}
	if chosen.Method == method.Unwired || !rule.ModeDeclared || rule.Mode != sift.ModeShadow {
		return nil
	}
	return RuleContradictsTheTableError{Method: chosen.Method, Mode: rule.Mode, Rule: rule.File, Table: table.File}
}

type ShellCut struct {
	Text   string
	Saved  int
	Method method.Method
}

func siftOrNothing(given *ShellSift) *ShellSift {
	if given != nil {
		return given
	}
	built, err := NewShellSift(nil)
	if err != nil {
		return nil
	}
	return &built
}

func (g gatedCall) cutShellResult(ctx context.Context, tool string, result Result) ShellCut {
	if g.sift == nil || tool != bashToolName || result.ExitCode == nil {
		return ShellCut{Text: result.Content}
	}
	code := *result.ExitCode
	stdout := strings.TrimSuffix(result.Content, fmt.Sprintf(commandExited, code))
	if stdout != "" && !strings.HasSuffix(stdout, "\n") {
		stdout += "\n"
	}
	shell := sift.Shell{Command: result.Command, Stdout: stdout, ExitCode: code}
	cut, err := g.sift.Cut(ctx, shell, g.task)
	if err != nil {
		return ShellCut{Text: result.Content}
	}
	return cut
}

func (s ShellSift) Cut(ctx context.Context, shell sift.Shell, task string) (ShellCut, error) {
	units := sift.SplitShell(shell)
	whole := sift.JoinUnits(units)
	marks, chosen, err := method.Run(ctx, s.Methods, shellSiftPoint, method.Arms[[]sift.Mark]{
		Cheap:  func(context.Context) ([]sift.Mark, error) { return cheapMarks(units), nil },
		Judged: s.judged(shell, units, task),
	})
	if err != nil {
		return ShellCut{Text: whole, Method: chosen}, err
	}
	elided := 0
	for i, unit := range units {
		if !marks[i].Keep {
			elided += len(unit.Text)
		}
	}
	if elided == 0 {
		return ShellCut{Text: whole, Method: chosen}, nil
	}
	cut := sift.Message(units, marks, s.Mode) +
		fmt.Sprintf("[sift: %d of %d bytes of output removed, %s]\n", elided, len(whole), chosen)
	if len(cut) >= len(whole) {
		return ShellCut{Text: whole, Method: chosen}, nil
	}
	return ShellCut{Text: cut, Saved: len(whole) - len(cut), Method: chosen}, nil
}

func cheapMarks(units []sift.Unit) []sift.Mark {
	marks := make([]sift.Mark, len(units))
	for i, unit := range units {
		marks[i] = sift.ShellCheap(unit)
	}
	return marks
}

func (s ShellSift) judged(shell sift.Shell, units []sift.Unit, task string) func(context.Context) ([]sift.Mark, error) {
	if s.Scores == nil {
		return nil
	}
	return func(ctx context.Context) ([]sift.Mark, error) {
		scores, err := s.Scores.Score(ctx, shell, units, task)
		if err != nil {
			return nil, err
		}
		marks := make([]sift.Mark, len(units))
		for i, unit := range units {
			answers := map[string]float64{}
			if score, answered := scores[i]; answered {
				answers[sift.NeededQuestion] = score
			}
			mark, err := sift.DecideShell(unit, answers, s.KeepAt)
			if err != nil {
				mark = sift.Mark{Keep: true, Reason: "kept, unanswered: " + err.Error()}
			}
			marks[i] = mark
		}
		return marks, nil
	}
}
