package main

import (
	"bytes"
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"tofu/internal/models/llm/catalog"
)

type side string

const (
	sideTool side = "tool"
	sideTurn side = "turn"
)

type arm struct {
	key    string
	name   string
	side   side
	briefs []string
}

type outcome struct {
	arm                               arm
	passed                            bool
	check, model, spend, failed, note string
	requests, in, out, changed        int
	spent                             time.Duration
}

type machine struct {
	exe, fixture, scratch, model, home string
	env                                []string
}

const (
	armNone        = "none, the untouched copy"
	armOneSmall    = "one sub-agent, approach one"
	armOneTiers    = "one sub-agent, approach two"
	armRace        = "tofu tool race, both approaches"
	armTofuTurn    = "tofu run, the lead given the task"
	armClaudeTurn  = "claude -p, the same task"
	claudeModel    = "claude-sonnet-5-5"
	tokenOutlives  = 45 * time.Minute
	sessionTimeout = 20 * time.Minute
	checkTimeout   = 3 * time.Minute
	restateWait    = "waiting for your go or your correction"
	personGo       = "go"
	briefSmall     = "In this Go module TestDiscounted in order_test.go fails: Order.Discounted takes a flat 100 cents off, and the discount is 10 percent of the total. Fix it in order.go alone: take 10 percent of the total, rounded down to whole cents, off the total. Add no package. Run go test . and stop when it passes."
	briefTiers     = "In this Go module TestDiscounted in order_test.go fails: Order.Discounted takes a flat 100 cents off, and the discount is 10 percent of the total. Fix it with a quantity tier table in the pricing package: pricing.Discounted(quantity, unitCents) takes the rate for the quantity from the table, 10 percent from one item, and Order.Discounted calls it. Add a test of the tiers in pricing. Run go test . ./pricing and stop when both pass."
	taskTurn       = "TestDiscounted fails: the discount is 10 percent of the total, not a flat 100 cents. Try two fixes for this bug, one confined to order.go and one that adds a quantity tier table to the pricing package, and keep the one that passes."
)

func arms() []arm {
	return []arm{
		{key: "none", name: armNone, side: sideTool},
		{key: "small", name: armOneSmall, side: sideTool, briefs: []string{briefSmall}},
		{key: "tiers", name: armOneTiers, side: sideTool, briefs: []string{briefTiers}},
		{key: "race", name: armRace, side: sideTool, briefs: []string{briefSmall, briefTiers}},
		{key: "tofu", name: armTofuTurn, side: sideTurn, briefs: []string{taskTurn}},
		{key: "claude", name: armClaudeTurn, side: sideTurn, briefs: []string{taskTurn}},
	}
}

func costs(model string) []string {
	return []string{
		armNone + ": no model call, go test only",
		armOneSmall + ": one tofu run --no-subagents on " + model + ", subscription windows, no money; the gate's Jev calls on the owner's classifier binding, under a tenth of a cent",
		armOneTiers + ": the same as approach one, a second session",
		armRace + ": two sub-agents at once on " + model + ", subscription windows, no money, about the two one sub-agent arms together less what the stopped run did not spend; Jev gate calls as above",
		armTofuTurn + ": one lead on " + model + " plus whatever it starts, race's two sub-agents if it calls race, subscription windows, no money; Jev gate calls as above",
		armClaudeTurn + ": one claude -p turn on " + claudeModel + " on the owner's Claude subscription, no money; total_cost_usd is the CLI's notional figure, not a charge",
	}
}

func main() {
	dev := flag.String("dev", "", "a tofu built from the current tree")
	fixture := flag.String("fixture", "bench/suites/tools/fixtures/go", "the go fixture, copied fresh for every arm")
	scratch := flag.String("scratch", "", "a directory each arm gets a fresh project copy and a fresh HOME under")
	model := flag.String("model", "claude-sub/"+claudeModel, "the model tofu's orchestrator and sub-agent roles are bound to in each fresh HOME")
	only := flag.String("arms", "", "a comma separated subset of none, small, tiers, race, tofu and claude, otherwise every arm")
	out := flag.String("out", "", "the report file, otherwise standard output")
	flag.Parse()
	if *dev == "" || *scratch == "" {
		fmt.Fprintln(os.Stderr, "usage: delegate -dev <tofu dev exe> -scratch <dir> [-model claude-sub/<model>] [-arms a,b] [-fixture <dir>] [-out <report.md>]\nthis suite needs the network and a claude-sub login in the owner's tofu home, and makes live subscription calls")
		os.Exit(2)
	}
	if !strings.HasPrefix(*model, "claude-sub/") {
		fmt.Fprintln(os.Stderr, "delegate: -model must be a claude-sub/ slug, since every arm runs on the subscription and never on a key")
		os.Exit(2)
	}
	began := time.Now()
	m, err := newMachine(*dev, *fixture, filepath.Join(*scratch, began.Format("20060102-150405")), *model)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, line := range costs(*model) {
		fmt.Fprintln(os.Stderr, "cost: "+line)
	}
	var rows []outcome
	for _, a := range arms() {
		if *only != "" && !slices.Contains(strings.Split(*only, ","), a.key) {
			continue
		}
		o := m.drive(a)
		rows = append(rows, o)
		fmt.Fprintf(os.Stderr, "%s: passed %v, %s\n", a.name, o.passed, cmp.Or(o.failed, "ran"))
	}
	report := render(rows, began, m)
	if *out == "" {
		fmt.Print(report)
		return
	}
	if err := os.WriteFile(*out, []byte(report), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newMachine(dev, fixture, scratch, model string) (machine, error) {
	owner, err := os.UserHomeDir()
	if err != nil {
		return machine{}, err
	}
	cache, err := exec.Command("go", "env", "GOCACHE").Output()
	if err != nil {
		return machine{}, fmt.Errorf("go env GOCACHE: %w", err)
	}
	env := slices.DeleteFunc(os.Environ(), func(kv string) bool {
		name := strings.ToUpper(strings.SplitN(kv, "=", 2)[0])
		return strings.HasPrefix(name, "CLAUDE") || strings.HasPrefix(name, "OPENROUTER") || strings.HasSuffix(name, "_API_KEY") || strings.HasPrefix(name, "ANTHROPIC") || name == "GOFLAGS"
	})
	env = append(env, "GOCACHE="+strings.TrimSpace(string(cache)), "GOPROXY=off", "GOMAXPROCS=4")
	return machine{exe: dev, fixture: fixture, scratch: scratch, model: model, home: owner, env: env}, nil
}

func (m machine) drive(a arm) outcome {
	o := outcome{arm: a}
	project, env, err := m.fresh(a)
	if err != nil {
		o.failed = err.Error()
		return o
	}
	var command []string
	parse := o.fromTofuRun
	switch a.name {
	case armNone:
	case armOneSmall, armOneTiers:
		command = []string{m.exe, "run", "--dir", project, "--model", m.model, "--no-subagents", a.briefs[0]}
	case armTofuTurn:
		command = []string{m.exe, "run", "--dir", project, "--model", m.model, a.briefs[0]}
	case armRace:
		body, _ := json.Marshal(map[string]any{"briefs": a.briefs, "check": "test", "path": "."})
		command, parse = []string{m.exe, "tool", "race", string(body), "--dir", project}, o.fromRace
	case armClaudeTurn:
		command, parse = []string{"claude", "-p", a.briefs[0], "--model", claudeModel, "--effort", "medium", "--output-format", "json", "--permission-mode", "bypassPermissions", "--safe-mode"}, o.fromClaude
	default:
		panic("arm " + a.name)
	}
	if command != nil {
		started := time.Now()
		stdout, output, err := run(project, env, sessionTimeout, command[0], command[1:]...)
		if a.name == armTofuTurn && strings.Contains(stdout, restateWait) {
			more, said, again := run(project, env, sessionTimeout, m.exe, "run", "--dir", project, "--model", m.model, "--session", "head", personGo)
			stdout, output, err = stdout+more, output+said, cmp.Or(err, again)
			o.note = "the lead restated the task and waited, so the suite answered " + personGo + " once as a person would; "
		}
		o.spent = time.Since(started)
		parse(stdout)
		if a.name == armTofuTurn {
			o.addRaces(filepath.Join(filepath.Dir(project), "home"))
		}
		if err != nil {
			o.failed = lastLine(output, err)
		}
		if err := os.WriteFile(filepath.Join(filepath.Dir(project), "output.txt"), []byte(output), 0o644); err != nil {
			o.failed = cmp.Or(o.failed, err.Error())
		}
	}
	o.passed, o.check = check(project, env)
	o.changed = changedFiles(project, env)
	return o
}

func (m machine) fresh(a arm) (string, []string, error) {
	dir := filepath.Join(m.scratch, a.key)
	project, home := filepath.Join(dir, "orders"), filepath.Join(dir, "home")
	if err := os.CopyFS(project, os.DirFS(m.fixture)); err != nil {
		return project, nil, err
	}
	env := slices.Clone(m.env)
	if a.name == armClaudeTurn {
		env = append(env, "HOME="+m.home, "USERPROFILE="+m.home)
	} else {
		roaming, local := filepath.Join(home, "AppData", "Roaming"), filepath.Join(home, "AppData", "Local")
		for _, made := range []string{filepath.Join(home, ".tofu"), roaming, local} {
			if err := os.MkdirAll(made, 0o700); err != nil {
				return project, nil, err
			}
		}
		env = append(env, "HOME="+home, "USERPROFILE="+home, "APPDATA="+roaming, "LOCALAPPDATA="+local)
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}, {"-c", "user.name=bench", "-c", "user.email=bench@example.invalid", "commit", "-q", "-m", "init"}} {
		if _, output, err := run(project, env, checkTimeout, "git", args...); err != nil {
			return project, env, fmt.Errorf("git %s: %s", args[0], lastLine(output, err))
		}
	}
	if a.name == armNone || a.name == armClaudeTurn {
		return project, env, nil
	}
	return project, env, m.login(project, home, env)
}

func (m machine) login(project, home string, env []string) error {
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Join(m.home, ".tofu", "agent.db"))+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	var stamp sql.NullString
	if err := db.QueryRow(`SELECT MIN(json_extract(data, '$.expires')) FROM credentials WHERE provider = 'claude-sub' AND disabled_cause IS NULL`).Scan(&stamp); err != nil {
		return err
	}
	if !stamp.Valid {
		return errors.New("the owner's tofu home holds no usable claude-sub login")
	}
	expires, err := time.Parse(time.RFC3339Nano, stamp.String)
	if err != nil {
		return err
	}
	if left := time.Until(expires); left < tokenOutlives {
		return fmt.Errorf("the claude-sub access token expires in %s, under %s, and a refresh inside the copy would rotate the refresh token the owner's home holds", left.Round(time.Minute), tokenOutlives)
	}
	if _, err := db.Exec(`VACUUM INTO ?`, filepath.Join(home, ".tofu", "agent.db")); err != nil {
		return err
	}
	classifier, err := os.ReadFile(catalog.RoleFile(filepath.Join(m.home, ".tofu"), catalog.RoleClassifier))
	if err != nil {
		return fmt.Errorf("the owner's home binds no classifier, and the gate needs one: %w", err)
	}
	_, bound, _ := strings.Cut(strings.TrimSpace(string(classifier)), "model:")
	for _, args := range [][]string{{"models", "update"}, {"models", "bind", "orchestrator", m.model}, {"models", "bind", "sub-agent", m.model}, {"models", "bind", "classifier", strings.TrimSpace(bound)}} {
		if _, output, err := run(project, env, checkTimeout, m.exe, args...); err != nil {
			return fmt.Errorf("tofu %s in the fresh home: %s", strings.Join(args, " "), lastLine(output, err))
		}
	}
	return nil
}

func check(project string, env []string) (bool, string) {
	listed, output, err := run(project, env, checkTimeout, "go", "list", "./...")
	if err != nil {
		return false, "fails: go list: " + lastLine(output, err)
	}
	packages := slices.DeleteFunc(strings.Fields(listed), func(p string) bool { return strings.HasSuffix(p, "/broken") })
	_, output, err = run(project, env, checkTimeout, "go", append([]string{"test", "-count=1"}, packages...)...)
	if err != nil {
		return false, "fails: " + failLine(output, err)
	}
	return true, fmt.Sprintf("passes: go test over %d packages", len(packages))
}

func changedFiles(project string, env []string) int {
	stdout, _, err := run(project, env, checkTimeout, "git", "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return -1
	}
	return strings.Count(stdout, "\n")
}

func run(dir string, env []string, limit time.Duration, name string, args ...string) (string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir, cmd.Env = dir, env
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	return stdout.String(), stdout.String() + "\n" + stderr.String(), err
}

func failLine(output string, err error) string {
	for line := range strings.Lines(output) {
		if strings.Contains(line, "got ") || strings.Contains(strings.ToLower(line), "error") || strings.HasPrefix(line, "FAIL") {
			return strings.TrimSpace(line)
		}
	}
	return lastLine(output, err)
}

func lastLine(output string, err error) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	return cmp.Or(strings.TrimSpace(lines[len(lines)-1]), err.Error())
}
