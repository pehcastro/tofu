package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"tofu/internal/judge/gate"
	"tofu/internal/judge/jev"
	"tofu/internal/judge/jev/wire/openrouter"
	"tofu/internal/judge/ledger"
	"tofu/internal/judge/question"
)

type judgeInput struct {
	State     any                       `json:"state"`
	Questions map[string]inlineQuestion `json:"questions"`
	Library   string                    `json:"library"`
	Rule      string                    `json:"rule"`
}

type judgeOpts struct {
	dryRun  bool
	noCache bool
	noRule  bool
}

func judgeVerb(args []string, in io.Reader, out, errOut io.Writer) int {
	opts, lintPath, err := parseJudgeArgs(args)
	if err != nil {
		return judgeFail(errOut, err)
	}
	if lintPath != "" {
		return judgeLint(lintPath, out, errOut)
	}

	raw, err := io.ReadAll(in)
	if err != nil {
		return judgeFail(errOut, fmt.Errorf("reading standard input: %w", err))
	}
	var input judgeInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return judgeFail(errOut, fmt.Errorf("the request body is not valid JSON: %w", err))
	}
	if input.State == nil {
		return judgeFail(errOut, errors.New("the request carries no state"))
	}

	set, err := resolveQuestions(input)
	if err != nil {
		return judgeFail(errOut, err)
	}
	if !opts.noRule && input.Rule != "" {
		pol, err := resolveRule(input.Rule, set)
		if err != nil {
			return judgeFail(errOut, err)
		}
		res, err := resolveRuleMode(pol)
		if err != nil {
			return judgeFail(errOut, err)
		}
		set.Rule = &res.Rule
		set.Mode = res.Mode
		set.ModeReason = res.Reason
	}
	jevRequest := jev.Request{State: input.State, Questions: set.Questions}

	if opts.dryRun {
		body, err := jevRequest.Encode(openrouter.Alias)
		if err != nil {
			return judgeFail(errOut, err)
		}
		_, _ = fmt.Fprintln(out, string(body))
		return exitOK
	}

	key, err := gateKey()
	if err != nil {
		return judgeFail(errOut, err)
	}
	client, err := jevClientOn(key, oneCallAtATime)
	if err != nil {
		return judgeFail(errOut, err)
	}

	outcome, err := runJudge(context.Background(), client, jevRequest, set, opts.noCache)
	if err != nil {
		return judgeFail(errOut, err)
	}

	body, err := json.Marshal(outcome.output(set.Kinds))
	if err != nil {
		return judgeFail(errOut, err)
	}
	_, _ = fmt.Fprintln(out, string(body))
	return judgeExitCode(outcome)
}

func judgeExitCode(outcome judgeOutcome) int {
	if outcome.verdict == ledger.VerdictUnset {
		return exitOK
	}
	if gate.ExitCode(outcome.mode, gate.VerdictOf(outcome.verdict)) == 1 {
		return exitVerdict
	}
	return exitOK
}

func judgeFail(errOut io.Writer, err error) int {
	_, _ = fmt.Fprintf(errOut, "tofu judge: %v\n", err)
	return exitUsage
}

func parseJudgeArgs(args []string) (judgeOpts, string, error) {
	var opts judgeOpts
	lintPath := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--dry-run":
			opts.dryRun = true
		case "--no-cache":
			opts.noCache = true
		case "--no-rule":
			opts.noRule = true
		case "--lint":
			i++
			if i >= len(args) {
				return judgeOpts{}, "", errors.New("--lint needs a path")
			}
			lintPath = args[i]
		default:
			return judgeOpts{}, "", fmt.Errorf("unknown argument %q", args[i])
		}
	}
	return opts, lintPath, nil
}

func judgeLint(path string, out, errOut io.Writer) int {
	findings, err := question.LintFile(path, question.DefaultCaps())
	if err != nil {
		return judgeFail(errOut, err)
	}
	for _, finding := range findings {
		_, _ = fmt.Fprintln(out, finding.String())
	}
	if len(findings) > 0 {
		return exitVerdict
	}
	return exitOK
}
