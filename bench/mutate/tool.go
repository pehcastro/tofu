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
