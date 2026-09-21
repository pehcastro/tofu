package mutate

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"tofu/internal/konst"
)

type Tool struct {
	Name   string
	Binary string
	Args   []string
}

var Gremlins = Tool{
	Name:   "gremlins",
	Binary: "gremlins",
	Args: []string{
		"unleash",
		"--timeout-coefficient", strconv.Itoa(konst.MutateTimeoutCoefficient),
		"--workers", strconv.Itoa(konst.MutateWorkers),
	},
}

var changes = map[string]string{
	"CONDITIONALS_BOUNDARY":   "a comparison boundary moved, > became >=",
	"CONDITIONALS_NEGATION":   "the condition was negated, == became !=",
	"ARITHMETIC_BASE":         "an arithmetic operator flipped, + became -",
	"INVERT_NEGATIVES":        "a negation was dropped, -x became x",
	"INCREMENT_DECREMENT":     "a step reversed, ++ became --",
	"INVERT_ASSIGNMENTS":      "a compound assignment flipped, += became -=",
	"INVERT_BITWISE":          "a bitwise operator flipped, & became |",
	"INVERT_LOGICAL":          "a logical operator flipped, && became ||",
	"INVERT_LOOPCTRL":         "a loop control reversed, break became continue",
	"REMOVE_SELF_ASSIGNMENTS": "a self assignment was removed",
}

type Outcome struct {
	Tool    string
	Package string
	Elapsed time.Duration
	Mutants []Mutant
}

func (t Tool) Command(ctx context.Context, dir, pattern string) *exec.Cmd {
	argv := make([]string, 0, len(t.Args)+1)
	argv = append(argv, t.Args...)
	argv = append(argv, pattern)
	cmd := exec.CommandContext(ctx, t.Binary, argv...)
	cmd.Dir = dir
	return cmd
}

func (t Tool) Run(ctx context.Context, dir, pattern string) (Outcome, error) {
	started := time.Now()
	cmd := t.Command(ctx, dir, pattern)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	runErr := run(ctx, cmd, konst.MutateChildMemoryCeilingBytes)
	mutants, err := Parse(output.String())
	if err != nil {
		return Outcome{}, err
	}
	if len(mutants) == 0 && runErr != nil {
		return Outcome{}, fmt.Errorf("%s in %s: %w: %s", t.Name, dir, runErr, strings.TrimSpace(output.String()))
	}
	return Outcome{Tool: t.Name, Package: pattern, Elapsed: time.Since(started), Mutants: mutants}, nil
}

func Render(o Outcome) string {
	score := Tally(o.Mutants)
	var out strings.Builder
	fmt.Fprintf(&out, "%s %s %.2f%% efficacy, %d killed, %d survived, %d not covered, %d timed out, %s\n",
		o.Tool, o.Package, score.Efficacy(), score.Killed, score.Lived, score.NotCovered, score.TimedOut, o.Elapsed.Round(time.Second))
	for _, m := range Survivors(o.Mutants) {
		fmt.Fprintf(&out, "  survived  %s:%d:%d  %s\n", m.File, m.Line, m.Column, Change(m.Mutator))
	}
	return out.String()
}

func Change(mutator string) string {
	if known, ok := changes[mutator]; ok {
		return known
	}
	return mutator
}
