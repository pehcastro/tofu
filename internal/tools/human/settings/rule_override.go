package settings

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"tofu/internal/agent/loop"
	"tofu/internal/judge/ledger"
	"tofu/internal/librarian/rules"
	"tofu/internal/models/llm/core"
	platformfs "tofu/internal/platform/fs"
	"tofu/internal/tools/contract"
)

type RuleOverride struct {
	Ask             loop.Person
	Running         func() ([]rules.Rule, error)
	Project, Global string
}

func (RuleOverride) Name() string { return loop.RuleOverrideToolName }

func (RuleOverride) Definition() core.Tool {
	return core.Tool{
		Name: loop.RuleOverrideToolName,
		Description: "asks the person to override one rule that stops you from doing what they asked. the person answers yes for this project, yes everywhere, or no, and nothing is written before a yes. " +
			"on a yes the override is a file in their rules, marked by: asked, and the rule changes from the next turn. on a no, work within the rule",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"rule":             map[string]any{"type": "string", "description": "the id of the rule that blocks you"},
				"reason_from_rule": map[string]any{"type": "string", "description": "why the rule exists, from its note"},
				"why_now":          map[string]any{"type": "string", "description": "what the person asked that the rule blocks, in one line"},
				"change":           map[string]any{"type": "string", "description": "off, or the text that replaces what the rule says"},
			},
			"required": []string{"rule", "why_now", "change"},
		},
	}
}

func (o RuleOverride) Run(ctx context.Context, raw json.RawMessage) (contract.Result, error) {
	var args struct {
		Rule           string `json:"rule"`
		ReasonFromRule string `json:"reason_from_rule"`
		WhyNow         string `json:"why_now"`
		Change         string `json:"change"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return contract.Result{}, fmt.Errorf("rule_override: arguments are not the expected shape: %w", err)
	}
	if strings.TrimSpace(args.Change) == "" {
		return contract.Result{}, errors.New("rule_override: change is off, or the text that replaces the rule")
	}
	running, err := o.Running()
	if err != nil {
		return contract.Result{}, fmt.Errorf("rule_override: %w", err)
	}
	at := slices.IndexFunc(running, func(r rules.Rule) bool { return r.ID == args.Rule })
	if at < 0 {
		return contract.Result{}, fmt.Errorf("rule_override: no rule %q runs, so there is nothing to override", args.Rule)
	}
	base := running[at]
	text := ""
	if args.Change != string(rules.ModeOff) {
		text = args.Change
	}
	data, err := rules.OverrideFile(base.ID, text, rules.Override{Of: base.ID, Version: base.Version, Reason: args.WhyNow, By: rules.ByAsked, At: time.Now().Format(time.DateOnly)})
	if err != nil {
		return contract.Result{}, fmt.Errorf("rule_override: %w", err)
	}
	within := "work within " + base.ID + ", and tell the person it is what stopped you."
	if o.Ask == nil {
		return contract.Result{Content: "no person is here to answer, so nothing was written. " + within}, nil
	}
	question := "A rule stops me: " + base.ID + ".\nIts reason: " + cmp.Or(base.Notes, args.ReasonFromRule, base.Text) + "\nWhy now: " + args.WhyNow + "\nOverride it?"
	shown, err := json.Marshal(map[string]string{"rule": base.ID, "change": args.Change, "question": question})
	if err != nil {
		return contract.Result{}, err
	}
	answer, err := o.Ask(ctx, loop.GateRequest{Tool: loop.RuleOverrideToolName, Args: shown}, loop.GateDecision{Verdict: ledger.VerdictAsk})
	if err != nil {
		return contract.Result{Content: "the person could not be asked, so nothing was written: " + err.Error() + ". " + within}, nil
	}
	var dir, where string
	switch answer {
	case loop.PersonDenied:
		return contract.Result{Content: "the person said no, and nothing was written. " + within}, nil
	case loop.PersonAllowedOnce:
		dir, where = o.Project, "this project"
	case loop.PersonAlwaysHere:
		dir, where = o.Global, "every project"
	default:
		panic("tools: unknown person answer")
	}
	file := filepath.Join(dir, base.ID+"@1.yaml")
	switch _, err := os.Stat(file); {
	case err == nil:
		return contract.Result{}, fmt.Errorf("rule_override: %s is there already, and tofu rules restore removes it first", file)
	case !errors.Is(err, fs.ErrNotExist):
		return contract.Result{}, fmt.Errorf("rule_override: %w", err)
	}
	if err := platformfs.WriteFile(file, data, 0o644); err != nil {
		return contract.Result{}, fmt.Errorf("rule_override: %w", err)
	}
	return contract.Result{Content: "the person said yes for " + where + ", and " + file + " now overrides " + base.ID + " with by: asked. go ahead with what they asked; the prompt carries the change from the next turn."}, nil
}
