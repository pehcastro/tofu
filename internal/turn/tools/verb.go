package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"

	"boji/internal/konst"
	"boji/internal/llm"
	"boji/internal/turn"
)

const verbDepthEnvar = "BOJI_VERB_DEPTH"

type verbParam int

const (
	verbParamPath verbParam = iota
	verbParamBody
)

func (p verbParam) name() string {
	switch p {
	case verbParamPath:
		return "path"
	case verbParamBody:
		return "body"
	}
	panic("tools: unknown verb parameter")
}

type verbSpec struct {
	tool   string
	words  []string
	about  string
	params []verbParam
}

func verbSpecs() []verbSpec {
	return []verbSpec{
		{
			tool:  "boji_lint_comments",
			words: []string{"lint", "comments"},
			about: "runs boji's own comment rule over the tree with the go parser, and prints every comment it found as path:line:column: text. " +
				"this project allows no comment of any kind, not a line comment and not a documentation comment, so any output at all is a list of things to delete. " +
				"path is optional and narrows the check to one file or directory relative to the working directory. " +
				"a nonzero exit means comments were found, which is an answer rather than a failure",
			params: []verbParam{verbParamPath},
		},
		{
			tool:  "boji_rules_check",
			words: []string{"rules", "check"},
			about: "runs boji's rule catalog over the tree and prints every rule that fired, which of them blocked, and how many findings each had. " +
				"path is optional and narrows the check to one file or directory relative to the working directory. " +
				"a nonzero exit means a rule in blocking mode fired",
			params: []verbParam{verbParamPath},
		},
		{
			tool:  "boji_judge",
			words: []string{"judge"},
			about: "asks boji's typed question battery about a state and prints the answer of each question with its distribution and the verdict the policy reached. " +
				"body is the request as json, an object carrying state and questions, and it is required. " +
				"use this to get a calibrated answer about the state of the work rather than guessing at one in prose",
			params: []verbParam{verbParamBody},
		},
	}
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
		properties[param.name()] = map[string]any{"type": "string"}
		if param == verbParamBody {
			required = append(required, param.name())
		}
	}
	return llm.Tool{
		Name:        v.spec.tool,
		Description: v.spec.about,
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
	words := slices.Clone(v.spec.words)
	body := ""
	for _, param := range v.spec.params {
		value := strings.TrimSpace(args[param.name()])
		delete(args, param.name())
		switch param {
		case verbParamPath:
			if value == "" {
				continue
			}
			if _, err := v.root.Resolve(value); err != nil {
				return turn.Result{}, fmt.Errorf("%s: %w", v.spec.tool, err)
			}
			words = append(words, value)
		case verbParamBody:
			if value == "" {
				return turn.Result{}, fmt.Errorf("%s: body is required and it is the json request the verb reads", v.spec.tool)
			}
			body = value
		}
	}
	if len(args) > 0 {
		return turn.Result{}, fmt.Errorf("%s: %s is none of its arguments", v.spec.tool, strings.Join(slices.Sorted(maps.Keys(args)), ", "))
	}

	return v.spawn(ctx, words, body)
}

func (v Verb) spawn(ctx context.Context, words []string, body string) (turn.Result, error) {
	depth, err := verbDepth()
	if err != nil {
		return turn.Result{}, fmt.Errorf("%s: %w", v.spec.tool, err)
	}
	if depth >= konst.VerbMaxDepth {
		return turn.Result{}, fmt.Errorf("%s: this boji is already nested %d deep and the bound is %d: a further nested run is refused",
			v.spec.tool, depth, konst.VerbMaxDepth)
	}
	running, err := os.Executable()
	if err != nil {
		return turn.Result{}, fmt.Errorf("%s: finding the running boji: %w", v.spec.tool, err)
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
	command := "boji " + strings.Join(words, " ")
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
