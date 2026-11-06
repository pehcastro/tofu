package eval

import (
	"bytes"
	"cmp"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/internal/konst"
	"tofu/internal/models/llm/core"
	"tofu/internal/platform/scratch"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/scratch/overlay"
	"tofu/internal/tools/shell/process"
)

const Name = "eval"

type Language string

const (
	Go     Language = "go"
	TS     Language = "ts"
	Python Language = "python"
	Rust   Language = "rust"
)

type Limits struct {
	Run          time.Duration
	StaleOverlay time.Duration
	StdoutLines  int
	ValueBytes   int
}

type Eval struct {
	root   contract.Root
	limits Limits
}

func New(dir string, limits Limits) (contract.Tool, error) {
	root, err := contract.NewRoot(dir)
	return Eval{root: root, limits: limits}, err
}

func (Eval) Name() string { return Name }

func (Eval) Definition() core.Tool {
	text := map[string]any{"type": "string"}
	return core.Tool{
		Name: Name,
		Description: "answers what an expression returns in this project, without writing a file or a main by hand: returns its value, its type, what it printed and how long it took, and the working tree never changes. " +
			"lang go builds a temporary main beside the package at path through go's -overlay, so the expression names the package, as orders.Order{Quantity: 3}.Total(); standard packages it names are imported, and imports adds others. " +
			"lang ts imports the file at path with tsx or node's type stripping and puts its exports in scope, as new Order(3, 250).total(); a promise is awaited. " +
			"lang python imports the file at path with the project's python and puts its names in scope, as Order(3, 250).total(). " +
			"lang rust builds a temporary binary in a copy of the crate with the library's items in scope, and the value needs Debug. " +
			"setup holds statements run before the expression, as x := \"a b\". a compile or syntax error comes back with expression:line:column or setup:line:column",
		Parameters: map[string]any{"type": "object", "required": []string{"lang", "expr"}, "properties": map[string]any{
			"lang":    map[string]any{"type": "string", "enum": []string{string(Go), string(TS), string(Python), string(Rust)}},
			"expr":    text,
			"path":    text,
			"setup":   text,
			"imports": map[string]any{"type": "array", "items": text},
		}},
	}
}

type request struct {
	Lang    Language `json:"lang"`
	Expr    string   `json:"expr"`
	Path    string   `json:"path"`
	Setup   string   `json:"setup"`
	Imports []string `json:"imports"`
}

type program struct {
	argv   []string
	dir    string
	env    []string
	files  map[string]string
	script string
	runner string
	inTree bool
	source generated
}

type generated struct {
	file       *regexp.Regexp
	setupLine  int
	setupLines int
	exprLine   int
	exprLines  int
	exprColumn int
}

type returned struct {
	Value *string `json:"value"`
	Type  string  `json:"type"`
	Ms    float64 `json:"ms"`
	Error string  `json:"error"`
}

func (e Eval) Run(ctx context.Context, raw json.RawMessage) (contract.Result, error) {
	var req request
	if err := json.Unmarshal(raw, &req); err != nil {
		return contract.Result{}, fmt.Errorf("eval: arguments are not the expected shape: %w", err)
	}
	if strings.TrimSpace(req.Expr) == "" {
		return contract.Failure("eval: expr is required: the expression whose value you want, as \"expr\": \"Total(3, 250)\""), nil
	}
	from, err := e.root.Resolve(cmp.Or(req.Path, "."))
	if err != nil {
		return contract.Failure("eval: path " + err.Error()), nil
	}
	marker := "tofu-eval-" + rand.Text()
	root := string(e.root)
	var prog program
	switch req.Lang {
	case Go:
		prog, err = goProgram(ctx, root, from, req, marker)
	case Rust:
		prog, err = rustProgram(root, from, req, marker)
	case TS:
		prog, err = tsProgram(root, from, req)
	case Python:
		prog, err = pythonProgram(root, from, req)
	default:
		return contract.Failure(fmt.Sprintf("eval: lang %q is none of go, ts, python or rust", req.Lang)), nil
	}
	if err != nil {
		return contract.Failure("eval: " + err.Error()), nil
	}
	made, err := overlay.Make(root, prog.files, e.limits.StaleOverlay)
	if err != nil {
		return contract.Result{}, fmt.Errorf("eval: the overlay was not made: %w", err)
	}
	defer func() { _ = made.Remove() }()
	if prog.inTree {
		tree, err := made.Tree()
		if err != nil {
			return contract.Result{}, fmt.Errorf("eval: the copy of the project was not made: %w", err)
		}
		rel, _ := filepath.Rel(root, prog.dir)
		prog.dir = filepath.Join(tree, rel)
	}
	if prog.runner != "" {
		runner := filepath.Join(made.Dir, prog.runner)
		if err := os.WriteFile(runner, []byte(prog.script), 0o644); err != nil {
			return contract.Result{}, fmt.Errorf("eval: %w", err)
		}
		prog.argv = append(prog.argv, runner)
	}
	if req.Lang == Go {
		prog.argv = slices.Insert(prog.argv, 2, made.GoFlag())
	}
	prog.env = slices.Concat(os.Environ(), []string{"GOMAXPROCS=" + strconv.Itoa(konst.TestCPUs), "TOFU_EVAL_MARKER=" + marker}, prog.env, scratch.ScratchEnv(ctx))
	return e.execute(ctx, prog, marker, made), nil
}

func (e Eval) execute(ctx context.Context, prog program, marker string, made overlay.Overlay) contract.Result {
	ctx, cancel := context.WithTimeout(ctx, e.limits.Run)
	defer cancel()
	command := strings.Join(prog.argv, " ")
	cmd := exec.CommandContext(ctx, prog.argv[0], prog.argv[1:]...)
	cmd.Dir, cmd.Env, cmd.WaitDelay = prog.dir, prog.env, konst.BashWaitDelayMillis*time.Millisecond
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	started := time.Now()
	tracked, err := process.StartTracked(cmd)
	if err != nil {
		return contract.Failure(fmt.Sprintf("eval: %s did not start in %s: %v", prog.argv[0], prog.dir, err))
	}
	_ = cmd.Wait()
	tracked.Release()
	took := time.Since(started).Milliseconds()
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return contract.Failure(fmt.Sprintf("eval: %s ran past its %d ms limit and was stopped", command, e.limits.Run.Milliseconds()))
	case ctx.Err() != nil:
		return contract.Failure("eval: the call was cancelled while " + command + " ran")
	}
	ran := fmt.Sprintf("ran %s in %s, %d ms; the working tree was not changed", made.Local(command, prog.dir), prog.dir, took)
	clean := func(text string) string {
		return prog.source.located(made.Local(strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n")), prog.dir))
	}
	printed, answer, found := strings.Cut(clean(stdout.String()), marker)
	printed = strings.TrimRight(printed, "\n")
	var got returned
	if !found || json.Unmarshal([]byte(strings.TrimSpace(answer)), &got) != nil {
		said := strings.TrimSpace(clean(stderr.String()) + "\n" + printed)
		return contract.Failure(fmt.Sprintf("eval: the expression returned no value: %s exited %d%s\n%s", prog.argv[0], cmd.ProcessState.ExitCode(), e.tail(said), ran))
	}
	if got.Value == nil {
		return contract.Failure("eval: the expression raised an error\n" + clean(got.Error) + "\nstdout: " + e.tail(printed) + "\n" + ran)
	}
	value := *got.Value
	if len(value) > e.limits.ValueBytes {
		value = fmt.Sprintf("%s ... (%d bytes, the first %d shown)", value[:e.limits.ValueBytes], len(value), e.limits.ValueBytes)
	}
	return contract.Result{Content: fmt.Sprintf("value: %s\ntype: %s\nms: %.3f\nstdout: %s\n%s", value, got.Type, got.Ms, e.tail(printed), ran)}
}

func (e Eval) tail(text string) string {
	if text == "" {
		return "none"
	}
	lines := strings.Split(text, "\n")
	if len(lines) <= e.limits.StdoutLines {
		return "\n" + text
	}
	return fmt.Sprintf("\n(%d earlier lines not shown)\n%s", len(lines)-e.limits.StdoutLines, strings.Join(lines[len(lines)-e.limits.StdoutLines:], "\n"))
}

func (g generated) located(text string) string {
	if g.file == nil {
		return text
	}
	return g.file.ReplaceAllStringFunc(text, func(found string) string {
		parts := g.file.FindStringSubmatch(found)
		line, _ := strconv.Atoi(parts[1])
		column, _ := strconv.Atoi(parts[2])
		switch {
		case line >= g.exprLine && line < g.exprLine+g.exprLines:
			if line == g.exprLine {
				column -= g.exprColumn - 1
			}
			return fmt.Sprintf("expression:%d:%d", line-g.exprLine+1, column)
		case line >= g.setupLine && line < g.setupLine+g.setupLines:
			return fmt.Sprintf("setup:%d:%d", line-g.setupLine+1, column)
		}
		return fmt.Sprintf("the generated main:%d:%d", line, column)
	})
}
