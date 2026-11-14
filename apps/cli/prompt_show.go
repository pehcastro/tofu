package main

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"slices"
	"strings"

	"tofu/apps/cli/output"
	"tofu/apps/cli/wiring"
	"tofu/internal/agent/loop"
	"tofu/internal/librarian/rules"
	platformproject "tofu/internal/platform/project"
)

func showPrompt(ctx context.Context, opts wiring.RunOpts, out, errOut io.Writer) int {
	hand, err := handConfig(ctx, opts, "", nil, writeNotice(errOut))
	if err != nil {
		return runFail(errOut, err)
	}
	if hand.gateErr != nil {
		writeNotice(errOut)("a run would gate no tool call and send no settings tool: " + hand.gateErr.Error())
	}
	prompt, board, offered := hand.prompt, hand.board, hand.config.OfferedTools()
	opts, instructions, composed := prompt.opts, prompt.instructions, prompt.composed
	sent := loop.Config{System: composed.Head(), Instructions: prompt.files, Memory: prompt.memory, Environment: composed.WithTaskRules(prompt.environment) + board.attached, Task: opts.Task, Notes: []string{board.notified}}
	system, firstUser := sent.SystemMessage(), sent.FirstUserMessage()
	uncomposed := len(runSystem(opts))

	shipsOff := func(part loop.PromptPart) bool {
		return slices.ContainsFunc(prompt.shippedOff, func(off rules.Rule) bool { return off.ID == part.RuleID })
	}
	held := slices.DeleteFunc(slices.Clone(composed.Unsent), func(part loop.PromptPart) bool { return part.RuleID == "" || shipsOff(part) })
	shippedOff := slices.DeleteFunc(slices.Clone(composed.Unsent), func(part loop.PromptPart) bool { return !shipsOff(part) })
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
		opts.Task, instructions,
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
		if why := wiring.ShadowCheck(loaded); why != "" {
			_, _ = fmt.Fprintf(out, "shadow %s: %s\n", loaded.ID, why)
		}
	}
	for _, effect := range board.effects {
		_, _ = fmt.Fprintf(out, "board hook %s after lead.attach; show-prompt ran nothing\n", effect)
	}
	library, points := wiring.ReadLibrary(platformproject.ResolvePlaces("."))
	if library.Unreadable != "" {
		_, _ = fmt.Fprintf(out, "points unreadable: %s\n", library.Unreadable)
	}
	for _, point := range points {
		listed := wiring.PointListing(point)
		_, _ = fmt.Fprintf(out, "point %s: %s\n", listed.ID, strings.TrimSpace(listed.Mode+" "+listed.Switch))
	}

	_, _ = fmt.Fprintf(out, "\nsystem message, %d bytes\n%s\n", len(system), system)
	for _, view := range sent.MemoryMessage() {
		_, _ = fmt.Fprintf(out, "\nmemory, a user message before the first, %d bytes\n%s\n", len(view.Content), view.Content)
	}
	boardText := ""
	if board.attached != "" {
		boardText = fmt.Sprintf(", %d of them the board text lead.attach frames in", len(board.attached))
	}
	if board.notified != "" {
		boardText += fmt.Sprintf(", %d of them the board notifies the lead has not read", len(board.notified))
	}
	_, _ = fmt.Fprintf(out, "\nfirst user message, %d bytes%s\n%s\n", len(firstUser), boardText, firstUser)
	_, _ = fmt.Fprintf(out, "\noffered tools, %d, as the request sends them\n", len(offered))
	for _, tool := range offered {
		_, _ = fmt.Fprintf(out, "\ntool %s\n%s\n", tool.Name, tool.Description)
	}
	_, _ = fmt.Fprintf(out, "\ncomposed system prompt %d bytes against %d uncomposed, %+d bytes\n",
		len(sent.System), uncomposed, len(sent.System)-uncomposed)
	return output.ExitOK
}
