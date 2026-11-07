package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const serveMethod = "the command starts with its standard input and output as pipes, as the desk starts a serve child, in a fresh home and an empty git project, and is sent each line of the -say file; after the settle time its own working set and private working set are read every 100 ms for the window and the median kept, its CPU time over the window divided by the wall time, its threads' context switches and its cycle count read at each end of the window, every line it printed is kept as serve-<label>-<n>.jsonl in the work directory with the methods printed in the window and the usage requests the stub answered over the whole run, then its standard input is closed"

func serve(args []string) error {
	flags := flag.NewFlagSet("serve", flag.ExitOnError)
	label := flags.String("label", "", "the run's name, and its file under runs/ as idle-<label>.json")
	build := flags.String("build", "", "what was built: the commit and anything changed on top of it")
	work := flags.String("work", "", "a scratch directory where each run makes its own fresh home and project")
	say := flags.String("say", "", "a file of json-rpc lines written to the child's standard input as it starts")
	runs := flags.Int("runs", 3, "how many fresh starts are measured")
	settle := flags.Duration("settle", 5*time.Second, "how long after the last line is said the child is left before it is read")
	window := flags.Duration("window", 10*time.Second, "how long the idle child is read for")
	home := flags.String("home", string(homeStub), "fresh, stub or login, as for idle")
	_ = flags.Parse(args)
	command := flags.Args()
	if *label == "" || *build == "" || *work == "" || *say == "" || len(command) == 0 {
		return errors.New("serve needs -label, -build, -work, -say and a command after --")
	}
	said, err := os.ReadFile(*say)
	if err != nil {
		return err
	}
	url, usage, err := startStub()
	if err != nil {
		return err
	}
	kind := idleHome(*home)
	stubbed, err := stubEnv(kind, url)
	if err != nil {
		return err
	}
	r := idleRun{Label: *label, Build: *build, Command: append([]string{exeBase(command[0])}, command[1:]...), Home: kind, NominalMHz: nominalMHz(), SettleS: settle.Seconds(), WindowS: window.Seconds(), Method: serveMethod, Machine: machine(), Date: time.Now().Format("2006-01-02 15:04")}
	for n := range *runs {
		dir, err := os.MkdirTemp(*work, "home-")
		if err != nil {
			return err
		}
		project := filepath.Join(dir, "project")
		if out, err := exec.Command("git", "init", "-q", project).CombinedOutput(); err != nil {
			return fmt.Errorf("git init: %v: %s", err, out)
		}
		env := withEnv(benchEnv("", dir, "", ""), slices.Concat(stubbed, []string{"APPDATA=" + filepath.Join(dir, "AppData", "Roaming"), "LOCALAPPDATA=" + filepath.Join(dir, "AppData", "Local")}))
		if kind == homeLogin {
			if err := login(command[0], env); err != nil {
				return err
			}
		}
		cmd := exec.Command(command[0], command[1:]...)
		cmd.Dir, cmd.Env = project, env
		usage.Store(0)
		reading, err := serveOnce(cmd, said, filepath.Join(*work, fmt.Sprintf("serve-%s-%d.jsonl", *label, n+1)), *settle, *window)
		if err != nil {
			return err
		}
		reading.UsageRequests = usage.Load()
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
	for n, reading := range r.Readings {
		fmt.Printf("start %d: printed in the window %v, usage requests %d\n", n+1, reading.Heard, reading.UsageRequests)
	}
	return nil
}

func serveOnce(cmd *exec.Cmd, said []byte, log string, settle, window time.Duration) (idleReading, error) {
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return idleReading{}, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return idleReading{}, err
	}
	kept, err := os.Create(log)
	if err != nil {
		return idleReading{}, err
	}
	defer func() { _ = kept.Close() }()
	if err := cmd.Start(); err != nil {
		return idleReading{}, err
	}
	type printed struct {
		at     time.Time
		method string
	}
	lines := make(chan []printed, 1)
	go func() {
		var seen []printed
		for scan := bufio.NewScanner(stdout); scan.Scan(); {
			_, _ = kept.Write(append(scan.Bytes(), '\n'))
			var line struct {
				Method string `json:"method"`
			}
			_ = json.Unmarshal(scan.Bytes(), &line)
			seen = append(seen, printed{at: time.Now(), method: line.Method})
		}
		lines <- seen
	}()
	if _, err := stdin.Write(said); err != nil {
		_ = cmd.Process.Kill()
		return idleReading{}, fmt.Errorf("%s took no lines: %v: %s", cmd.Path, err, stderr.String())
	}
	time.Sleep(settle)
	from := time.Now()
	reading, err := readRelay(cmd.Process.Pid, window)
	to := time.Now()
	_ = stdin.Close()
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	select {
	case <-exited:
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		<-exited
	}
	heard := map[string]int{}
	for _, line := range <-lines {
		if line.method != "" && !line.at.Before(from) && !line.at.After(to) {
			heard[line.method]++
		}
	}
	for _, method := range slices.Sorted(maps.Keys(heard)) {
		reading.Heard = append(reading.Heard, fmt.Sprintf("%s x%d", method, heard[method]))
	}
	if err != nil {
		return idleReading{}, fmt.Errorf("%s: %v: %s", cmd.Path, err, strings.TrimSpace(stderr.String()))
	}
	return reading, nil
}
