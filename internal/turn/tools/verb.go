package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/search"
	"tofu/internal/turn"
	"tofu/library/docs"
)

const (
	verbDepthEnvar = "TOFU_VERB_DEPTH"
	DocsToolName   = "tofu_docs"
)

type verbParam int

const (
	verbParamPath verbParam = iota
	verbParamState
	verbParamBattery
	verbParamID
	verbParamPoint
	verbParamSet
	verbParamTopic
)

func (p verbParam) about() (name string, describe string, required bool) {
	switch p {
	case verbParamPath:
		return "path", "one file or directory to narrow the check to, relative to the working directory. leave it out to check the whole tree", false
	case verbParamState:
		return "state", "what you want judged, as plain prose or as a json object. describe the work and its evidence, for instance the commands you ran and what they printed", true
	case verbParamBattery:
		return "battery", "the name of the question set to ask, either stop_check@1 for whether the work is finished or tool_gate@1 for whether an action is safe to take", true
	case verbParamID:
		return "id", "the id of one decision that was already recorded, exactly as it appears in the ledger row or in the message that blocked you", true
	case verbParamPoint:
		return "point", "the decision point whose recorded rows to rescore, for instance tool_gate or stop_check", true
	case verbParamSet:
		return "set", "the thresholds to move, each one as name=value, and more than one separated by commas, for instance risk_ask_at=1.2,risk_deny_at=2.8. leave it out to rescore against the thresholds that were recorded", false
	case verbParamTopic:
		return "topic", "one topic, or a few words to match against the asks and the pages. leave it out for the index", false
	}
	panic("tools: unknown verb parameter")
}

type verbSpec struct {
	tool   string
	words  []string
	about  string
	needs  string
	params []verbParam
}

func verbSpecs() []verbSpec {
	return []verbSpec{
		{
			tool:  "tofu_lint_comments",
			words: []string{"lint", "comments"},
			about: "runs tofu's own comment rule over the tree with the go parser, and prints every comment it found as path:line:column: text. " +
				"this project allows no comment of any kind, not a line comment and not a documentation comment, so any output at all is a list of things to delete. " +
				"a nonzero exit means comments were found, which is an answer rather than a failure",
			needs:  "it needs nothing beyond the working tree, and it reads only go source",
			params: []verbParam{verbParamPath},
		},
		{
			tool:  "tofu_rules_check",
			words: []string{"rules", "check"},
			about: "runs tofu's rule library over the tree and prints every rule that fired, which of them blocked, and how many findings each had. " +
				"the first line is the answer: how many fires blocked, out of how many, and which rule set ran. " +
				"a nonzero exit means a rule in blocking mode fired",
			needs:  "it needs nothing beyond the working tree: the rule library travels inside the binary, and a library/rules directory in the working tree replaces it",
			params: []verbParam{verbParamPath},
		},
		{
			tool:  "tofu_judge",
			words: []string{"judge"},
			about: "asks a typed question battery about a state and prints, for every question, the answer with its distribution, and the verdict the policy reached. " +
				"use it to get a calibrated answer about the state of the work rather than guessing at one in prose",
			needs:  "it needs the openrouter key, as OPENROUTER_KEY in the environment or in a .env file at the root of the working tree; without it the verb exits 2 and says so",
			params: []verbParam{verbParamState, verbParamBattery},
		},
		{
			tool:  "tofu_why",
			words: []string{"why"},
			about: "prints the chain behind one decision that was already recorded: every question with its answer and its whole distribution, the verdict the policy reached, the threshold each answer was compared against, and a line naming any question that landed in the dead band. " +
				"reach for it when the gate has already allowed, asked about or denied something and you want the reason that was recorded rather than a fresh guess at it. " +
				"a nonzero exit means no row carries that id",
			needs:  "it needs the .tofu ledger in the working tree and nothing else: it reads the record, so it makes no network call, spends nothing and needs no key",
			params: []verbParam{verbParamID},
		},
		{
			tool:  "tofu_replay",
			words: []string{"replay"},
			about: "rescores every decision recorded at one point against thresholds you move, and prints how many rows were read, how many were rescored, how many verdicts changed, and for each change how many recorded outcomes now agree and how many now disagree. " +
				"reach for it before proposing a threshold, so the proposal carries what that number would have done to decisions that were really made. " +
				"a nonzero exit means the point or a threshold name was not one the policy declares, and the message lists the names it takes",
			needs:  "it needs the .tofu ledger in the working tree: it rescores the answers that were recorded, so it makes no network call, spends nothing and needs no key",
			params: []verbParam{verbParamPoint, verbParamSet},
		},
		{
			tool:  DocsToolName,
			words: []string{"docs"},
			about: "prints tofu's own documentation: with no topic an index of asks and the tofu command that does each, with a topic that page, with a few words the asks and pages that match. " +
				"the topics are " + docsTopics() + ". " +
				"call it before changing tofu's settings, rules, sub-agents or models",
			needs:  "it needs nothing: the docs travel inside the binary, so it makes no network call",
			params: []verbParam{verbParamTopic},
		},
	}
}

func docsTopics() string {
	pages, _ := fs.Glob(docs.Files(), "*.md")
	return strings.ReplaceAll(strings.Join(pages, ", "), ".md", "")
}

type judgeRequest struct {
	State   json.RawMessage `json:"state"`
	Library string          `json:"library"`
}

func judgeBody(state, battery string) (string, error) {
	value := json.RawMessage(state)
	if !strings.HasPrefix(state, "{") || !json.Valid(value) {
		quoted, err := json.Marshal(state)
		if err != nil {
			return "", err
		}
		value = quoted
	}
	body, err := json.Marshal(judgeRequest{State: value, Library: battery})
	return string(body), err
}

type Verb struct {
	root turn.Root
	spec verbSpec
}

func NewVerbs(dir string) ([]turn.Tool, error) {
	root, err := turn.NewRoot(dir)
	if err != nil {
		return nil, err
	}
	specs := verbSpecs()
	verbs := make([]turn.Tool, len(specs))
	for i, spec := range specs {
		verbs[i] = Verb{root: root, spec: spec}
	}
	return verbs, nil
}

func (v Verb) Name() string { return v.spec.tool }

func (v Verb) Definition() llm.Tool {
	properties := make(map[string]any, len(v.spec.params))
	var required []string
	for _, param := range v.spec.params {
		name, describe, must := param.about()
		properties[name] = map[string]any{"type": "string", "description": describe}
		if must {
			required = append(required, name)
		}
	}
	return llm.Tool{
		Name:        v.spec.tool,
		Description: v.spec.about + ". " + v.spec.needs,
		Parameters:  map[string]any{"type": "object", "properties": properties, "required": required},
	}
}

func (v Verb) Run(ctx context.Context, raw json.RawMessage) (turn.Result, error) {
	args := map[string]string{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return turn.Result{}, fmt.Errorf("%s: arguments are not the expected shape: %w", v.spec.tool, err)
		}
	}
	values := make(map[verbParam]string, len(v.spec.params))
	names := make([]string, len(v.spec.params))
	for i, param := range v.spec.params {
		names[i], _, _ = param.about()
		values[param] = strings.TrimSpace(args[names[i]])
		delete(args, names[i])
	}
	if len(args) > 0 {
		return turn.Result{}, fmt.Errorf("%s: it has no argument named %s: its arguments are %s",
			v.spec.tool, strings.Join(slices.Sorted(maps.Keys(args)), ", "), strings.Join(names, " and "))
	}

	words := slices.Clone(v.spec.words)
	state, battery := "", ""
	for _, param := range v.spec.params {
		name, describe, must := param.about()
		value := values[param]
		if value == "" {
			if must {
				return turn.Result{}, fmt.Errorf("%s: %s is required: %s", v.spec.tool, name, describe)
			}
			continue
		}
		switch param {
		case verbParamPath:
			if _, err := v.root.Resolve(value); err != nil {
				return turn.Result{}, fmt.Errorf("%s: %w", v.spec.tool, err)
			}
			words = append(words, value)
		case verbParamState:
			state = value
		case verbParamBattery:
			battery = value
		case verbParamID, verbParamTopic:
			words = append(words, value)
		case verbParamPoint:
			words = append(words, "--point", value)
		case verbParamSet:
			for _, setting := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ' ' }) {
				words = append(words, "--set", setting)
			}
		}
	}
	if state == "" {
		return v.spawn(ctx, words, "")
	}
	body, err := judgeBody(state, battery)
	if err != nil {
		return turn.Result{}, fmt.Errorf("%s: %w", v.spec.tool, err)
	}
	return v.spawn(ctx, words, body)
}

func (v Verb) spawn(ctx context.Context, words []string, body string) (turn.Result, error) {
	depth, err := verbDepth()
	if err != nil {
		return turn.Result{}, fmt.Errorf("%s: %w", v.spec.tool, err)
	}
	if depth >= konst.VerbMaxDepth {
		return turn.Result{}, fmt.Errorf("%s: this tofu is already nested %d deep and the bound is %d: a further nested run is refused",
			v.spec.tool, depth, konst.VerbMaxDepth)
	}
	running, err := os.Executable()
	if err != nil {
		return turn.Result{}, fmt.Errorf("%s: finding the running tofu: %w", v.spec.tool, err)
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(konst.VerbTimeoutMillis)*time.Millisecond)
	defer cancel()

	cmd := exec.CommandContext(ctx, running, words...)
	cmd.Dir = string(v.root)
	cmd.Env = append(os.Environ(), verbDepthEnvar+"="+strconv.Itoa(depth+1))
	if body != "" {
		cmd.Stdin = strings.NewReader(body)
	}

	output, runErr := cmd.CombinedOutput()
	command := "tofu " + strings.Join(words, " ")
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return turn.Result{}, errors.New(v.spec.tool + ": " + search.Note(search.Stopped,
			fmt.Sprintf("%s ran past the %d ms deadline and was killed", command, konst.VerbTimeoutMillis)))
	}
	if cmd.ProcessState == nil {
		return turn.Result{}, fmt.Errorf("%s: %s did not run: %w", v.spec.tool, command, runErr)
	}
	code := cmd.ProcessState.ExitCode()
	content := string(output)
	if strings.TrimSpace(content) == "" {
		content = fmt.Sprintf("%s exited %d and printed nothing", command, code)
	}
	return turn.Result{Content: content, Command: command, ExitCode: &code}, nil
}

func verbDepth() (int, error) {
	raw := os.Getenv(verbDepthEnvar)
	if raw == "" {
		return 0, nil
	}
	depth, err := strconv.Atoi(raw)
	if err != nil || depth < 0 {
		return 0, fmt.Errorf("%s is %q, which is not a nesting depth", verbDepthEnvar, raw)
	}
	return depth, nil
}
