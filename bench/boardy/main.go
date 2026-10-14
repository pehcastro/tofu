package main

import (
	"bufio"
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
	"strconv"
	"strings"
	"time"
)

const (
	shownTicket = boardKey + "-750"
	serveLine   = 64 << 20
)

type probe struct {
	name   string
	args   []string
	method string
	params any
}

func probes() []probe {
	board := map[string]string{"board": boardKey}
	return []probe{
		{name: "list", args: []string{"boardy", "list", "--board", boardKey}},
		{name: "show", args: []string{"boardy", "show", shownTicket}},
		{name: "report", args: []string{"boardy", "report", "--board", boardKey}},
		{name: "epic list", args: []string{"boardy", "epic", "list", "--board", boardKey}},
		{name: "catchup", args: []string{"boardy", "catchup", "--board", boardKey}},
		{name: "serve boardy.boards", method: "boardy.boards", params: struct{}{}},
		{name: "serve boardy.list", method: "boardy.list", params: board},
		{name: "serve boardy.report", method: "boardy.report", params: board},
	}
}

type arm struct {
	label, tofu, version string
	samples              map[string][]time.Duration
	answers              map[string][]byte
}

var (
	inDoing = regexp.MustCompile(`in doing\s+\S+|"minutes":\s*\d+`)
	clocked = newClock()
)

type run struct {
	home, project, pprof string
	calls, tickets       int
}

func (r run) env(pprof string) []string {
	env := append(os.Environ(), "HOME="+r.home, "USERPROFILE="+r.home)
	if pprof != "" {
		env = append(env, "TOFU_PPROF="+pprof)
	}
	return env
}

func (r run) profile(a *arm, name string, round int, call ...string) string {
	if r.pprof == "" {
		return ""
	}
	return filepath.Join(append([]string{r.pprof, a.label, strings.ReplaceAll(name, " ", "-"), strconv.Itoa(round)}, call...)...)
}

func (r run) once(tofu, profile string, args ...string) ([]byte, time.Duration, error) {
	var out, errOut bytes.Buffer
	cmd := exec.Command(tofu, args...)
	cmd.Dir, cmd.Stdout, cmd.Stderr, cmd.Env = r.project, &out, &errOut, r.env(profile)
	started := clocked.now()
	err := cmd.Run()
	took := clocked.since(started)
	if err != nil {
		return nil, took, fmt.Errorf("tofu %s: %w: %s", strings.Join(args, " "), err, errOut.String())
	}
	return out.Bytes(), took, nil
}

func (r run) verb(a *arm, p probe, round int) error {
	for call := range r.calls {
		out, took, err := r.once(a.tofu, r.profile(a, p.name, round, strconv.Itoa(call)), p.args...)
		if err != nil {
			return fmt.Errorf("%s %w", a.label, err)
		}
		a.samples[p.name] = append(a.samples[p.name], took)
		a.answers[p.name] = inDoing.ReplaceAll(out, []byte("<masked>"))
	}
	return nil
}

func (r run) serve(a *arm, served []probe, round int) error {
	cmd := exec.Command(a.tofu, "serve", "--stdio", "--dir", r.project)
	cmd.Env = r.env(r.profile(a, "serve", round))
	var errOut bytes.Buffer
	cmd.Stderr = &errOut
	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	lines := bufio.NewScanner(out)
	lines.Buffer(make([]byte, 0, 1<<20), serveLine)
	next := 0
	call := func(method string, params any) ([]byte, time.Duration, error) {
		next++
		body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": strconv.Itoa(next), "method": method, "params": params})
		started := clocked.now()
		if _, err := in.Write(append(body, '\n')); err != nil {
			return nil, 0, err
		}
		for lines.Scan() {
			var line struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
				Result json.RawMessage `json:"result"`
				Error  json.RawMessage `json:"error"`
			}
			if json.Unmarshal(lines.Bytes(), &line) != nil || line.Method != "" || string(line.ID) != strconv.Quote(strconv.Itoa(next)) {
				continue
			}
			took := clocked.since(started)
			if line.Error != nil {
				return nil, took, fmt.Errorf("%s: %s", method, line.Error)
			}
			return line.Result, took, nil
		}
		return nil, 0, fmt.Errorf("%s: tofu serve closed its output: %w %s", method, lines.Err(), errOut.String())
	}
	_, _, err = call("initialize", map[string]string{"client": "bench-boardy"})
	for c := 0; err == nil && c < r.calls; c++ {
		for _, p := range served {
			var answer []byte
			var took time.Duration
			if answer, took, err = call(p.method, p.params); err != nil {
				break
			}
			a.samples[p.name] = append(a.samples[p.name], took)
			a.answers[p.name] = inDoing.ReplaceAll(answer, []byte("<masked>"))
		}
	}
	_ = in.Close()
	return errors.Join(err, cmd.Wait())
}

func quantile(samples []time.Duration, q float64) time.Duration {
	sorted := slices.Sorted(slices.Values(samples))
	return sorted[int(q*float64(len(sorted)-1)+0.5)]
}

func ms(d time.Duration) string { return fmt.Sprintf("%.2f", float64(d.Microseconds())/1000) }

func table(arms []*arm) string {
	var out strings.Builder
	out.WriteString("| probe | n |")
	for _, a := range arms {
		fmt.Fprintf(&out, " %s p50 ms | %s p90 ms |", a.label, a.label)
	}
	first, later := arms[0], arms[1:]
	for _, a := range later {
		fmt.Fprintf(&out, " %s p50 ratio | %s output |", a.label, a.label)
	}
	out.WriteString("\n|---|---|" + strings.Repeat("---|---|", len(arms)+len(later)) + "\n")
	for _, p := range probes() {
		fmt.Fprintf(&out, "| %s | %d |", p.name, len(first.samples[p.name]))
		for _, a := range arms {
			fmt.Fprintf(&out, " %s | %s |", ms(quantile(a.samples[p.name], 0.5)), ms(quantile(a.samples[p.name], 0.9)))
		}
		for _, a := range later {
			same := "differs"
			if bytes.Equal(first.answers[p.name], a.answers[p.name]) {
				same = "identical"
			}
			fmt.Fprintf(&out, " %.2fx | %s |", float64(quantile(first.samples[p.name], 0.5))/float64(quantile(a.samples[p.name], 0.5)), same)
		}
		out.WriteString("\n")
	}
	return out.String()
}

func parseArms(text string) ([]*arm, error) {
	var arms []*arm
	for pair := range strings.SplitSeq(text, ",") {
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
		arms = append(arms, &arm{label: label, tofu: full, version: strings.Join(strings.Fields(string(version)), " "), samples: map[string][]time.Duration{}, answers: map[string][]byte{}})
	}
	return arms, nil
}

func prepare(r run, tofu string) (string, error) {
	for _, dir := range []string{r.home, r.project} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
	}
	cmd := exec.Command(tofu, "boardy", "init", "--key", boardKey, "--name", "bench boardy", "--json")
	cmd.Dir, cmd.Env = r.project, r.env("")
	raw, err := cmd.Output()
	var made struct {
		Data struct {
			Dir string `json:"dir"`
		} `json:"data"`
	}
	if err == nil {
		err = json.Unmarshal(raw, &made)
	}
	if err != nil {
		return "", fmt.Errorf("boardy init: %w: %s", err, raw)
	}
	return generate(made.Data.Dir, r.tickets)
}

func measure(arms []*arm, r run, rounds int, note, keep string) error {
	made, err := prepare(r, arms[0].tofu)
	if err != nil {
		return err
	}
	_, settled, err := r.once(arms[0].tofu, "", "boardy", "list", "--board", boardKey, "--all")
	if err != nil {
		return err
	}
	var verbs, served []probe
	for _, p := range probes() {
		if p.method == "" {
			verbs = append(verbs, p)
		} else {
			served = append(served, p)
		}
	}
	fmt.Printf("bench/boardy, %s, %s\n", time.Now().Format(time.DateTime), note)
	fmt.Printf("  machine   %s/%s, %d cpus, GOMAXPROCS of the driven tofu %q\n", runtime.GOOS, runtime.GOARCH, runtime.NumCPU(), os.Getenv("GOMAXPROCS"))
	for _, a := range arms {
		fmt.Printf("  %-9s %s (%s)\n", a.label, a.version, a.tofu)
	}
	fmt.Printf("  board     %s %s, generated from seed %d, in a home made fresh in this run at %s\n", boardKey, made, mockSeed, r.home)
	fmt.Printf("  settle    one uncounted boardy list --all by the first arm after the board is written took %s ms: the first read of just-written files is slow on this machine, most likely the virus scanner\n", ms(settled))
	fmt.Printf("  calls     %d per probe per round, %d rounds, arm order alternates; a verb is one process, wall clock; serve is one process per arm per round, one uncounted initialize, then every call timed; nothing else discarded\n", r.calls, rounds)
	fmt.Printf("  masked    the in doing duration of report and serve boardy.report, which counts minutes up to now\n")
	if r.pprof != "" {
		fmt.Printf("  pprof     TOFU_PPROF under %s, which also costs time: these timings are not comparable with a run without it\n", r.pprof)
	}
	fmt.Println()
	for round := range rounds {
		order := slices.Clone(arms)
		if round%2 == 1 {
			slices.Reverse(order)
		}
		for _, a := range order {
			for _, p := range verbs {
				if err := r.verb(a, p, round); err != nil {
					return err
				}
			}
			if err := r.serve(a, served, round); err != nil {
				return fmt.Errorf("%s serve: %w", a.label, err)
			}
		}
	}
	fmt.Print(table(arms))
	if keep == "" {
		return nil
	}
	for _, a := range arms {
		for name, answer := range a.answers {
			path := filepath.Join(keep, a.label, strings.ReplaceAll(name, " ", "-")+".out")
			if err := errors.Join(os.MkdirAll(filepath.Dir(path), 0o755), os.WriteFile(path, answer, 0o644)); err != nil {
				return err
			}
		}
	}
	return nil
}

func main() {
	armsFlag := flag.String("arms", "", "label=path pairs, comma separated, such as before=old.exe,after=new.exe; the first arm makes the board")
	calls := flag.Int("calls", 20, "calls of each probe per arm per round")
	tickets := flag.Int("tickets", 1500, "tickets on the generated board, sub-tickets included")
	rounds := flag.Int("rounds", 2, "rounds, each arm once per round")
	note := flag.String("note", "", "what the arms are built from, printed in the header")
	pprof := flag.String("pprof", "", "a folder each call's TOFU_PPROF goes under, by arm, probe and round")
	keep := flag.String("keep", "", "a folder to write each arm's last masked output into, one file per probe; it also keeps the work folder")
	flag.Parse()
	arms, err := parseArms(*armsFlag)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "bench/boardy:", err)
		os.Exit(2)
	}
	work, err := os.MkdirTemp("", "tofu-bench-boardy-")
	if err == nil {
		r := run{home: filepath.Join(work, "home"), project: filepath.Join(work, "project"), pprof: *pprof, calls: *calls, tickets: *tickets}
		err = measure(arms, r, *rounds, *note, *keep)
	}
	if *keep == "" {
		err = errors.Join(err, os.RemoveAll(work))
	}
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "bench/boardy:", err)
		os.Exit(1)
	}
}
