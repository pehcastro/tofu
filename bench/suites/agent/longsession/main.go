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
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	screenColumns     = 160
	screenRows        = 45
	mebibyte          = 1 << 20
	sampleEvery       = 5 * time.Second
	quitWithin        = 90 * time.Second
	mockLoginCode     = "mock-login-code"
	model             = "claude-sub/claude-sonnet-5"
	screenTail        = 256 << 10
	quitPresses       = 6
	seedStepsPerAgent = 4
	seedMinutesCap    = 60
	workPace          = 4500 * time.Millisecond
	hostLeaves        = 10 * time.Second
	hostReady         = 10 * time.Second
	liveArm           = "live450"
	buildProcs        = "4"
	spawnBatch        = 20
)

type reading struct {
	at                               time.Time
	workingMB, privateMB, cpuSeconds float64
}

type row struct {
	Arm           string  `json:"arm"`
	Phase         string  `json:"phase"`
	Agents        int     `json:"agents"`
	Workers       int     `json:"workers"`
	Minute        int     `json:"minute"`
	WorkingMB     float64 `json:"working_mb"`
	PrivateMB     float64 `json:"private_mb"`
	CPUSeconds    float64 `json:"cpu_s"`
	Cores         float64 `json:"cores"`
	Requests      int64   `json:"requests"`
	WorkSteps     int64   `json:"work_steps"`
	Resumed       int64   `json:"resumed"`
	HostPID       int     `json:"host_pid"`
	HostWorkingMB float64 `json:"host_working_mb"`
	HostPrivateMB float64 `json:"host_private_mb"`
	HostCPU       float64 `json:"host_cpu_s"`
	HostCores     float64 `json:"host_cores"`
	Version       string  `json:"version"`
	Commit        string  `json:"commit"`
	Credential    string  `json:"credential"`
	Wire          string  `json:"wire"`
	GOMAXPROCS    string  `json:"gomaxprocs"`
	Machine       string  `json:"machine"`
	Date          string  `json:"date"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: longsession live450|seed|measure|report [flags]")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case liveArm:
		err = live(os.Args[2:])
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
		fmt.Fprintln(os.Stderr, "longsession:", err)
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

func tofu(exe, dir string, env []string, stdin string, args ...string) error {
	cmd := exec.Command(exe, args...)
	cmd.Dir, cmd.Env, cmd.Stdin = dir, env, strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tofu %s: %v: %s", strings.Join(args, " "), err, out[max(0, len(out)-4000):])
	}
	return nil
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

func seed(args []string) error {
	flags := flag.NewFlagSet("seed", flag.ExitOnError)
	exe := flags.String("tofu", "", "the tofu that writes the session")
	home := flags.String("home", "", "the template home, made fresh")
	dir := flags.String("project", "", "the mock project, made fresh")
	agents := flags.Int("agents", 450, "sub-agents the lead spawns")
	batch := flags.Int("batch", spawnBatch, "spawns per lead step, also the running cap")
	ceiling := flags.Int("ceiling", 0, "the lead's context ceiling in tokens, low enough to fork the session into generations as a long session does; 0 keeps the default")
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
	server, err := startStub(*agents, *batch, 0, time.Second)
	if err != nil {
		return err
	}
	env := freshEnv(*home, server.env()...)
	if err := signedIn(*exe, *home, *dir, env, *batch); err != nil {
		return err
	}
	started := time.Now()
	run := []string{"run", "--dir", *dir, "--model", model, "--max-steps", strconv.Itoa(*agents * seedStepsPerAgent), "--no-gate", "--sift", "free"}
	if *ceiling > 0 {
		run = append(run, "--context-ceiling", strconv.Itoa(*ceiling))
	}
	err = tofu(*exe, *dir, env, "", append(run, leadMarker+" seed: spawn the mock sub-agents")...)
	fmt.Printf("seeded %d of %d sub-agents, %d reported, %d requests, in %s\n", server.started.Load(), *agents, server.reported.Load(), server.asked.Load(), time.Since(started).Round(time.Second))
	return err
}

func versionOf(exe string) (string, string) {
	out, _ := exec.Command(exe, "version").Output()
	fields := strings.Fields(string(out))
	if len(fields) < 4 {
		return "unknown", "unknown"
	}
	return fields[1], fields[3]
}

func measure(args []string) error {
	flags := flag.NewFlagSet("measure", flag.ExitOnError)
	arm := flags.String("arm", "", "the arm label")
	exe := flags.String("tofu", "", "the tofu under measurement")
	template := flags.String("template", "", "the seeded home, copied fresh for this run")
	dir := flags.String("project", "", "the mock project the session belongs to")
	work := flags.String("work", "", "where the run home, the profile and the screen go")
	phases := flags.String("phases", "idle", "in order, one process: seed spawns -agents in the app until every one reports, idle waits, message has the lead message one settled sub-agent, which resumes from disk and answers, work has -workers sub-agents work while the lead waits on a shell")
	minutes := flags.Int("minutes", 10, "the window of idle, message and work")
	settle := flags.Duration("settle", 30*time.Second, "time from launch before the first window opens, not measured")
	agents := flags.Int("agents", 450, "sub-agents in the session, spawned by seed or already in the template")
	batch := flags.Int("batch", spawnBatch, "spawns per lead step in seed")
	workers := flags.Int("workers", 1, "sub-agents that work through the work phase")
	extra := flags.String("env", "", "KEY=VALUE pairs, space separated, the arm under measurement reads")
	out := flags.String("out", "", "the jsonl the rows are appended to")
	_ = flags.Parse(args)
	if *arm == "" || *exe == "" || *template == "" || *dir == "" || *work == "" || *out == "" {
		return errors.New("measure needs -arm, -tofu, -template, -project, -work and -out")
	}
	server, err := startStub(*agents, *batch, *workers, workPace)
	if err != nil {
		return err
	}
	server.mode.Store(int32(idling))
	label := fmt.Sprintf("%s-%s", *arm, time.Now().Format("150405"))
	home, profile := filepath.Join(*work, "home-"+label), filepath.Join(*work, "pprof-"+label)
	if err := os.CopyFS(home, os.DirFS(*template)); err != nil {
		return err
	}
	version, commit := versionOf(*exe)
	machine, _ := os.Hostname()
	base := row{Arm: *arm, Agents: *agents, Workers: *workers, Version: version, Commit: commit, Credential: "mock claude-sub login against a local stub",
		Wire: "anthropic messages from a local stub, no model", GOMAXPROCS: "unset, the runtime default of " + strconv.Itoa(runtime.NumCPU()),
		Machine: fmt.Sprintf("%s %s/%s %d cpu", machine, runtime.GOOS, runtime.GOARCH, runtime.NumCPU()), Date: time.Now().Format(time.RFC3339)}
	env := freshEnv(home, append(server.env(), strings.Fields(*extra)...)...)
	hostLog, err := os.Create(filepath.Join(*work, "host-"+label+".log"))
	if err != nil {
		return err
	}
	defer func() { _ = hostLog.Close() }()
	served := &hosts{exe: *exe, dir: *dir, profile: profile, env: env, log: hostLog}
	defer served.stop()
	hostPID, err := served.current()
	if err != nil {
		return err
	}
	c, err := launch([]string{*exe, "--continue"}, *dir, append(slices.Clone(env), "TOFU_PPROF="+filepath.Join(profile, "app")))
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
	last, err := c.read()
	if err != nil {
		return err
	}
	hostLast, _ := readPID(hostPID)
	asked, steps := server.asked.Load(), server.workSteps.Load()
	fmt.Printf("%s opens: working %.0f MB, private %.0f MB, cpu %.1f s; host pid %d private %.0f MB, cpu %.1f s; after %s settle (not measured)\n",
		label, last.workingMB, last.privateMB, last.cpuSeconds, hostPID, hostLast.privateMB, hostLast.cpuSeconds, *settle)
	for _, phase := range strings.Split(*phases, ",") {
		window, said := *minutes, ""
		switch phase {
		case "seed":
			server.mode.Store(int32(seeding))
			window, said = seedMinutesCap, leadMarker+" seed: spawn the mock sub-agents"
		case "idle":
			server.mode.Store(int32(idling))
		case "message":
			server.mode.Store(int32(messaging))
			said = leadMarker + " message: message one settled mock sub-agent"
		case "work":
			server.mode.Store(int32(working))
			said = leadMarker + " work: spawn one mock worker"
		default:
			return fmt.Errorf("unknown phase %q", phase)
		}
		serving, err := served.current()
		if err != nil {
			return err
		}
		if serving != hostPID {
			hostPID, hostLast = serving, reading{}
		}
		if err := typed(c, said); err != nil {
			return err
		}
		for minute := 1; minute <= window; minute++ {
			var now reading
			for range int(time.Minute / sampleEvery) {
				time.Sleep(sampleEvery)
				if now, err = c.read(); err != nil {
					return err
				}
			}
			r := base
			r.Phase, r.Minute, r.WorkingMB, r.PrivateMB, r.CPUSeconds = phase, minute, now.workingMB, now.privateMB, now.cpuSeconds-last.cpuSeconds
			r.Cores = r.CPUSeconds / now.at.Sub(last.at).Seconds()
			r.Requests, r.WorkSteps, r.Resumed = server.asked.Load()-asked, server.workSteps.Load()-steps, server.resumed.Load()
			hostNow, err := readPID(hostPID)
			if serving := hostOf(*exe, *dir, env); err != nil || serving != hostPID {
				return fmt.Errorf("host %d no longer serves the project at %s minute %d, host %d does, so the row would read the wrong process: %v", hostPID, phase, minute, serving, err)
			}
			r.HostPID, r.HostWorkingMB, r.HostPrivateMB, r.HostCPU = hostPID, hostNow.workingMB, hostNow.privateMB, hostNow.cpuSeconds-hostLast.cpuSeconds
			r.HostCores = r.HostCPU / now.at.Sub(last.at).Seconds()
			asked, steps, last, hostLast = server.asked.Load(), server.workSteps.Load(), now, hostNow
			line, _ := json.Marshal(r)
			if _, err := file.Write(append(line, '\n')); err != nil {
				return err
			}
			fmt.Printf("%s %s minute %2d: app working %5.0f MB private %5.0f MB cpu %6.1f s cores %.2f  host %d working %5.0f MB private %5.0f MB cpu %6.1f s  requests %d  worker steps %d  reported %d  resumed %d\n",
				label, phase, minute, r.WorkingMB, r.PrivateMB, r.CPUSeconds, r.Cores, r.HostPID, r.HostWorkingMB, r.HostPrivateMB, r.HostCPU, r.Requests, r.WorkSteps, server.reported.Load(), r.Resumed)
			if phase == "seed" && server.reported.Load() >= int64(*agents) {
				break
			}
		}
	}
	quitted := c.quit(quitWithin)
	hostStayed := hostPID
	for deadline := time.Now().Add(hostLeaves); hostStayed != 0 && time.Now().Before(deadline); time.Sleep(time.Second) {
		hostStayed = hostOf(*exe, *dir, env)
	}
	fmt.Printf("%s quit by ctrl+c: exited %v, host %d gone within %s: %v, app profile in %s\n",
		label, quitted, hostPID, hostLeaves, hostStayed == 0, filepath.Join(profile, "app"))
	return os.WriteFile(filepath.Join(*work, "screen-"+label+".ansi"), screen.bytes(), 0o644)
}

func typed(c *console, said string) error {
	if said == "" {
		return nil
	}
	if _, err := c.Write([]byte(said)); err != nil {
		return err
	}
	time.Sleep(time.Second)
	_, err := c.Write([]byte("\r"))
	return err
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
