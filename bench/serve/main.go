package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"

	"tofu/internal/host"
)

const (
	stepsPerTurn = 20
	boardKey     = "BENCH"
	boardEpic    = "BENCH-E1"
	boardTickets = 3
)

type arm struct {
	label, tofu, version string
	samples              map[string][]time.Duration
	answers              map[string][]byte
	sizes                map[string]int
}

type method struct {
	name   string
	params func(session string) any
}

func methods() []method {
	none := func(string) any { return struct{}{} }
	return []method{
		{"initialize", func(string) any { return host.InitializeParams{Client: "bench-serve"} }},
		{"status.list", none},
		{"session.list", none},
		{"query.docs", none},
		{"query.commands", none},
		{"query.settings", none},
		{"boardy.boards", none},
		{"session.trace", func(session string) any { return host.SessionParams{Session: session} }},
		{besideSmall, func(session string) any { return host.SessionParams{Session: session} }},
	}
}

func cassette(steps int) string {
	var lines []string
	for step := 1; step <= steps; step++ {
		if step%stepsPerTurn == 0 {
			lines = append(lines, fmt.Sprintf(`{"text":"turn %d done"}`, step/stepsPerTurn))
			continue
		}
		lines = append(lines, fmt.Sprintf(`{"tools":[{"name":"read","args":{"path":"notes.txt","start_line":%d,"end_line":%d}}]}`, step%stepsPerTurn, step%stepsPerTurn+1))
	}
	return strings.Join(lines, "\n") + "\n"
}

func verb(tofu, project string, env []string, args ...string) error {
	cmd := exec.Command(tofu, args...)
	cmd.Dir, cmd.Env = project, env
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("tofu %s: %w: %s", strings.Join(args, " "), err, out)
	}
	return nil
}

func environment(home string) []string {
	return append(os.Environ(), "HOME="+home, "USERPROFILE="+home)
}

func build(tofu, root, deck string, steps int) (string, error) {
	home, project := filepath.Join(root, "home"), filepath.Join(root, "project")
	for _, dir := range []string{home, project} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
	}
	notes := strings.Repeat("a line of notes the cassette reads\n", stepsPerTurn+1)
	if err := os.WriteFile(filepath.Join(project, "notes.txt"), []byte(notes), 0o644); err != nil {
		return "", err
	}
	if err := os.WriteFile(deck, []byte(cassette(steps)), 0o644); err != nil {
		return "", err
	}
	env := environment(home)
	if err := verb(tofu, project, env, "boardy", "init", "--key", boardKey, "--name", "bench serve"); err != nil {
		return "", err
	}
	if err := verb(tofu, project, env, "boardy", "epic", "new", boardEpic, "bench epic", "--board", boardKey); err != nil {
		return "", err
	}
	for ticket := 1; ticket <= boardTickets; ticket++ {
		if err := verb(tofu, project, env, "boardy", "new", "--board", boardKey, "--epic", boardEpic, fmt.Sprintf("bench ticket %d", ticket)); err != nil {
			return "", err
		}
	}
	s, err := serve(tofu, project, deck, filepath.Join(root, "build.log"), env)
	if err != nil {
		return "", err
	}
	var state host.SessionState
	err = func() error {
		if _, _, err := s.call("initialize", host.InitializeParams{Client: "bench-serve"}); err != nil {
			return err
		}
		if _, _, err := s.call("session.open", host.SessionOpenParams{Asking: host.AskingAuto}); err != nil {
			return err
		}
		raw, _, err := s.call("session.state", struct{}{})
		if err == nil {
			err = json.Unmarshal(raw, &state)
		}
		for turn := 1; err == nil && turn <= steps/stepsPerTurn; turn++ {
			err = s.turn(state.Session, fmt.Sprintf("read the notes, pass %d", turn))
		}
		return err
	}()
	return state.Session, errors.Join(err, s.close(), hostGone(home))
}

func mask(raw []byte, opened string) []byte {
	raw = regexp.MustCompile(`\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(\.\d+)?(Z|[+-]\d\d:\d\d)`).ReplaceAll(raw, []byte("<time>"))
	if opened == "" {
		return raw
	}
	return bytes.ReplaceAll(raw, []byte(opened), []byte("<opened>"))
}

func (a *arm) measure(pristine, live, deck, session string, calls, beside int) error {
	if err := os.RemoveAll(live); err != nil {
		return err
	}
	if err := os.CopyFS(live, os.DirFS(pristine)); err != nil {
		return err
	}
	s, err := serve(a.tofu, filepath.Join(live, "project"), deck, filepath.Join(live, "serve.log"), environment(filepath.Join(live, "home")))
	if err != nil {
		return err
	}
	err = func() error {
		if _, _, err := s.call("initialize", host.InitializeParams{Client: "bench-serve"}); err != nil {
			return err
		}
		raw, _, err := s.call("session.state", struct{}{})
		var state host.SessionState
		if err == nil {
			err = json.Unmarshal(raw, &state)
		}
		for call := 0; err == nil && call < calls; call++ {
			for _, m := range methods() {
				var took time.Duration
				if raw, took, err = s.call(m.name, m.params(session)); err != nil {
					break
				}
				a.samples[m.name] = append(a.samples[m.name], took)
				a.answers[m.name] = mask(raw, state.Session)
			}
		}
		for range beside {
			for _, delay := range besideDelays() {
				if err == nil {
					err = a.beside(s, session, delay)
				}
			}
		}
		return err
	}()
	return errors.Join(err, s.close(), hostGone(filepath.Join(live, "home")))
}

func quantile(samples []time.Duration, q float64) time.Duration {
	sorted := slices.Sorted(slices.Values(samples))
	return sorted[int(q*float64(len(sorted)-1)+0.5)]
}

func ms(d time.Duration) string {
	return fmt.Sprintf("%.2f", float64(d.Microseconds())/1000)
}

func table(arms []*arm) string {
	var out strings.Builder
	fmt.Fprintf(&out, "%-16s %5s", "method", "n")
	for _, a := range arms {
		fmt.Fprintf(&out, " %12s %12s", a.label+" p50", a.label+" p99")
	}
	if len(arms) > 1 {
		fmt.Fprintf(&out, " %9s %10s", "p50 x", "bytes")
	}
	out.WriteString("\n")
	for _, m := range methods() {
		first := arms[0]
		fmt.Fprintf(&out, "%-16s %5d", m.name, len(first.samples[m.name]))
		for _, a := range arms {
			fmt.Fprintf(&out, " %12s %12s", ms(quantile(a.samples[m.name], 0.5)), ms(quantile(a.samples[m.name], 0.99)))
		}
		if last := arms[len(arms)-1]; len(arms) > 1 {
			same := "differ"
			if bytes.Equal(first.answers[m.name], last.answers[m.name]) {
				same = "identical"
			}
			fmt.Fprintf(&out, " %8.2fx %10s", float64(quantile(first.samples[m.name], 0.5))/float64(quantile(last.samples[m.name], 0.5)), same)
		}
		out.WriteString("\n")
	}
	return out.String()
}

func besideTable(arms []*arm) string {
	var out strings.Builder
	fmt.Fprintf(&out, "\n%-34s %5s", "beside case", "n")
	for _, a := range arms {
		fmt.Fprintf(&out, " %12s %12s %14s", a.label+" p50", a.label+" p99", a.label+" bytes")
	}
	out.WriteString("\n")
	for _, delay := range besideDelays() {
		asked, small := besideNames(delay)
		for _, name := range []string{asked, small} {
			fmt.Fprintf(&out, "%-34s %5d", name, len(arms[0].samples[name]))
			for _, a := range arms {
				fmt.Fprintf(&out, " %12s %12s %14d", ms(quantile(a.samples[name], 0.5)), ms(quantile(a.samples[name], 0.99)), a.sizes[name])
			}
			out.WriteString("\n")
		}
	}
	return out.String()
}

type plan struct {
	calls, rounds, steps, rows, beside int
	keep                               string
}

func run(arms []*arm, p plan) error {
	work, err := os.MkdirTemp("", "bserve-")
	if err != nil {
		return err
	}
	live, pristine, deck := filepath.Join(work, "live"), filepath.Join(work, "pristine"), filepath.Join(work, "session.jsonl")
	session, err := build(arms[0].tofu, live, deck, p.steps)
	if err != nil {
		return fmt.Errorf("building the %d step session: %w, the log is %s", p.steps, err, filepath.Join(live, "build.log"))
	}
	if err := os.Remove(filepath.Join(live, "build.log")); err != nil {
		return err
	}
	if p.rows > 0 {
		if err := writeLedger(live, p.rows); err != nil {
			return fmt.Errorf("writing %d synthetic ledger rows: %w", p.rows, err)
		}
	}
	if err := os.CopyFS(pristine, os.DirFS(live)); err != nil {
		return err
	}
	fmt.Printf("bench/serve, %s, the version and commit of each arm below\n", time.Now().Format(time.DateOnly))
	fmt.Printf("  machine   %s/%s, %d cpus, GOMAXPROCS of the driven tofu %s\n", runtime.GOOS, runtime.GOARCH, runtime.NumCPU(), os.Getenv("GOMAXPROCS"))
	for _, a := range arms {
		fmt.Printf("  %-9s %s (%s)\n", a.label, a.version, a.tofu)
	}
	fmt.Printf("  session   %s, %d steps in %d turns, built on a cassette in a fresh home under %s\n", session, p.steps, p.steps/stepsPerTurn, work)
	fmt.Printf("  calls     %d per method per round, %d rounds, arms alternate; one initialize per run before timing, not counted\n", p.calls, p.rounds)
	fmt.Printf("  ledger    %d synthetic rows; beside case %d times per delay per round: %s with last %d, then %s after each of %v\n", p.rows, p.beside, besideAsked, besideLast, besideSmall, besideDelays())
	fmt.Printf("  masked    timestamps and the id of the session serve opens, before comparing answers\n\n")
	for round := range p.rounds {
		order := slices.Clone(arms)
		if round%2 == 1 {
			slices.Reverse(order)
		}
		for _, a := range order {
			if err := a.measure(pristine, live, deck, session, p.calls, p.beside); err != nil {
				return fmt.Errorf("%s round %d: %w, the log is %s", a.label, round+1, err, filepath.Join(live, "serve.log"))
			}
		}
	}
	fmt.Print(table(arms))
	if p.beside > 0 {
		fmt.Print(besideTable(arms))
	}
	if p.keep == "" {
		return os.RemoveAll(work)
	}
	for _, a := range arms {
		if err := os.MkdirAll(filepath.Join(p.keep, a.label), 0o755); err != nil {
			return err
		}
		for name, answer := range a.answers {
			if err := os.WriteFile(filepath.Join(p.keep, a.label, name+".json"), answer, 0o644); err != nil {
				return err
			}
		}
	}
	return nil
}

func parseArms(text string) ([]*arm, error) {
	var arms []*arm
	for _, pair := range strings.Split(text, ",") {
		label, path, named := strings.Cut(pair, "=")
		if !named || label == "" || path == "" {
			return nil, fmt.Errorf("--arms takes label=path pairs separated by commas, and was given %q", pair)
		}
		full, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		version, err := exec.Command(full, "version").Output()
		if err != nil {
			return nil, fmt.Errorf("%s version: %w", full, err)
		}
		arms = append(arms, &arm{label: label, tofu: full, version: strings.Join(strings.Fields(string(version)), " "), samples: map[string][]time.Duration{}, answers: map[string][]byte{}, sizes: map[string]int{}})
	}
	return arms, nil
}

func main() {
	armsFlag := flag.String("arms", "", "label=path pairs, comma separated, such as before=old.exe,after=new.exe; the first arm builds the session")
	calls := flag.Int("calls", 50, "calls of each method per round")
	rounds := flag.Int("rounds", 2, "rounds, each arm once per round")
	steps := flag.Int("steps", 200, "steps in the traced session, a multiple of 20")
	rows := flag.Int("ledger", 0, "synthetic ledger rows written into the bench home before timing")
	beside := flag.Int("beside", 0, "times per round to send query.drain while a query.ledger of the last 10000 rows builds; needs --ledger")
	keep := flag.String("keep", "", "a folder to write each arm's masked answers into, one file per method; it also keeps the work folder the header names, which is removed without it")
	flag.Parse()
	arms, err := parseArms(*armsFlag)
	switch {
	case err != nil:
	case *steps < stepsPerTurn || *steps%stepsPerTurn != 0:
		err = fmt.Errorf("--steps %d is not a positive multiple of %d", *steps, stepsPerTurn)
	case *beside > 0 && *rows == 0:
		err = errors.New("--beside needs --ledger rows to build against")
	}
	if err == nil {
		err = run(arms, plan{calls: *calls, rounds: *rounds, steps: *steps, rows: *rows, beside: *beside, keep: *keep})
	}
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "bench/serve:", err)
		os.Exit(1)
	}
}
