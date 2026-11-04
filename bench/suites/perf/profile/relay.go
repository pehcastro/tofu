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
	"strings"
	"time"

	"tofu/internal/tools/web/browser/driver"
)

const benchHostsKey = `Software\tofu-bench\NativeMessagingHosts`

func relay(args []string) error {
	flags := flag.NewFlagSet("relay", flag.ExitOnError)
	label := flags.String("label", "", "the run's name, and its file under runs/ as idle-<label>.json")
	build := flags.String("build", "", "what was built: the commit and anything changed on top of it")
	work := flags.String("work", "", "a scratch directory where each run makes its own fresh home")
	tofu := flags.String("tofu", "", "a tofu whose browser install, under a bench hosts key, names the relay Chrome would start; without it the command after -- runs as is")
	runs := flags.Int("runs", 3, "how many fresh starts are measured")
	settle := flags.Duration("settle", 30*time.Second, "how long after the hello the process is left before it is read")
	window := flags.Duration("window", 5*time.Minute, "how long the idle process is read for")
	_ = flags.Parse(args)
	command := flags.Args()
	if *label == "" || *build == "" || *work == "" || (*tofu == "") == (len(command) == 0) {
		return errors.New("relay needs -label, -build, -work, and either -tofu or a command after --")
	}
	r := idleRun{Label: *label, Build: *build, Home: homeFresh, NominalMHz: nominalMHz(), SettleS: settle.Seconds(), WindowS: window.Seconds(), Method: relayMethod, Machine: machine(), Date: time.Now().Format("2006-01-02 15:04")}
	for range *runs {
		home, err := os.MkdirTemp(*work, "home-")
		if err != nil {
			return err
		}
		env := withEnv(benchEnv("", home, "", ""), []string{"APPDATA=" + filepath.Join(home, "AppData", "Roaming"), "LOCALAPPDATA=" + filepath.Join(home, "AppData", "Local")})
		started := command
		if *tofu != "" {
			if started, err = installedRelay(*tofu, home, env); err != nil {
				return err
			}
		}
		reading, err := relayOnce(started, env, *settle, *window)
		if *tofu != "" {
			err = errors.Join(err, runIn(env, *tofu, "browser", "--hosts-key", benchHostsKey, "uninstall"))
		}
		if err != nil {
			return err
		}
		r.Command = append([]string{exeBase(started[0])}, started[1:]...)
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

func runIn(env []string, exe string, args ...string) error {
	cmd := exec.Command(exe, args...)
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s %s: %v: %s", exeBase(exe), strings.Join(args, " "), err, out)
	}
	return nil
}

func installedRelay(tofu, home string, env []string) ([]string, error) {
	if err := runIn(env, tofu, "browser", "--hosts-key", benchHostsKey, "install"); err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(driver.ExtensionDir(home)), driver.HostName+".json"))
	var manifest struct {
		Path           string   `json:"path"`
		AllowedOrigins []string `json:"allowed_origins"`
	}
	if err == nil {
		err = json.Unmarshal(raw, &manifest)
	}
	if err == nil && len(manifest.AllowedOrigins) != 1 {
		err = fmt.Errorf("the manifest allows %v, not one origin", manifest.AllowedOrigins)
	}
	if err != nil {
		return nil, err
	}
	started := []string{manifest.Path, manifest.AllowedOrigins[0]}
	if runtime.GOOS == "windows" {
		started = append(started, "--parent-window=0")
	}
	return started, nil
}

func relayOnce(command, env []string, settle, window time.Duration) (idleReading, error) {
	var printed struct {
		Data struct {
			Build string `json:"build"`
		} `json:"data"`
	}
	built := exec.Command(command[0], "browser", "build", "--json")
	built.Env = env
	if out, err := built.Output(); err == nil {
		_ = json.Unmarshal(out, &printed)
	}
	cmd := exec.Command(command[0], command[1:]...)
	var stderr bytes.Buffer
	cmd.Env, cmd.Stderr = env, &stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return idleReading{}, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return idleReading{}, err
	}
	if err := cmd.Start(); err != nil {
		return idleReading{}, err
	}
	heard := make(chan []string, 1)
	go func() {
		var types []string
		for {
			raw, err := driver.ReadMessage(stdout)
			if err != nil {
				heard <- types
				return
			}
			var message struct {
				T string `json:"t"`
			}
			_ = json.Unmarshal(raw, &message)
			types = append(types, message.T)
		}
	}()
	hello, _ := json.Marshal(map[string]any{"t": "hello", "version": driver.ProtocolVersion, "build": printed.Data.Build, "tabs": []driver.Tab{{ID: 1, URL: "https://example.test/", Title: "Example"}}})
	if err := driver.WriteMessage(stdin, hello); err != nil {
		_ = cmd.Process.Kill()
		return idleReading{}, fmt.Errorf("%s took no hello: %v: %s", command[0], err, stderr.String())
	}
	time.Sleep(settle)
	reading, err := readRelay(cmd.Process.Pid, window)
	_ = stdin.Close()
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	select {
	case <-exited:
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		<-exited
	}
	reading.Heard = slices.Compact(<-heard)
	if err != nil {
		return idleReading{}, fmt.Errorf("%s: %v: %s", command[0], err, stderr.String())
	}
	return reading, nil
}
