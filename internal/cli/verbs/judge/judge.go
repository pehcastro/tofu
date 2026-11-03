package judge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"tofu/internal/cli/output"
	"tofu/internal/cli/wiring"
	"tofu/internal/judge/gate"
	"tofu/internal/judge/ledger"
	"tofu/internal/judge/questions"
	"tofu/internal/models/classifiers"
	"tofu/internal/models/classifiers/core"
)

const Usage = "tofu judge [--dry-run] [--no-cache] [--no-rule] [--json] < request.json, or tofu judge --lint file [--json]"

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

func Verb(args []string, in io.Reader, out, errOut io.Writer) int {
	o := output.Verb{Name: "judge", UsageLine: Usage, AsJSON: output.JSONAsked(args), Out: out, ErrOut: errOut}
	opts, err := parseJudgeArgs(args)
	if err != nil {
		return o.Usage(err)
	}
	if opts.lintPath != "" {
		o.Name = "judge --lint"
		return judgeLint(o, opts.lintPath)
	}

	raw, err := io.ReadAll(in)
	if err != nil {
		return wiring.Failed(o, fmt.Errorf("reading standard input: %w", err))
	}
	var input judgeInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return wiring.Failed(o, fmt.Errorf("the request body is not valid JSON: %w", err))
	}
	if input.State == nil {
		return wiring.Failed(o, errors.New("the request carries no state"))
	}

	set, err := resolveQuestions(input)
	if err != nil {
		return wiring.Failed(o, err)
	}
	if !opts.noRule && input.Rule != "" {
		pol, err := ResolveRule(input.Rule, set)
		if err != nil {
			return wiring.Failed(o, err)
		}
		res, err := ResolveRuleMode(pol)
		if err != nil {
			return wiring.Failed(o, err)
		}
		set.Rule = &res.Rule
		set.Mode = res.Mode
		set.ModeReason = res.Reason
	}
	jevRequest := jev.Request{State: input.State, Questions: set.Questions}

	if opts.dryRun {
		bound, err := wiring.BoundClassifier()
		if err != nil {
			return wiring.Failed(o, err)
		}
		model, err := classifier.Route{Model: bound}.Alias()
		if err != nil {
			return wiring.Failed(o, err)
		}
		body, err := jevRequest.Encode(model)
		if err != nil {
			return wiring.Failed(o, err)
		}
		return output.Protocol(o, output.ExitOK, json.RawMessage(body), string(body)+"\n")
	}

	client, err := wiring.NewJevClient()
	if err != nil {
		return wiring.Failed(o, err)
	}
	outcome, err := RunJudge(context.Background(), client, jevRequest, set, opts.noCache)
	if err != nil {
		return wiring.Failed(o, err)
	}
	answers := outcome.output(set.Kinds)
	body, err := json.Marshal(answers)
	if err != nil {
		return wiring.Failed(o, err)
	}
	return output.Protocol(o, judgeExitCode(outcome), judgeReport{answers, outcome.verdict, outcome.mode}, string(body)+"\n")
}

func judgeExitCode(outcome Outcome) int {
	if outcome.verdict == ledger.VerdictUnset {
		return output.ExitOK
	}
	if gate.ExitCode(outcome.mode, gate.VerdictOf(outcome.verdict)) == 1 {
		return output.ExitVerdict
	}
	return output.ExitOK
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
		case output.JSONFlag:
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

func judgeLint(o output.Verb, path string) int {
	findings, err := question.LintFile(path, question.DefaultCaps())
	if err != nil {
		return wiring.Failed(o, err)
	}
	text := ""
	listed := make([]questionFinding, len(findings))
	for i, f := range findings {
		text += f.String() + "\n"
		listed[i] = questionFinding{File: f.File, Line: f.Line, Set: f.Set, Question: f.Question, Rule: f.Rule, Detail: f.Detail}
	}
	code := output.ExitOK
	if len(findings) > 0 {
		code = output.ExitVerdict
	}
	return output.Protocol(o, code, struct {
		Findings []questionFinding `json:"findings"`
	}{listed}, text)
}
