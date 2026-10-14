package main

import (
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
)

type idleReading struct {
	WorkingSetMB        float64 `json:"working_set_mb"`
	PrivateWorkingSetMB float64 `json:"private_working_set_mb"`
	PeakWorkingSetMB    float64 `json:"peak_working_set_mb"`
	CPUPercent          float64 `json:"cpu_percent_of_one_core"`
	SwitchesPerS        float64 `json:"context_switches_per_s"`
	MCyclesPerS         float64 `json:"mcycles_per_s"`
	Threads             int     `json:"threads"`
}

type idleRun struct {
	Label      string        `json:"label"`
	Build      string        `json:"build"`
	Command    []string      `json:"command"`
	Home       idleHome      `json:"home"`
	NominalMHz float64       `json:"nominal_mhz"`
	SettleS    float64       `json:"settle_s"`
	WindowS    float64       `json:"window_s"`
	Readings   []idleReading `json:"readings"`
	Method     string        `json:"method"`
	Machine    string        `json:"machine"`
	Date       string        `json:"date"`
}

func envName(kv string) string {
	return strings.ToUpper(strings.SplitN(kv, "=", 2)[0])
}

func withEnv(env, extra []string) []string {
	names := map[string]bool{}
	for _, kv := range extra {
		names[envName(kv)] = true
	}
	return append(slices.DeleteFunc(env, func(kv string) bool { return names[envName(kv)] }), extra...)
}

func idle(args []string) error {
	flags := flag.NewFlagSet("idle", flag.ExitOnError)
	label := flags.String("label", "", "the run's name, and its file under runs/ as idle-<label>.json")
	build := flags.String("build", "", "what was built: the commit and anything changed on top of it")
	work := flags.String("work", "", "a scratch directory where each run makes its own fresh home and project")
	runs := flags.Int("runs", 5, "how many fresh starts are measured")
	settle := flags.Duration("settle", 5*time.Second, "how long after start the process is left before it is read")
	window := flags.Duration("window", 2*time.Second, "how long the idle process is read for")
	home := flags.String("home", string(homeFresh), "fresh: a fresh home on the real network; stub: every URL tofu calls on a local stub; login: the stub, a mock claude-sub login and a mock Jev key, so the interface opens a signed-in chat")
	pprof := flags.Bool("pprof", false, "set TOFU_PPROF to pprof-<label>-<n> in the work directory, whose heap-peak.pprof attributes what the start allocated")
	_ = flags.Parse(args)
	command := flags.Args()
	if *label == "" || *build == "" || *work == "" || len(command) == 0 {
		return errors.New("idle needs -label, -build, -work and a command after --")
	}
	url, err := startStub()
	if err != nil {
		return err
	}
	kind := idleHome(*home)
	stubbed, err := stubEnv(kind, url)
	if err != nil {
		return err
	}
	r := idleRun{Label: *label, Build: *build, Command: append([]string{exeBase(command[0])}, command[1:]...), Home: kind, NominalMHz: nominalMHz(), SettleS: settle.Seconds(), WindowS: window.Seconds(), Method: idleMethod, Machine: machine(), Date: time.Now().Format("2006-01-02 15:04")}
	for n := range *runs {
		dir, err := os.MkdirTemp(*work, "home-")
		if err != nil {
			return err
		}
		project := filepath.Join(dir, "project")
		if out, err := exec.Command("git", "init", "-q", project).CombinedOutput(); err != nil {
			return fmt.Errorf("git init: %v: %s", err, out)
		}
		pprofDir := ""
		if *pprof {
			pprofDir = filepath.Join(*work, fmt.Sprintf("pprof-%s-%d", *label, n+1))
		}
		env := withEnv(benchEnv("", dir, "", pprofDir), slices.Concat(stubbed, []string{"APPDATA=" + filepath.Join(dir, "AppData", "Roaming"), "LOCALAPPDATA=" + filepath.Join(dir, "AppData", "Local")}))
		if kind == homeLogin {
			if err := login(command[0], env); err != nil {
				return err
			}
		}
		cmd := exec.Command(command[0], command[1:]...)
		cmd.Dir, cmd.Env = project, env
		screen, err := os.Create(filepath.Join(*work, fmt.Sprintf("screen-%s-%d.ansi", *label, n+1)))
		if err != nil {
			return err
		}
		reading, err := idleOnce(cmd, *settle, *window, screen)
		_ = screen.Close()
		if err != nil {
			return err
		}
		r.Readings = append(r.Readings, reading)
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
	}
	body, _ := json.MarshalIndent(r, "", "  ")
	if err := os.WriteFile(filepath.Join(here(), "runs", "idle-"+*label+".json"), append(body, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Print(r.table())
	return nil
}

func spread(values []float64) string {
	sorted := slices.Sorted(slices.Values(values))
	return fmt.Sprintf("%.1f | %.1f | %.1f", sorted[0], sorted[len(sorted)/2], sorted[len(sorted)-1])
}

func (r idleRun) column(pick func(idleReading) float64) []float64 {
	var values []float64
	for _, reading := range r.Readings {
		values = append(values, pick(reading))
	}
	return values
}

func (r idleRun) cyclesPercent(reading idleReading) float64 {
	if r.NominalMHz == 0 {
		return 0
	}
	return 100 * reading.MCyclesPerS / r.NominalMHz
}

func (r idleRun) table() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s, `%s`, home %s, %d fresh starts read for %.0f s from %.0f s after start, %s, %.0f MHz nominal, %s\n\n", r.Label, r.Build, strings.Join(r.Command, " "), r.Home, len(r.Readings), r.WindowS, r.SettleS, r.Machine, r.NominalMHz, r.Date)
	b.WriteString("| reading | min | median | max |\n|---|---|---|---|\n")
	for _, row := range []struct {
		name string
		pick func(idleReading) float64
	}{
		{"working set MB", func(x idleReading) float64 { return x.WorkingSetMB }},
		{"private working set MB", func(x idleReading) float64 { return x.PrivateWorkingSetMB }},
		{"peak working set MB", func(x idleReading) float64 { return x.PeakWorkingSetMB }},
		{"CPU while idle by cycles, % of one core", r.cyclesPercent},
		{"CPU while idle by process times, % of one core", func(x idleReading) float64 { return x.CPUPercent }},
		{"context switches per s", func(x idleReading) float64 { return x.SwitchesPerS }},
		{"million CPU cycles per s", func(x idleReading) float64 { return x.MCyclesPerS }},
		{"threads", func(x idleReading) float64 { return float64(x.Threads) }},
	} {
		fmt.Fprintf(&b, "| %s | %s |\n", row.name, spread(r.column(row.pick)))
	}
	return b.String()
}

func middle(values []float64) string {
	sorted := slices.Sorted(slices.Values(values))
	return fmt.Sprintf("%.2f (%.2f to %.2f)", sorted[len(sorted)/2], sorted[0], sorted[len(sorted)-1])
}

func idles(labels []string) error {
	if len(labels) == 0 {
		return errors.New("idles needs the labels of idle runs to set side by side")
	}
	fmt.Print("| run | home | starts | CPU % of one core by cycles | by process times | context switches per s | working set MB |\n|---|---|---|---|---|---|---|\n")
	for _, label := range labels {
		body, err := os.ReadFile(filepath.Join(here(), "runs", "idle-"+label+".json"))
		if err != nil {
			return err
		}
		var r idleRun
		if err := json.Unmarshal(body, &r); err != nil {
			return err
		}
		fmt.Printf("| %s | %s | %d | %s | %s | %s | %s |\n", label, r.Home, len(r.Readings), middle(r.column(r.cyclesPercent)),
			middle(r.column(func(x idleReading) float64 { return x.CPUPercent })), middle(r.column(func(x idleReading) float64 { return x.SwitchesPerS })),
			middle(r.column(func(x idleReading) float64 { return x.WorkingSetMB })))
	}
	return nil
}
