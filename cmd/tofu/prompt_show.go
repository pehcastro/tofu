package main

import (
	"cmp"
	"fmt"
	"io"
	"slices"
	"strings"

	"tofu/internal/llm"
	"tofu/internal/rule"
	settingspkg "tofu/internal/settings"
	"tofu/internal/turn"
)

func showPrompt(opts runOpts, out, errOut io.Writer) int {
	shell, err := turn.ResolveRunShell(settingText(opts.dir, settingspkg.Shell, writeNotice(errOut)))
	if err != nil {
		return runFail(errOut, err)
	}
	opts.shell = shell
	named, _, err := buildRunToolsForRun(opts.dir, opts.toolSet, readsWhen(opts.readBeforeEdit, turn.NewReadLedger()), nil, shell)
	if err != nil {
		return runFail(errOut, err)
	}
	prompt, err := composeRun(opts, named, runtime{notify: writeNotice(errOut), open: openAppWire})
	if err != nil {
		return runFail(errOut, err)
	}
	offered, err := offeredTools(opts, prompt, named)
	if err != nil {
		return runFail(errOut, err)
	}
	opts, instructions, composed := prompt.opts, prompt.instructions, prompt.composed
	sent := turn.Config{System: composed.Head(), Instructions: prompt.files, Memory: prompt.memory, Environment: composed.WithTaskRules(prompt.environment), Task: opts.task}
	system, firstUser := sent.SystemMessage(), sent.FirstUserMessage()
	uncomposed := len(runSystem(opts))

	shipsOff := func(part turn.PromptPart) bool {
		return slices.ContainsFunc(prompt.shippedOff, func(off rule.Rule) bool { return off.ID == part.RuleID })
	}
	held := slices.DeleteFunc(slices.Clone(composed.Unsent), func(part turn.PromptPart) bool { return part.RuleID == "" || shipsOff(part) })
	shippedOff := slices.DeleteFunc(slices.Clone(composed.Unsent), func(part turn.PromptPart) bool { return !shipsOff(part) })
	fired, off, widest := 0, len(shippedOff), 0
	for _, part := range composed.Parts {
		switch {
		case part.Off:
			off++
		case part.RuleID != "":
			fired++
		}
		widest = max(widest, len(part.Concern))
	}
	_, _ = fmt.Fprintf(out, "task: %s\ninstruction files: %s\npaths the task names: %s\nverb the task names: %s\nrules: %d loaded, %d fire, %d held back, %d switched off\n\n",
		opts.task, instructions,
		cmp.Or(strings.Join(composed.Task.Paths, ", "), "none, so a scope and a language reach nothing"),
		cmp.Or(string(composed.Task.Verb), "none, so a rule that names a task holds back"),
		fired+off+len(held), fired, len(held), off)

	for _, part := range composed.Parts {
		_, _ = fmt.Fprintf(out, "part %-*s %6d bytes  from %s\n", widest, part.Concern, len(part.Text), part.From())
	}
	for _, part := range held {
		_, _ = fmt.Fprintf(out, "held %s: %s\n", part.RuleID, part.Why)
	}
	for _, part := range shippedOff {
		_, _ = fmt.Fprintf(out, "off %s: %s\n", part.RuleID, part.Why)
	}
	for _, loaded := range prompt.subAgents.Prompt.Rules {
		if why := shadowCheck(loaded); why != "" {
			_, _ = fmt.Fprintf(out, "shadow %s: %s\n", loaded.ID, why)
		}
	}
	library, points := readLibrary()
	if library.Unreadable != "" {
		_, _ = fmt.Fprintf(out, "points unreadable: %s\n", library.Unreadable)
	}
	for _, point := range points {
		listed := pointListing(point)
		_, _ = fmt.Fprintf(out, "point %s: %s\n", listed.ID, strings.TrimSpace(listed.Mode+" "+listed.Switch))
	}

	_, _ = fmt.Fprintf(out, "\nsystem message, %d bytes\n%s\n", len(system), system)
	for _, view := range sent.MemoryMessage() {
		_, _ = fmt.Fprintf(out, "\nmemory, a user message before the first, %d bytes\n%s\n", len(view.Content), view.Content)
	}
	_, _ = fmt.Fprintf(out, "\nfirst user message, %d bytes\n%s\n", len(firstUser), firstUser)
	_, _ = fmt.Fprintf(out, "\noffered tools, %d, as the request sends them\n", len(offered))
	for _, tool := range offered {
		_, _ = fmt.Fprintf(out, "\ntool %s\n%s\n", tool.Name, tool.Description)
	}
	_, _ = fmt.Fprintf(out, "\ncomposed system prompt %d bytes against %d uncomposed, %+d bytes\n",
		len(sent.System), uncomposed, len(sent.System)-uncomposed)
	return exitOK
}

func offeredTools(opts runOpts, prompt composedRun, named []turn.Tool) ([]llm.Tool, error) {
	config, spawner, err := runConfig(opts, named, runtime{open: openAppWire, composed: &prompt})
	if err != nil || opts.agent == "" {
		return config.Tools.Definitions(), err
	}
	agent, err := prompt.subAgents.Named(opts.agent)
	if err != nil {
		return nil, err
	}
	return spawner.OfferedTo(agent, ""), nil
}
