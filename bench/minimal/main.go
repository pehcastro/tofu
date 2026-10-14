package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
)

const (
	sampleEvery = 50 * time.Millisecond
	mebibyte    = 1 << 20
)

type limits struct {
	MemoryMB int
	CPU      int
}

type child struct {
	Name      string  `json:"name"`
	Processes int     `json:"processes"`
	PeakRSSMB float64 `json:"peak_rss_mb"`
}

type goroutineCount struct {
	GOMAXPROCS       int    `json:"gomaxprocs"`
	PeakGoroutines   uint64 `json:"peak_goroutines"`
	SteadyGoroutines uint64 `json:"steady_goroutines"`
	PeakHeapBytes    uint64 `json:"peak_heap_bytes"`
	PeakStackBytes   uint64 `json:"peak_stack_bytes"`
	PeakGoTotalBytes uint64 `json:"peak_go_total_bytes"`
}

type row struct {
	Tool          string   `json:"tool"`
	Version       string   `json:"version"`
	Task          string   `json:"task"`
	Agents        int      `json:"agents"`
	Model         string   `json:"model"`
	Credential    string   `json:"credential"`
	Wire          string   `json:"wire"`
	Mechanism     string   `json:"mechanism"`
	MemoryLimitMB int      `json:"memory_limit_mb"`
	CPUs          int      `json:"cpus"`
	Machine       string   `json:"machine"`
	Date          string   `json:"date"`
	Exit          int      `json:"exit"`
	Answered      int      `json:"answered"`
	OOMKills      int      `json:"oom_kills"`
	TimedOut      bool     `json:"timed_out"`
	Finished      bool     `json:"finished"`
	WallS         float64  `json:"wall_s"`
	CPUS          float64  `json:"cpu_s"`
	PeakMB        float64  `json:"peak_mb"`
	SteadyMB      float64  `json:"steady_mb"`
	PeakRSSMB     float64  `json:"peak_rss_mb"`
	PeakPrivateMB float64  `json:"peak_private_mb"`
	PeakProcesses int      `json:"peak_processes"`
	Children      []child  `json:"children"`
	Commands      []string `json:"commands"`
	goroutineCount
	Samples int `json:"samples"`
}

type finished struct {
	cpu       time.Duration
	peakBytes uint64
	oomKills  int
}

type reading struct {
	chargedBytes uint64
	privateBytes uint64
	rss          map[string]uint64
	processes    map[string]int
	commands     []string
}

func (r *row) tally(now reading, children map[string]*child) {
	r.PeakProcesses = max(r.PeakProcesses, sum(now.processes))
	r.PeakPrivateMB = max(r.PeakPrivateMB, float64(now.privateBytes)/mebibyte)
	r.PeakRSSMB = max(r.PeakRSSMB, float64(sum(now.rss))/mebibyte)
	for _, command := range now.commands {
		if !slices.Contains(r.Commands, command) {
			r.Commands = append(r.Commands, command)
		}
	}
	for name, count := range now.processes {
		seen := children[name]
		if seen == nil {
			seen = &child{Name: name}
			children[name] = seen
		}
		seen.Processes = max(seen.Processes, count)
		seen.PeakRSSMB = max(seen.PeakRSSMB, float64(now.rss[name])/mebibyte)
	}
}

func measure(cmd *exec.Cmd, limit limits, hold, timeout time.Duration, into *row) error {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	start := time.Now()
	cage, err := confine(cmd, limit)
	if err != nil {
		return err
	}
	into.Mechanism, into.MemoryLimitMB = mechanism, limit.MemoryMB
	if limit.CPU >= 0 {
		into.CPUs = 1
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	if hold == 0 {
		_ = stdin.Close()
	}
	ticker := time.NewTicker(sampleEvery)
	defer ticker.Stop()
	var charged []uint64
	children := map[string]*child{}
	closeAt, killAt := start.Add(hold), start.Add(timeout)
	for waiting := true; waiting; {
		now := cage.sample()
		if len(now.processes) > 0 {
			charged = append(charged, now.chargedBytes)
		}
		into.tally(now, children)
		select {
		case <-done:
			waiting = false
		case <-ticker.C:
			if hold > 0 && time.Now().After(closeAt) {
				_ = stdin.Close()
			}
			if time.Now().After(killAt) && !into.TimedOut {
				into.TimedOut = cmd.Process.Kill() == nil
			}
		}
	}
	into.WallS = time.Since(start).Seconds()
	final := cage.finish()
	into.CPUS, into.OOMKills = final.cpu.Seconds(), final.oomKills
	into.PeakMB = max(float64(final.peakBytes), float64(slices.Max(append(charged, 0)))) / mebibyte
	into.Exit, into.Samples, into.Date = cmd.ProcessState.ExitCode(), len(charged), time.Now().Format(time.DateOnly)
	into.Machine = fmt.Sprintf("%s/%s %d logical CPUs", runtime.GOOS, runtime.GOARCH, runtime.NumCPU())
	for _, name := range slices.Sorted(maps.Keys(children)) {
		into.Children = append(into.Children, *children[name])
	}
	if len(charged) == 0 {
		return errors.New("the tree ended before its first sample")
	}
	slices.Sort(charged)
	into.SteadyMB = float64(charged[len(charged)/2]) / mebibyte
	return nil
}

func sum[V int | uint64](values map[string]V) V {
	var total V
	for value := range maps.Values(values) {
		total += value
	}
	return total
}

func answered(transcript []byte, project string, agents int) int {
	found := 0
	for k := min(agents, 1); k <= agents; k++ {
		raw, _ := os.ReadFile(filepath.Join(project, fmt.Sprintf("f%d.txt", k)))
		if first := strings.SplitN(string(raw), "\n", 2)[0]; first != "" && strings.Contains(string(transcript), first) {
			found++
		}
	}
	return found
}

func appendRow(path string, measured row) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	line, _ := json.Marshal(measured)
	_, err = file.Write(append(line, '\n'))
	return errors.Join(err, file.Close())
}

func sample(args []string) error {
	flags := flag.NewFlagSet("sample", flag.ExitOnError)
	var measured row
	var limit limits
	flags.StringVar(&measured.Tool, "tool", "", "the tool measured")
	flags.StringVar(&measured.Version, "version", "", "the version the tool printed")
	flags.StringVar(&measured.Task, "task", "spawn", "spawn or serve-idle")
	flags.IntVar(&measured.Agents, "agents", 0, "sub-agents asked for")
	flags.StringVar(&measured.Model, "model", "cassette", "the model build")
	flags.StringVar(&measured.Credential, "credential", "none", "subscription or none")
	flags.StringVar(&measured.Wire, "wire", "cassette", "the wire spoken")
	flags.IntVar(&limit.MemoryMB, "memory", 256, "the memory limit in MiB")
	flags.IntVar(&limit.CPU, "cpu", 1, "the one logical CPU the tree is pinned to")
	procs := flags.Int("gomaxprocs", 0, "GOMAXPROCS for the tool, 0 to leave it to the limit")
	hold := flags.Duration("hold", 0, "how long standard input stays open, for an idle server")
	timeout := flags.Duration("timeout", 5*time.Minute, "when the tree is killed")
	count := flags.String("count", "", "the goroutine count file the overlay build writes")
	project := flags.String("project", "", "the scratch project holding f0.txt to f10.txt")
	out := flags.String("out", "", "the results file a row is appended to")
	transcript := flags.String("transcript", "", "where the tool's output is kept")
	_ = flags.Parse(args)
	command := flags.Args()
	if len(command) == 0 || *out == "" || *transcript == "" {
		return errors.New("sample needs -out, -transcript and a command after --")
	}
	output, err := os.Create(*transcript)
	if err != nil {
		return err
	}
	defer func() { _ = output.Close() }()
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Dir, cmd.Stdout, cmd.Stderr = *project, output, output
	cmd.Env = slices.DeleteFunc(os.Environ(), func(pair string) bool { return strings.HasPrefix(pair, "GOMAXPROCS=") })
	if *procs > 0 {
		cmd.Env = append(cmd.Env, fmt.Sprintf("GOMAXPROCS=%d", *procs))
	}
	if err := measure(cmd, limit, *hold, *timeout, &measured); err != nil {
		return err
	}
	said, _ := os.ReadFile(*transcript)
	want := 0
	if measured.Task == "spawn" {
		measured.Answered, want = answered(said, *project, measured.Agents), max(measured.Agents, 1)
	}
	if *count != "" {
		raw, _ := os.ReadFile(*count)
		_ = json.Unmarshal(raw, &measured.goroutineCount)
	}
	measured.Finished = measured.Exit == 0 && measured.OOMKills == 0 && !measured.TimedOut && measured.Answered == want
	fmt.Printf("%s %s %s limit=%dMB agents=%d answered=%d exit=%d oom=%d wall=%.1fs cpu=%.2fs peak=%.1fMB steady=%.1fMB rss=%.1fMB processes=%d goroutines=%d/%d gomaxprocs=%d\n",
		measured.Tool, measured.Task, measured.Mechanism, limit.MemoryMB, measured.Agents, measured.Answered, measured.Exit, measured.OOMKills, measured.WallS, measured.CPUS,
		measured.PeakMB, measured.SteadyMB, measured.PeakRSSMB, measured.PeakProcesses, measured.PeakGoroutines, measured.SteadyGoroutines, measured.GOMAXPROCS)
	if !measured.Finished {
		tail := said[max(0, len(said)-600):]
		fmt.Printf("did not finish, transcript tail: %s\n", tail)
	}
	return appendRow(*out, measured)
}

func readRows(path string) ([]row, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	var rows []row
	scanner := bufio.NewScanner(file)
	scanner.Buffer(nil, mebibyte)
	for scanner.Scan() {
		var read row
		if err := json.Unmarshal(scanner.Bytes(), &read); err != nil {
			return nil, err
		}
		rows = append(rows, read)
	}
	return rows, scanner.Err()
}

func children(r row) string {
	var said []string
	for _, c := range r.Children {
		said = append(said, fmt.Sprintf("%s x%d %.1f", c.Name, c.Processes, c.PeakRSSMB))
	}
	return strings.Join(said, ", ")
}

func goroutines(r row) string {
	if r.GOMAXPROCS == 0 {
		return "not counted"
	}
	return fmt.Sprintf("%d / %d", r.PeakGoroutines, r.SteadyGoroutines)
}

func report(args []string) error {
	if len(args) != 1 {
		return errors.New("report needs the results file")
	}
	rows, err := readRows(args[0])
	if err != nil {
		return err
	}
	fmt.Println("| tool | task | agents | mechanism | limit MB | vCPU | finished | answered | exit | OOM kills | wall s | CPU s | peak MB | steady MB | peak private MB | peak RSS MB | Go heap peak MB | goroutines peak / steady | GOMAXPROCS | processes | children, peak RSS MB | version | model | credential | date |")
	fmt.Println("|" + strings.Repeat("---|", 25))
	for _, r := range rows {
		fmt.Printf("| %s | %s | %d | %s | %d | %d | %t | %d | %d | %d | %.1f | %.2f | %.1f | %.1f | %.1f | %.1f | %.1f | %s | %d | %d | %s | %s | %s | %s | %s |\n",
			r.Tool, r.Task, r.Agents, r.Mechanism, r.MemoryLimitMB, r.CPUs, r.Finished, r.Answered, r.Exit, r.OOMKills, r.WallS, r.CPUS, r.PeakMB, r.SteadyMB, r.PeakPrivateMB, r.PeakRSSMB,
			float64(r.PeakHeapBytes)/mebibyte, goroutines(r), r.GOMAXPROCS, r.PeakProcesses, children(r), r.Version, r.Model, r.Credential, r.Date)
	}
	return nil
}

func main() {
	verbs := map[string]func([]string) error{"sample": sample, "report": report}
	if len(os.Args) < 2 || verbs[os.Args[1]] == nil {
		fmt.Fprintln(os.Stderr, "usage: minimal sample|report")
		os.Exit(2)
	}
	if err := verbs[os.Args[1]](os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "bench/minimal:", err)
		os.Exit(1)
	}
}
