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
}

type idleRun struct {
	Label    string        `json:"label"`
	Build    string        `json:"build"`
	Command  []string      `json:"command"`
	SettleS  float64       `json:"settle_s"`
	Readings []idleReading `json:"readings"`
	Method   string        `json:"method"`
	Machine  string        `json:"machine"`
	Date     string        `json:"date"`
}

func idle(args []string) error {
	flags := flag.NewFlagSet("idle", flag.ExitOnError)
	label := flags.String("label", "", "the run's name, and its file under runs/ as idle-<label>.json")
	build := flags.String("build", "", "what was built: the commit and anything changed on top of it")
	work := flags.String("work", "", "a scratch directory where each run makes its own fresh home and project")
	runs := flags.Int("runs", 5, "how many fresh starts are measured")
	settle := flags.Duration("settle", 5*time.Second, "how long after start the process is left before it is read")
	pprof := flags.Bool("pprof", false, "set TOFU_PPROF to pprof-<label>-<n> in the work directory, whose heap-peak.pprof attributes what the start allocated")
	_ = flags.Parse(args)
	command := flags.Args()
	if *label == "" || *build == "" || *work == "" || len(command) == 0 {
		return errors.New("idle needs -label, -build, -work and a command after --")
	}
	r := idleRun{Label: *label, Build: *build, Command: append([]string{exeBase(command[0])}, command[1:]...), SettleS: settle.Seconds(), Method: idleMethod, Machine: machine(), Date: time.Now().Format("2006-01-02 15:04")}
	for n := range *runs {
		home, err := os.MkdirTemp(*work, "home-")
		if err != nil {
			return err
		}
		project := filepath.Join(home, "project")
		if out, err := exec.Command("git", "init", "-q", project).CombinedOutput(); err != nil {
			return fmt.Errorf("git init: %v: %s", err, out)
		}
		cmd := exec.Command(command[0], command[1:]...)
		cmd.Dir = project
		pprofDir := ""
		if *pprof {
			pprofDir = filepath.Join(*work, fmt.Sprintf("pprof-%s-%d", *label, n+1))
		}
		cmd.Env = append(benchEnv("", home, "", pprofDir), "APPDATA="+filepath.Join(home, "AppData", "Roaming"), "LOCALAPPDATA="+filepath.Join(home, "AppData", "Local"))
		screen, err := os.Create(filepath.Join(*work, fmt.Sprintf("screen-%s-%d.ansi", *label, n+1)))
		if err != nil {
			return err
		}
		reading, err := idleOnce(cmd, *settle, screen)
		_ = screen.Close()
		if err != nil {
			return err
		}
		r.Readings = append(r.Readings, reading)
		if err := os.RemoveAll(home); err != nil {
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

func (r idleRun) table() string {
	var working, private, peak, cpu []float64
	for _, reading := range r.Readings {
		working = append(working, reading.WorkingSetMB)
		private = append(private, reading.PrivateWorkingSetMB)
		peak = append(peak, reading.PeakWorkingSetMB)
		cpu = append(cpu, reading.CPUPercent)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s, `%s`, %d fresh starts read %.0f s after start, %s, %s\n\n", r.Label, r.Build, strings.Join(r.Command, " "), len(r.Readings), r.SettleS, r.Machine, r.Date)
	b.WriteString("| reading | min | median | max |\n|---|---|---|---|\n")
	fmt.Fprintf(&b, "| working set MB | %s |\n| private working set MB | %s |\n| peak working set MB | %s |\n| CPU while idle, %% of one core | %s |\n", spread(working), spread(private), spread(peak), spread(cpu))
	return b.String()
}
