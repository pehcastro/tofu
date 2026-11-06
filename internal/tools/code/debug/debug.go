package debug

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"tofu/internal/models/llm/core"
	"tofu/internal/tools/code/dap"
	"tofu/internal/tools/contract"
)

type Limits struct {
	Request     time.Duration
	Run         time.Duration
	Stop        time.Duration
	Idle        time.Duration
	Frames      int
	Locals      int
	ValueRunes  int
	OutputLines int
}

type Debug struct {
	root    contract.Root
	limits  Limits
	mutex   sync.Mutex
	session *session
	idle    *time.Timer
	retired string
}

type language struct {
	name    string
	adapter dap.Adapter
	launch  func(root string, test bool, words []string) map[string]any
}

func delve() language {
	return language{name: "go", adapter: dap.Adapter{Program: "dlv", Args: []string{"dap"}, Install: "go install github.com/go-delve/delve/cmd/dlv@latest", Listens: true},
		launch: func(root string, test bool, words []string) map[string]any {
			if test {
				launch := map[string]any{"mode": "test", "program": root, "outputMode": "remote"}
				if len(words) > 0 {
					launch["args"] = []string{"-test.run", "^(" + strings.Join(words, "|") + ")$"}
				}
				return launch
			}
			return map[string]any{"mode": "debug", "program": filepath.Join(root, words[0]), "args": words[1:], "cwd": root, "outputMode": "remote"}
		}}
}

func debugpy() language {
	return language{name: "python", adapter: dap.Adapter{Program: "python", Args: []string{"-m", "debugpy.adapter"}, Install: "python -m pip install debugpy", Probe: []string{"-c", "import debugpy"}},
		launch: func(root string, test bool, words []string) map[string]any {
			launch := map[string]any{"cwd": root, "console": "internalConsole", "justMyCode": true}
			if test {
				launch["module"], launch["args"] = "unittest", words
				return launch
			}
			launch["program"], launch["args"] = filepath.Join(root, words[0]), words[1:]
			return launch
		}}
}

func New(dir string, limits Limits) (contract.Tool, error) {
	root, err := contract.NewRoot(dir)
	return &Debug{root: root, limits: limits}, err
}

func (*Debug) Name() string { return "debug" }

func (*Debug) Definition() core.Tool {
	return core.Tool{
		Name: "debug",
		Description: "runs a go or python program or test under a real debugger and answers what the values are at a line, instead of adding print lines you then have to remove. " +
			"steps run in order, and the session stays open between calls until stop. each step is one of: " +
			"start test [names...] debugs the tests of the working directory, all of them or the ones named (go test names, or python unittest ids such as test_order.OrderTest.test_discounted); " +
			"start <path> [args...] debugs a program, a go package directory or a python file; " +
			"break <path:line> [--if <expression>] stops there, only when the expression is true; " +
			"run starts or continues until a break or the end; step, step in and step out move one line; " +
			"locals prints the values at the stop; eval <expression> prints one value; stop ends the program and the debugger. " +
			"trace <package.Function> instead of break and run runs a go program to its end and lists each call of that function, such as orders.Order.Total, with its arguments, result, milliseconds and caller, the first " + strconv.Itoa(traceCalls) + " in full and the count of all of them. " +
			"every stop prints where it is, the locals one line each and the top of the stack. " +
			"go needs dlv and python needs debugpy; tofu never installs either and names the install command when one is missing",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"steps": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			},
			"required": []string{"steps"},
		},
	}
}

func (d *Debug) Run(ctx context.Context, raw json.RawMessage) (contract.Result, error) {
	var args struct {
		Steps []string `json:"steps"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return contract.Result{}, fmt.Errorf("debug: arguments are not the expected shape: %w", err)
	}
	if len(args.Steps) == 0 {
		return contract.Failure("debug: steps is empty: start with start test or start <path>"), nil
	}
	d.mutex.Lock()
	defer d.mutex.Unlock()
	if d.idle != nil {
		d.idle.Stop()
	}
	defer d.retireWhenIdle()
	var said strings.Builder
	said.WriteString(d.retired)
	d.retired = ""
	for _, step := range args.Steps {
		said.WriteString("> " + step + "\n")
		answer, err := d.step(ctx, strings.TrimSpace(step))
		said.WriteString(answer)
		if err != nil {
			return contract.Failure("debug: " + step + ": " + err.Error() + "\n" + said.String()), nil
		}
	}
	return contract.Result{Content: said.String()}, nil
}

func (d *Debug) retireWhenIdle() {
	open := d.session
	if open == nil {
		return
	}
	d.idle = time.AfterFunc(d.limits.Idle, func() {
		d.mutex.Lock()
		defer d.mutex.Unlock()
		if d.session == open {
			d.session = nil
			d.retired = "the open session was stopped after " + d.limits.Idle.String() + " unused: " + open.stop()
		}
	})
}

func (d *Debug) step(ctx context.Context, step string) (string, error) {
	verb, rest, _ := strings.Cut(step, " ")
	rest = strings.TrimSpace(rest)
	if verb == "start" {
		return d.start(ctx, rest)
	}
	if d.session == nil {
		return "", errors.New("no debug session is open: the first step is start test or start <path>")
	}
	s := d.session
	switch verb {
	case "break":
		return s.breakAt(ctx, rest)
	case "run":
		return s.run(ctx)
	case "step":
		command, known := map[string]string{"": "next", "in": "stepIn", "out": "stepOut"}[rest]
		if !known {
			return "", errors.New("step takes nothing, in or out")
		}
		return s.move(ctx, command)
	case "locals":
		return s.locals(ctx)
	case "eval":
		return s.eval(ctx, rest)
	case "trace":
		return s.trace(ctx, rest)
	case "stop":
		d.session = nil
		return s.stop(), nil
	}
	return "", errors.New("unknown step " + strconv.Quote(verb) + ": the steps are start, break, run, step, locals, eval, trace and stop")
}

func (d *Debug) start(ctx context.Context, target string) (string, error) {
	words := strings.Fields(target)
	if len(words) == 0 {
		return "", errors.New("start needs test or a path")
	}
	said := ""
	if d.session != nil {
		said = "the open session was stopped first: " + d.session.stop()
		d.session = nil
	}
	test := words[0] == "test"
	if test {
		words = words[1:]
	} else if _, err := d.root.Resolve(words[0]); err != nil {
		return said, err
	}
	chosen, err := d.choose(test, words)
	if err != nil {
		return said, err
	}
	s, err := open(ctx, d.limits, string(d.root), chosen, chosen.launch(string(d.root), test, words))
	if err != nil {
		return said, err
	}
	d.session = s
	return said + fmt.Sprintf("%s started under %s, pid %d: break where it should stop, then run\n", target, chosen.adapter.Program, s.client.Pid()), nil
}

func (d *Debug) choose(test bool, words []string) (language, error) {
	if test {
		if contract.IsFile(filepath.Join(string(d.root), "go.mod")) {
			return delve(), nil
		}
		if matches, _ := filepath.Glob(filepath.Join(string(d.root), "*.py")); len(matches) > 0 {
			return debugpy(), nil
		}
		return language{}, errors.New("start test debugs go or python tests, and the working directory has neither a go.mod nor a .py file")
	}
	if strings.HasSuffix(words[0], ".py") {
		return debugpy(), nil
	}
	if info, err := os.Stat(filepath.Join(string(d.root), words[0])); err == nil && (info.IsDir() || strings.HasSuffix(words[0], ".go")) {
		return delve(), nil
	}
	return language{}, errors.New(words[0] + " is neither a python file nor a go file or package directory")
}
