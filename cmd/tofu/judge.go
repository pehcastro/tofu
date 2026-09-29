package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"

	"tofu/interface/cli"
	"tofu/internal/judge/gate"
	"tofu/internal/judge/jev"
	"tofu/internal/judge/jev/wire/openrouter"
	"tofu/internal/judge/ledger"
	"tofu/internal/judge/question"
	"tofu/internal/transport"
)

const judgeUsage = "tofu judge [--dry-run] [--no-cache] [--no-rule] [--json] < request.json, or tofu judge --lint file [--json]"

type judgeInput struct {
	State     any                       `json:"state"`
	Questions map[string]inlineQuestion `json:"questions"`
	Library   string                    `json:"library"`
	Rule      string                    `json:"rule"`
}

type judgeOpts struct {
	dryRun   bool
	noCache  bool
	noRule   bool
	lintPath string
}

type judgeReport struct {
	Answers map[string]any `json:"answers"`
	Verdict ledger.Verdict `json:"verdict,omitempty"`
	Mode    gate.Mode      `json:"mode,omitempty"`
}

type questionFinding struct {
	File     string        `json:"file"`
	Line     int           `json:"line"`
	Set      string        `json:"set"`
	Question string        `json:"question,omitempty"`
	Rule     question.Rule `json:"rule"`
	Detail   string        `json:"detail"`
}

func judgeVerb(args []string, in io.Reader, out, errOut io.Writer) int {
	o := verbOutput{verb: "judge", usageLine: judgeUsage, asJSON: jsonAsked(args), out: out, errOut: errOut}
	opts, err := parseJudgeArgs(args)
	if err != nil {
		return o.usage(err)
	}
	if opts.lintPath != "" {
		o.verb = "judge --lint"
		return judgeLint(o, opts.lintPath)
	}

	raw, err := io.ReadAll(in)
	if err != nil {
		return failed(o, fmt.Errorf("reading standard input: %w", err))
	}
	var input judgeInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return failed(o, fmt.Errorf("the request body is not valid JSON: %w", err))
	}
	if input.State == nil {
		return failed(o, errors.New("the request carries no state"))
	}

	set, err := resolveQuestions(input)
	if err != nil {
		return failed(o, err)
	}
	if !opts.noRule && input.Rule != "" {
		pol, err := resolveRule(input.Rule, set)
		if err != nil {
			return failed(o, err)
		}
		res, err := resolveRuleMode(pol)
		if err != nil {
			return failed(o, err)
		}
		set.Rule = &res.Rule
		set.Mode = res.Mode
		set.ModeReason = res.Reason
	}
	jevRequest := jev.Request{State: input.State, Questions: set.Questions}

	if opts.dryRun {
		body, err := jevRequest.Encode(openrouter.Alias)
		if err != nil {
			return failed(o, err)
		}
		return protocol(o, exitOK, json.RawMessage(body), string(body)+"\n")
	}

	key, err := gateKey()
	if err != nil {
		return failed(o, err)
	}
	client, err := jevClientOn(key, oneCallAtATime)
	if err != nil {
		return failed(o, err)
	}
	outcome, err := runJudge(context.Background(), client, jevRequest, set, opts.noCache)
	if err != nil {
		return failed(o, err)
	}
	answers := outcome.output(set.Kinds)
	body, err := json.Marshal(answers)
	if err != nil {
		return failed(o, err)
	}
	return protocol(o, judgeExitCode(outcome), judgeReport{answers, outcome.verdict, outcome.mode}, string(body)+"\n")
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

func parseJudgeArgs(args []string) (judgeOpts, error) {
	var opts judgeOpts
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--dry-run":
			opts.dryRun = true
		case "--no-cache":
			opts.noCache = true
		case "--no-rule":
			opts.noRule = true
		case jsonFlag:
		case "--lint":
			i++
			if i >= len(args) {
				return judgeOpts{}, errors.New("--lint needs a path")
			}
			opts.lintPath = args[i]
		default:
			return judgeOpts{}, fmt.Errorf("unknown argument %q", args[i])
		}
	}
	return opts, nil
}

func judgeLint(o verbOutput, path string) int {
	findings, err := question.LintFile(path, question.DefaultCaps())
	if err != nil {
		return failed(o, err)
	}
	text := ""
	listed := make([]questionFinding, len(findings))
	for i, f := range findings {
		text += f.String() + "\n"
		listed[i] = questionFinding{File: f.File, Line: f.Line, Set: f.Set, Question: f.Question, Rule: f.Rule, Detail: f.Detail}
	}
	code := exitOK
	if len(findings) > 0 {
		code = exitVerdict
	}
	return protocol(o, code, struct {
		Findings []questionFinding `json:"findings"`
	}{listed}, text)
}

func protocol(o verbOutput, code int, data any, text string) int {
	var err error
	if o.asJSON {
		err = writeJSON(o.out, cli.Envelope{Verb: o.verb, OK: code == exitOK, At: time.Now(), Data: data})
	} else {
		_, err = io.WriteString(o.out, text)
	}
	if err != nil {
		return exitVerdict
	}
	return code
}

func failed(o verbOutput, err error) int {
	var refused *transport.Error
	switch {
	case !errors.As(err, &refused):
	case refused.Kind == transport.KindMissingCredential:
		err = problemError{What: refused.Detail, Hint: "tofu login " + openRouterName}
	case refused.Status == 0:
	case refused.Kind == transport.KindAuth:
		err = problemError{What: "jev refused the key (" + strconv.Itoa(refused.Status) + ")", Hint: "tofu login " + openRouterName}
	default:
		err = problemError{What: "jev refused the call (" + strconv.Itoa(refused.Status) + " " + refused.Kind.String() + ")"}
	}
	_ = o.fail(err)
	return exitUsage
}
