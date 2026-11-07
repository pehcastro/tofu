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
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"tofu/internal/models/llm/catalog"
)

type arm struct {
	key, exe string
}

type outcome struct {
	arm                        arm
	run                        int
	passed                     bool
	check, model, failed, note string
	requests, peak, peakStep   int
	last, sum, out             int
	notices, logReads, logSize int
	calls                      map[string]int
	offered                    bool
	spent                      time.Duration
}

type machine struct {
	fixture, scratch, model, home string
	ceiling                       int
	env                           []string
}

const (
	tokenOutlives  = 45 * time.Minute
	sessionTimeout = 20 * time.Minute
	checkTimeout   = 3 * time.Minute
	restateWait    = "waiting for your go or your correction"
	personGo       = "go"
	forgetNotice   = "passed 70% of its"
	logBytes       = 30 * 1024
	task           = "CI failed on the last run and its whole log is build.log in this folder. Read build.log first, find why CI failed, fix the cause in this Go module without touching broken/, then run go test . and stop when it passes. Go ahead without asking."
)

var (
	ownTools    = []string{"context", "budget", "forget", "note", "digest"}
	turnLine    = regexp.MustCompile(`(?m)^turn \S+ outcome (\S+) model`)
	stepLine    = regexp.MustCompile(`(?m)^step \d+: stop_reason `)
	requestText = regexp.MustCompile(`^#\w+ · (\S+) · in (\d+) out (\d+) read (\d+) wrote (\d+)`)
	forgetSent  = regexp.MustCompile(`"name":\s*"forget"`)
)

type traceRow struct {
	At   time.Time `json:"at"`
	Kind string    `json:"kind"`
	ID   string    `json:"id"`
	Text string    `json:"text"`
}

func main() {
	tree := flag.String("tree", ".", "the tofu repository root both arms are built from, back to back")
	scratch := flag.String("scratch", "", "a directory each run gets a fresh project copy and a fresh HOME under")
	model := flag.String("model", "claude-sub/claude-sonnet-5", "the claude-sub model every run asks")
	ceiling := flag.Int("ceiling", 60000, "the context ceiling every run gets with --context-ceiling, so the 0.7 notice can cross in one turn")
	runs := flag.Int("runs", 3, "sessions per arm, one at a time, alternating arms")
	out := flag.String("out", "", "the report file, otherwise standard output")
	flag.Parse()
	if *scratch == "" {
		fmt.Fprintln(os.Stderr, "usage: context -scratch <dir> [-tree <repo>] [-model claude-sub/<model>] [-ceiling N] [-runs N] [-out <report.md>]\nthis suite needs the network and a claude-sub login in the owner's tofu home, and makes live subscription calls")
		os.Exit(2)
	}
	if !strings.HasPrefix(*model, "claude-sub/") {
		fmt.Fprintln(os.Stderr, "context: -model must be a claude-sub/ slug, since every run is on the subscription and never on a key")
		os.Exit(2)
	}
	began := time.Now()
	m, err := newMachine(filepath.Join(*tree, "bench/suites/tools/fixtures/go"), filepath.Join(*scratch, began.Format("20060102-150405")), *model, *ceiling)
	if err == nil {
		err = os.MkdirAll(m.scratch, 0o700)
	}
	var on, off string
	if err == nil {
		on, off, err = m.build(*tree)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, "cost: every run is one tofu run on "+*model+", subscription windows, no money; the gate's Jev calls on the owner's classifier binding, under a tenth of a cent")
	var rows []outcome
	for i := 1; i <= *runs; i++ {
		for _, a := range []arm{{key: "on", exe: on}, {key: "off", exe: off}} {
			o := m.drive(a, i)
			rows = append(rows, o)
			fmt.Fprintf(os.Stderr, "%s run %d: peak %d, passed %v, %s\n", a.key, i, o.peak, o.passed, cmp.Or(o.failed, "ran"))
		}
	}
	report := render(rows, began, m, version(on))
	if *out == "" {
		fmt.Print(report)
		return
	}
	if err := os.WriteFile(*out, []byte(report), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newMachine(fixture, scratch, model string, ceiling int) (machine, error) {
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
	env = append(env, "GOCACHE="+strings.TrimSpace(string(cache)), "GOPROXY=off", "GOMAXPROCS=4", "GOWORK=off")
	return machine{fixture: fixture, scratch: scratch, model: model, home: owner, ceiling: ceiling, env: env}, nil
}

func (m machine) build(tree string) (string, string, error) {
	source, err := filepath.Abs(filepath.Join(tree, "apps", "cli", "run.go"))
	if err != nil {
		return "", "", err
	}
	body, err := os.ReadFile(source)
	if err != nil {
		return "", "", err
	}
	text := string(body)
	for _, constructor := range []string{`{"context", newContextTool}`, `{"budget", newBudgetTool}`, `{"forget", newForgetTool}`, `{"note", newNoteTool}`, `{"digest", newDigestTool}`} {
		line := "\t\t" + constructor + ",\n"
		if strings.Count(text, line) != 1 {
			return "", "", fmt.Errorf("the off arm drops %s from leadToolConstructors in %s, and that line is there %d times, not once", constructor, source, strings.Count(text, line))
		}
		text = strings.Replace(text, line, "", 1)
	}
	replaced := filepath.Join(m.scratch, "run_off.go")
	overlay := filepath.Join(m.scratch, "overlay.json")
	listed, _ := json.Marshal(map[string]map[string]string{"Replace": {source: replaced}})
	if err := cmp.Or(os.WriteFile(replaced, []byte(text), 0o644), os.WriteFile(overlay, listed, 0o644)); err != nil {
		return "", "", err
	}
	on, off := filepath.Join(m.scratch, "tofu-on.exe"), filepath.Join(m.scratch, "tofu-off.exe")
	env := append(slices.Clone(os.Environ()), "GOMAXPROCS=4")
	for _, args := range [][]string{{"build", "-o", on, "./apps/cli"}, {"build", "-overlay", overlay, "-o", off, "./apps/cli"}} {
		if _, output, err := run(tree, env, 10*time.Minute, "go", args...); err != nil {
			return "", "", fmt.Errorf("go %s: %s", strings.Join(args, " "), lastLine(output, err))
		}
	}
	if said, other := version(on), version(off); said != other {
		return "", "", fmt.Errorf("the tree moved between the two builds: on is %s and off is %s", said, other)
	}
	return on, off, nil
}

func (m machine) drive(a arm, i int) outcome {
	o := outcome{arm: a, run: i, calls: map[string]int{}}
	dir := filepath.Join(m.scratch, fmt.Sprintf("%s-%d", a.key, i))
	project, env, err := m.fresh(dir)
	if err != nil {
		o.failed = err.Error()
		return o
	}
	if written, err := os.Stat(filepath.Join(project, "build.log")); err == nil {
		o.logSize = int(written.Size())
	}
	asked := []string{"run", "--dir", project, "--model", m.model, "--wire", "anthropic", "--effort", "medium", "--context-ceiling", strconv.Itoa(m.ceiling)}
	started := time.Now()
	stdout, output, err := run(project, env, sessionTimeout, a.exe, append(asked, task)...)
	if strings.Contains(stdout, restateWait) {
		more, said, again := run(project, env, sessionTimeout, a.exe, append(asked, "--session", "head", personGo)...)
		stdout, output, err = stdout+more, output+said, cmp.Or(err, again)
		o.note = "the lead restated the task and waited, so the suite answered " + personGo + " once; "
	}
	o.spent = time.Since(started)
	if err != nil {
		o.failed = lastLine(output, err)
	}
	rows, traces, traceErr := traced(project, env, a.exe)
	o.parse(stdout, rows)
	sent, _, _ := run(project, env, checkTimeout, a.exe, "session", "request", "head", "last", "--json")
	o.offered = forgetSent.MatchString(sent)
	if err := cmp.Or(traceErr, os.WriteFile(filepath.Join(dir, "output.txt"), []byte(output+"\n"+traces), 0o644)); err != nil {
		o.failed = cmp.Or(o.failed, err.Error())
	}
	_, said, err := run(project, env, checkTimeout, "go", "test", "-count=1", ".")
	o.passed, o.check = err == nil, "passes: go test ."
	if err != nil {
		o.check = "fails: " + lastLine(said, err)
	}
	return o
}

func traced(project string, env []string, exe string) ([]traceRow, string, error) {
	listed, output, err := run(project, env, checkTimeout, exe, "session", "list", "--all", "--json")
	if err != nil {
		return nil, "", fmt.Errorf("tofu session list: %s", lastLine(output, err))
	}
	var sessions struct {
		Data struct {
			Sessions []struct {
				Handle      string `json:"handle"`
				Generations int    `json:"generations"`
			} `json:"sessions"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(listed), &sessions); err != nil {
		return nil, "", fmt.Errorf("tofu session list --json: %w", err)
	}
	var handles []string
	for _, s := range sessions.Data.Sessions {
		line := s.Handle[:strings.LastIndex(s.Handle, ".")+1]
		for generation := 1; generation <= s.Generations; generation++ {
			handles = append(handles, line+strconv.Itoa(generation))
		}
	}
	var rows []traceRow
	var said strings.Builder
	for _, handle := range handles {
		body, output, err := run(project, env, checkTimeout, exe, "session", "trace", handle, "--agent", "lead", "--kind", "request,call,notice", "--json")
		if err != nil {
			return nil, "", fmt.Errorf("tofu session trace %s: %s", handle, lastLine(output, err))
		}
		var trace struct {
			Data struct {
				Rows []traceRow `json:"rows"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(body), &trace); err != nil {
			return nil, "", fmt.Errorf("tofu session trace %s --json: %w", handle, err)
		}
		for _, row := range trace.Data.Rows {
			if !slices.ContainsFunc(rows, func(seen traceRow) bool { return seen.ID == row.ID }) {
				rows = append(rows, row)
			}
		}
		said.WriteString(body + "\n")
	}
	slices.SortStableFunc(rows, func(a, b traceRow) int { return a.At.Compare(b.At) })
	return rows, said.String(), nil
}

func (o *outcome) parse(stdout string, rows []traceRow) {
	for _, turn := range turnLine.FindAllStringSubmatch(stdout, -1) {
		o.note += "outcome " + turn[1] + ", "
	}
	for _, row := range rows {
		switch row.Kind {
		case "request":
			sent := requestText.FindStringSubmatch(row.Text)
			if sent == nil {
				o.note += "a request row reads " + row.Text + ", "
				continue
			}
			window := number(sent[2]) + number(sent[4]) + number(sent[5])
			o.model, o.sum, o.out, o.last = sent[1], o.sum+window, o.out+number(sent[3]), window
			o.requests++
			if window > o.peak {
				o.peak, o.peakStep = window, o.requests
			}
		case "call":
			tool, _, _ := strings.Cut(row.Text, " ")
			o.calls[tool]++
			if tool == "read" && strings.Contains(row.Text, "build.log") {
				o.logReads++
			}
		case "notice":
			if strings.Contains(row.Text, forgetNotice) {
				o.notices++
			}
		}
	}
	if printed := len(stepLine.FindAllString(stdout, -1)); printed != o.requests {
		o.note += fmt.Sprintf("tofu run printed %d step lines and the traces hold %d lead requests, ", printed, o.requests)
	}
}

func number(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func (m machine) fresh(dir string) (string, []string, error) {
	project, home := filepath.Join(dir, "orders"), filepath.Join(dir, "home")
	if err := os.CopyFS(project, os.DirFS(m.fixture)); err != nil {
		return project, nil, err
	}
	roaming, local := filepath.Join(home, "AppData", "Roaming"), filepath.Join(home, "AppData", "Local")
	for _, made := range []string{filepath.Join(home, ".tofu", "roles"), roaming, local} {
		if err := os.MkdirAll(made, 0o700); err != nil {
			return project, nil, err
		}
	}
	env := append(slices.Clone(m.env), "HOME="+home, "USERPROFILE="+home, "APPDATA="+roaming, "LOCALAPPDATA="+local)
	_, failing, _ := run(project, env, checkTimeout, "go", "test", "-count=1", "-v", ".")
	if err := os.WriteFile(filepath.Join(project, "build.log"), buildLog(failing), 0o644); err != nil {
		return project, env, err
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}, {"-c", "user.name=bench", "-c", "user.email=bench@example.invalid", "commit", "-q", "-m", "init"}} {
		if _, output, err := run(project, env, checkTimeout, "git", args...); err != nil {
			return project, env, fmt.Errorf("git %s: %s", args[0], lastLine(output, err))
		}
	}
	return project, env, m.login(home)
}

func buildLog(failing string) []byte {
	var b bytes.Buffer
	at := time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)
	stamp := func() string { at = at.Add(time.Second); return at.Format("2006-01-02T15:04:05") + " ci-runner-7 " }
	tail := 40
	for step := 1; b.Len() < logBytes-len(failing)-tail*48; step++ {
		fmt.Fprintf(&b, "%sstep=%d go: downloading fixture.test/dep%d v0.%d.0 ok\n", stamp(), step, step, step)
	}
	for line := range strings.Lines(strings.TrimSpace(failing) + "\n") {
		b.WriteString(stamp() + line)
	}
	for shard := 1; shard <= tail; shard++ {
		fmt.Fprintf(&b, "%scleanup cache shard %d removed\n", stamp(), shard)
	}
	return b.Bytes()
}

func (m machine) login(home string) error {
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
	return os.WriteFile(catalog.RoleFile(filepath.Join(home, ".tofu"), catalog.RoleClassifier), classifier, 0o644)
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

func lastLine(output string, err error) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	return cmp.Or(strings.TrimSpace(lines[len(lines)-1]), err.Error())
}
