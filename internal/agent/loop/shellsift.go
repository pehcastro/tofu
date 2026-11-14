package loop

import (
	"context"
	"fmt"
	"strings"
	"time"

	"tofu/internal/judge/points"
	"tofu/internal/konst"
	"tofu/internal/platform/outside"
	"tofu/internal/store/session"
	"tofu/internal/store/walls"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/shell/bash/sift"
	shipped "tofu/library"
)

const shellSiftPoint = sift.ShellSchema

const shellSiftRuleRef = shellSiftPoint + "@1"

const timeoutNoteLead = "bash: timeout_ms "

type ShellScores interface {
	Score(ctx context.Context, shell sift.Shell, units []sift.Unit, task string) (map[int]float64, error)
}

type ShellSift struct {
	Methods points.Table
	KeepAt  float64
	Mode    sift.Mode
	Scores  ShellScores
}

type RuleContradictsTheTableError struct {
	Method points.Method
	Mode   sift.Mode
	Rule   string
	Table  string
}

func (e RuleContradictsTheTableError) Error() string {
	return fmt.Sprintf("%s names %s for %s and %s declares mode %s. the method and the mode are one state: flip the rule or unwire the point",
		e.Table, e.Method, shellSiftPoint, e.Rule, e.Mode)
}

func NewShellSift(scores ShellScores) (ShellSift, error) {
	table, err := points.Load(shipped.Files())
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

func ruleAgreesWithTheTable(table points.Table, rule sift.ShellRule) error {
	chosen, err := table.Of(shellSiftPoint)
	if err != nil {
		return err
	}
	if chosen.Method == points.Unwired || !rule.ModeDeclared || rule.Mode != sift.ModeShadow {
		return nil
	}
	return RuleContradictsTheTableError{Method: chosen.Method, Mode: rule.Mode, Rule: rule.File, Table: table.File}
}

type ShellCut struct {
	Text   string
	Saved  int
	Method points.Method
	Folded bool
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

func (g gatedCall) cutShellResult(ctx context.Context, tool string, result contract.Result) ShellCut {
	if g.sift == nil || tool != bashToolName || result.ExitCode == nil {
		return ShellCut{Text: result.Content}
	}
	code := *result.ExitCode
	stdout := strings.TrimSuffix(result.Content, fmt.Sprintf(commandExited, code))
	if stdout != "" && !strings.HasSuffix(stdout, "\n") {
		stdout += "\n"
	}
	note := ""
	for _, line := range strings.SplitAfter(stdout, "\n") {
		if !strings.HasPrefix(line, heldDropLead) && !strings.HasPrefix(line, timeoutNoteLead) {
			break
		}
		note += line
	}
	shell := sift.Shell{Command: result.Command, Note: note, Stdout: stdout, ExitCode: code}
	started := time.Now()
	clocked, clock := outside.Open(ctx, time.Now)
	cut, err := g.sift.Cut(clocked, shell, g.task)
	if g.site.log != nil {
		method, jev := string(cut.Method), clock.OutsideOf(outside.Jev).Milliseconds()
		if cut.Folded {
			method = "fold"
		}
		_, _ = g.site.log.Append(session.Event{Turn: g.site.turn, Agent: g.site.agent, Call: g.site.call, Kind: session.EventWall},
			walls.Wall{Op: walls.OpSift, Name: method, MS: time.Since(started).Milliseconds(), At: started.UnixMilli(), OK: err == nil, OutsideMS: &jev})
	}
	if err != nil {
		return ShellCut{Text: result.Content}
	}
	return cut
}

func (s ShellSift) Cut(ctx context.Context, shell sift.Shell, task string) (ShellCut, error) {
	units := sift.SplitShell(shell)
	whole := sift.JoinUnits(units)
	original := len(whole)
	if folded, known := sift.Fold(shell); known {
		return ShellCut{Text: folded, Saved: max(0, original-len(folded)), Method: points.Cheap, Folded: true}, nil
	}
	if strings.Count(shell.Stdout, "\n") > konst.ShellDrainLinesFloor {
		shell.Stdout = sift.Drain(shell.Stdout, konst.ShellDrainSimilarityPercent, konst.ShellDrainTemplatesCap)
		units = sift.SplitShell(shell)
		whole = sift.JoinUnits(units)
	}
	marks, chosen, err := points.Run(ctx, s.Methods, shellSiftPoint, points.Arms[[]sift.Mark]{
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
		return ShellCut{Text: whole, Saved: original - len(whole), Method: chosen}, nil
	}
	cut := sift.Message(units, marks, s.Mode) +
		fmt.Sprintf("[sift: %d of %d bytes of output removed, %s]\n", elided, len(whole), chosen)
	if len(cut) >= len(whole) {
		return ShellCut{Text: whole, Saved: original - len(whole), Method: chosen}, nil
	}
	return ShellCut{Text: cut, Saved: original - len(cut), Method: chosen}, nil
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
