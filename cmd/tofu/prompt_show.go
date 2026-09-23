package main

import (
	"cmp"
	"fmt"
	"io"
	"strings"
)

func showPrompt(opts runOpts, out, errOut io.Writer) int {
	environment, notice := runEnvironment(opts)
	if notice != "" {
		writeNotice(errOut)(notice)
	}
	composed, err := composePrompt(opts, environment)
	if err != nil {
		return runFail(errOut, err)
	}
	system := composed.System()
	firstUser := environment + "\n\n" + opts.task
	uncomposed := len(runSystem(opts))

	fired, widest := 0, 0
	for _, part := range composed.Parts {
		if part.RuleID != "" {
			fired++
		}
		widest = max(widest, len(part.Concern))
	}
	_, _ = fmt.Fprintf(out, "task: %s\npaths the task names: %s\nverb the task names: %s\nrules: %d fire, %d held back\n\n",
		opts.task,
		cmp.Or(strings.Join(composed.Task.Paths, ", "), "none, so a scope and a language reach nothing"),
		cmp.Or(string(composed.Task.Verb), "none, so a rule that names a task holds back"),
		fired, len(composed.HeldBack))

	for _, part := range composed.Parts {
		_, _ = fmt.Fprintf(out, "part %-*s %6d bytes  from %s\n", widest, part.Concern, len(part.Text), part.From())
	}
	for _, held := range composed.HeldBack {
		_, _ = fmt.Fprintf(out, "held %s: %s\n", held.RuleID, held.Why)
	}

	_, _ = fmt.Fprintf(out, "\nsystem message, %d bytes\n%s\n", len(system), system)
	_, _ = fmt.Fprintf(out, "\nfirst user message, %d bytes\n%s\n", len(firstUser), firstUser)
	_, _ = fmt.Fprintf(out, "\ncomposed system prompt %d bytes against %d uncomposed, %+d bytes\n",
		len(system), uncomposed, len(system)-uncomposed)
	return exitOK
}
