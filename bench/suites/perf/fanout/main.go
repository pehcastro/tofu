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
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	screenColumns = 160
	screenRows    = 45
	mebibyte      = 1 << 20
	quitPresses   = 6
	quitWithin    = 60 * time.Second
	hostReady     = 15 * time.Second
	hostLeaves    = 20 * time.Second
	model         = "claude-sub/claude-sonnet-5"
	mockLoginCode = "mock-login-code"
	fellWords     = "fell behind the tofu host"
	screenTail    = 256 << 10
)

type reading struct {
	at                               time.Time
	workingMB, privateMB, cpuSeconds float64
}

type sample struct {
	Kind          string  `json:"kind"`
	Arm           string  `json:"arm"`
	Phase         string  `json:"phase"`
	Second        int     `json:"second"`
	HostCores     float64 `json:"host_cores"`
	HostWorkingMB float64 `json:"host_working_mb"`
	HostPrivateMB float64 `json:"host_private_mb"`
	TUICores      float64 `json:"tui_cores"`
	TUIWorkingMB  float64 `json:"tui_working_mb"`
	TUIPrivateMB  float64 `json:"tui_private_mb"`
}

type summary struct {
	Kind            string  `json:"kind"`
	Arm             string  `json:"arm"`
	Phase           string  `json:"phase"`
	Seconds         int     `json:"seconds"`
	HostMeanCores   float64 `json:"host_mean_cores"`
	HostPeakCores   float64 `json:"host_peak_cores"`
	HostPeakWorking float64 `json:"host_peak_working_mb"`
	HostPeakPrivate float64 `json:"host_peak_private_mb"`
	TUIMeanCores    float64 `json:"tui_mean_cores"`
	TUIPeakCores    float64 `json:"tui_peak_cores"`
	TUIPeakWorking  float64 `json:"tui_peak_working_mb"`
	TUIPeakPrivate  float64 `json:"tui_peak_private_mb"`
	WorkSteps       int64   `json:"work_steps"`
	FellBehind      int     `json:"fell_behind"`
	FellAgainWithin int     `json:"fell_again_within_1s"`
	Workers         int     `json:"workers"`
	Profiled        bool    `json:"profiled"`
	Version         string  `json:"version"`
	Machine         string  `json:"machine"`
	Date            string  `json:"date"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: fanout seed|measure|report [flags]")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "seed":
		err = seed(os.Args[2:])
	case "measure":
		err = measure(os.Args[2:])
	case "report":
		err = report(os.Args[2:])
	default:
		err = fmt.Errorf("unknown verb %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "fanout:", err)
		os.Exit(1)
	}
}

func freshEnv(home string, extra ...string) []string {
	env := slices.DeleteFunc(os.Environ(), func(kv string) bool {
		name := strings.ToUpper(strings.SplitN(kv, "=", 2)[0])
		return name == "HOME" || name == "USERPROFILE" || name == "GOMAXPROCS" || strings.HasPrefix(name, "TOFU_") || strings.HasPrefix(name, "CLAUDE") ||
			strings.HasPrefix(name, "OPENROUTER") || strings.HasSuffix(name, "_API_KEY")
	})
	return append(append(env, "HOME="+home, "USERPROFILE="+home), extra...)
}

func tofu(exe, dir string, env []string, stdin string, args ...string) ([]byte, error) {
	cmd := exec.Command(exe, args...)
	cmd.Dir, cmd.Env, cmd.Stdin = dir, env, strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("tofu %s: %v: %s", strings.Join(args, " "), err, out[max(0, len(out)-4000):])
	}
	return out, nil
}

func seed(args []string) error {
	flags := flag.NewFlagSet("seed", flag.ExitOnError)
	exe := flags.String("tofu", "", "the tofu that writes the session")
	home := flags.String("home", "", "the template home, made fresh")
	dir := flags.String("project", "", "the mock project, made fresh")
	s := &stub{}
	flags.IntVar(&s.agents, "agents", 1000, "settled sub-agents the session ends with")
	flags.IntVar(&s.batch, "batch", 20, "spawns per lead step, also the running cap")
	flags.IntVar(&s.padMission, "mission-bytes", 3000, "bytes of each spawn brief, as a real brief runs")
	flags.IntVar(&s.padReport, "report-bytes", 3000, "bytes of each hand-back report")
	ceiling := flags.Int("ceiling", 70000, "the lead's context ceiling, low enough that the session rolls over into generations as a long one does")
	_ = flags.Parse(args)
	if *exe == "" || *home == "" || *dir == "" {
		return errors.New("seed needs -tofu, -home and -project")
	}
	for _, made := range []string{*home, *dir} {
		if _, err := os.Stat(made); err == nil {
			return fmt.Errorf("%s exists; seed writes a fresh one", made)
		}
	}
	if err := project(*dir); err != nil {
		return err
	}
	if err := startStub(s); err != nil {
		return err
	}
	env := freshEnv(*home, s.env()...)
	if err := os.MkdirAll(*home, 0o755); err != nil {
		return err
	}
	for _, step := range []struct {
		stdin string
		args  []string
	}{
		{mockLoginCode + "\n", []string{"login", "llm", "claude-sub", "--paste"}},
		{"", []string{"models", "bind", "classifier", "typesafe/jev-latest"}},
		{"", []string{"models", "bind", "orchestrator", model}},
		{"", []string{"settings", "set", "subAgentsPerTurn", strconv.Itoa(s.batch)}},
	} {
		if _, err := tofu(*exe, *dir, env, step.stdin, step.args...); err != nil {
			return err
		}
	}
	started := time.Now()
	_, err := tofu(*exe, *dir, env, "", "run", "--dir", *dir, "--model", model, "--max-steps", strconv.Itoa(s.agents*4), "--no-gate", "--sift", "free",
		"--context-ceiling", strconv.Itoa(*ceiling), leadMarker+" seed: spawn the mock sub-agents")
	fmt.Printf("seeded %d of %d sub-agents, %d reported, %d requests, in %s\n", s.started.Load(), s.agents, s.reported.Load(), s.asked.Load(), time.Since(started).Round(time.Second))
	return err
}

func project(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for k := range 10 {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("mock%d.txt", k)), []byte(fmt.Sprintf("mock line %d\nmock body\n", k)), 0o644); err != nil {
			return err
		}
	}
	for _, git := range [][]string{{"init", "-q"}, {"add", "-A"}, {"-c", "user.name=bench", "-c", "user.email=bench@example.invalid", "commit", "-q", "-m", "seed"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, git...)...).CombinedOutput(); err != nil {
			return fmt.Errorf("git %v: %v: %s", git, err, out)
		}
	}
	return nil
}

func hostPID(exe, dir string, env []string) int {
	var status struct {
		PID int `json:"pid"`
	}
	out, err := tofu(exe, dir, env, "", "host", "status", "--json", "--dir", dir)
	if err != nil || json.Unmarshal(out, &status) != nil {
		return 0
	}
	return status.PID
}

func measure(args []string) error {
	flags := flag.NewFlagSet("measure", flag.ExitOnError)
	arm := flags.String("arm", "", "the arm label")
	exe := flags.String("tofu", "", "the tofu under measurement")
	template := flags.String("template", "", "the seeded home, copied fresh for this run")
	dir := flags.String("project", "", "the mock project the session belongs to")
	work := flags.String("work", "", "where the run home, the profiles and the screen go")
	out := flags.String("out", "", "the jsonl the rows are appended to")
	settle := flags.Duration("settle", 30*time.Second, "time from launch before the idle window opens, not measured")
	idle := flags.Int("idle", 60, "seconds of the idle window: a member, a host, no turn")
	busy := flags.Int("stream", 180, "seconds of the work window: the lead waits on a shell while -workers sub-agents stream")
	s := &stub{}
	flags.IntVar(&s.workers, "workers", 6, "sub-agents that stream through the work window")
	flags.IntVar(&s.padMission, "mission-bytes", 3000, "bytes of each worker's brief")
	rate := flags.Int("rate", 40, "stream deltas a second per worker")
	flags.IntVar(&s.stepDeltas, "step-deltas", 160, "thinking deltas per worker step before its text and tool call")
	flags.IntVar(&s.leadReply, "lead-reply-bytes", 0, "bytes of text the lead streams, a line at a time, before it spawns the workers")
	profiled := flags.Bool("pprof", false, "profile the host and the TUI, which costs CPU of its own: a heap sample every 4 KB allocated")
	_ = flags.Parse(args)
	if *arm == "" || *exe == "" || *template == "" || *dir == "" || *work == "" || *out == "" {
		return errors.New("measure needs -arm, -tofu, -template, -project, -work and -out")
	}
	s.deltaGap = time.Second / time.Duration(*rate)
	s.mode.Store(int32(idling))
	if err := startStub(s); err != nil {
		return err
	}
	label := *arm + "-" + time.Now().Format("150405")
	home, profile := filepath.Join(*work, "home-"+label), filepath.Join(*work, "pprof-"+label)
	if err := os.CopyFS(home, os.DirFS(*template)); err != nil {
		return err
	}
	env := freshEnv(home, s.env()...)
	profiling := func(role string) []string {
		if !*profiled {
			return env
		}
		return append(slices.Clone(env), "TOFU_PPROF="+filepath.Join(profile, role))
	}
	hostLog, err := os.Create(filepath.Join(*work, "host-"+label+".log"))
	if err != nil {
		return err
	}
	defer func() { _ = hostLog.Close() }()
	hosted := exec.Command(*exe, "host", "run", "--dir", *dir)
	hosted.Dir, hosted.Env, hosted.Stdout, hosted.Stderr = *dir, profiling("host"), hostLog, hostLog
	if err := hosted.Start(); err != nil {
		return err
	}
	exited := make(chan error, 1)
	go func() { exited <- hosted.Wait() }()
	defer stopHost(*exe, *dir, env, hosted, exited)
	for deadline := time.Now().Add(hostReady); hostPID(*exe, *dir, env) != hosted.Process.Pid; time.Sleep(time.Second) {
		if time.Now().After(deadline) {
			return fmt.Errorf("host %d did not answer within %s, its log is %s", hosted.Process.Pid, hostReady, hostLog.Name())
		}
	}
	c, err := launch([]string{*exe, "--continue"}, *dir, profiling("app"))
	if err != nil {
		return err
	}
	defer c.close()
	screen := &tail{}
	go c.drain(screen)
	time.Sleep(*settle)
	file, err := os.OpenFile(*out, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	machine, _ := os.Hostname()
	version, _ := tofu(*exe, *dir, env, "", "version")
	ranOn, date := fmt.Sprintf("%s %s/%s %d cpu", machine, runtime.GOOS, runtime.GOARCH, runtime.NumCPU()), time.Now().Format(time.RFC3339)
	write := func(row any) error {
		line, _ := json.Marshal(row)
		_, err := file.Write(append(line, '\n'))
		return err
	}
	var phases []summary
	for _, phase := range []struct {
		name    string
		seconds int
	}{{"idle", *idle}, {"work", *busy}} {
		if phase.name == "work" {
			s.mode.Store(int32(working))
			if err := typed(c, leadMarker+" work: spawn the mock workers"); err != nil {
				return err
			}
		}
		steps := s.workSteps.Load()
		got, err := window(*arm, phase.name, phase.seconds, hosted.Process.Pid, c.pid, write)
		if err != nil {
			return err
		}
		got.Kind, got.Arm, got.Workers, got.Profiled, got.Version, got.Machine, got.Date = "phase", *arm, s.workers, *profiled, strings.TrimSpace(string(version)), ranOn, date
		got.WorkSteps = s.workSteps.Load() - steps
		phases = append(phases, got)
		fmt.Printf("%s %s: host mean %.3f peak %.3f cores, peak %.0f MB working %.0f MB private; tui mean %.3f peak %.3f cores, peak %.0f MB working %.0f MB private; %d worker steps\n",
			label, got.Phase, got.HostMeanCores, got.HostPeakCores, got.HostPeakWorking, got.HostPeakPrivate, got.TUIMeanCores, got.TUIPeakCores, got.TUIPeakWorking, got.TUIPeakPrivate, got.WorkSteps)
	}
	quitted := c.quit(quitWithin)
	fell, again, err := fellBehind(*exe, *dir, env)
	if err != nil {
		return err
	}
	for _, got := range phases {
		got.FellBehind, got.FellAgainWithin = fell, again
		if err := write(got); err != nil {
			return err
		}
	}
	profiles := "no profiles, -pprof was off"
	if *profiled {
		profiles = "profiles in " + profile
	}
	fmt.Printf("%s: %d fell-behind notices, %d of them within a second of the one before; quit by ctrl+c %v; %s\n", label, fell, again, quitted, profiles)
	return os.WriteFile(filepath.Join(*work, "screen-"+label+".ansi"), screen.bytes(), 0o644)
}

func window(arm, phase string, seconds, host, tui int, write func(any) error) (summary, error) {
	got := summary{Phase: phase, Seconds: seconds}
	hostLast, err := readPID(host)
	if err != nil {
		return got, err
	}
	tuiLast, err := readPID(tui)
	if err != nil {
		return got, err
	}
	hostStart, tuiStart := hostLast, tuiLast
	for second := 1; second <= seconds; second++ {
		time.Sleep(time.Second)
		hostNow, err := readPID(host)
		if err != nil {
			return got, err
		}
		tuiNow, err := readPID(tui)
		if err != nil {
			return got, err
		}
		row := sample{Kind: "second", Arm: arm, Phase: phase, Second: second,
			HostCores: (hostNow.cpuSeconds - hostLast.cpuSeconds) / hostNow.at.Sub(hostLast.at).Seconds(), HostWorkingMB: hostNow.workingMB, HostPrivateMB: hostNow.privateMB,
			TUICores: (tuiNow.cpuSeconds - tuiLast.cpuSeconds) / tuiNow.at.Sub(tuiLast.at).Seconds(), TUIWorkingMB: tuiNow.workingMB, TUIPrivateMB: tuiNow.privateMB}
		if err := write(row); err != nil {
			return got, err
		}
		got.HostPeakCores, got.TUIPeakCores = max(got.HostPeakCores, row.HostCores), max(got.TUIPeakCores, row.TUICores)
		got.HostPeakWorking, got.HostPeakPrivate = max(got.HostPeakWorking, row.HostWorkingMB), max(got.HostPeakPrivate, row.HostPrivateMB)
		got.TUIPeakWorking, got.TUIPeakPrivate = max(got.TUIPeakWorking, row.TUIWorkingMB), max(got.TUIPeakPrivate, row.TUIPrivateMB)
		hostLast, tuiLast = hostNow, tuiNow
	}
	got.HostMeanCores = (hostLast.cpuSeconds - hostStart.cpuSeconds) / hostLast.at.Sub(hostStart.at).Seconds()
	got.TUIMeanCores = (tuiLast.cpuSeconds - tuiStart.cpuSeconds) / tuiLast.at.Sub(tuiStart.at).Seconds()
	return got, nil
}

func fellBehind(exe, dir string, env []string) (int, int, error) {
	out, err := tofu(exe, dir, env, "", "session", "trace", "head", "--grep", fellWords, "--json")
	if err != nil {
		return 0, 0, err
	}
	var traced struct {
		Data struct {
			Rows []struct {
				At time.Time `json:"at"`
			} `json:"rows"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out, &traced); err != nil {
		return 0, 0, err
	}
	again := 0
	for index := 1; index < len(traced.Data.Rows); index++ {
		if traced.Data.Rows[index].At.Sub(traced.Data.Rows[index-1].At) < time.Second {
			again++
		}
	}
	return len(traced.Data.Rows), again, nil
}

func stopHost(exe, dir string, env []string, hosted *exec.Cmd, exited chan error) {
	_, _ = tofu(exe, dir, env, "", "host", "stop", "--dir", dir)
	select {
	case <-exited:
	case <-time.After(hostLeaves):
		_ = hosted.Process.Kill()
		<-exited
	}
}

func typed(c *console, said string) error {
	if _, err := c.Write([]byte(said)); err != nil {
		return err
	}
	time.Sleep(time.Second)
	_, err := c.Write([]byte("\r"))
	return err
}

func report(args []string) error {
	if len(args) != 1 {
		return errors.New("report needs the rows jsonl")
	}
	file, err := os.Open(args[0])
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	fmt.Println("| arm | phase | seconds | host mean cores | host peak cores | host peak working MB | host peak private MB | TUI mean cores | TUI peak cores | TUI peak working MB | TUI peak private MB | worker steps | fell behind | again within 1 s |")
	fmt.Println("|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|")
	var conditions []string
	lines := bufio.NewScanner(file)
	for lines.Scan() {
		var got summary
		if err := json.Unmarshal(lines.Bytes(), &got); err != nil {
			return err
		}
		if got.Kind != "phase" {
			continue
		}
		fmt.Printf("| %s | %s | %d | %.3f | %.3f | %.0f | %.0f | %.3f | %.3f | %.0f | %.0f | %d | %d | %d |\n", got.Arm, got.Phase, got.Seconds, got.HostMeanCores, got.HostPeakCores,
			got.HostPeakWorking, got.HostPeakPrivate, got.TUIMeanCores, got.TUIPeakCores, got.TUIPeakWorking, got.TUIPeakPrivate, got.WorkSteps, got.FellBehind, got.FellAgainWithin)
		said := fmt.Sprintf("%s: %s, %d workers, profiled %v, %s, %s", got.Arm, got.Version, got.Workers, got.Profiled, got.Machine, got.Date[:10])
		if !slices.Contains(conditions, said) {
			conditions = append(conditions, said)
		}
	}
	fmt.Println()
	for _, said := range conditions {
		fmt.Println("- " + said)
	}
	return lines.Err()
}

type tail struct {
	mu   sync.Mutex
	held bytes.Buffer
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.held.Write(p)
	if over := t.held.Len() - screenTail; over > screenTail {
		t.held.Next(over)
	}
	return len(p), nil
}

func (t *tail) bytes() []byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	return bytes.Clone(t.held.Bytes())
}
