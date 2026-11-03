package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"time"
)

type hosts struct {
	exe, dir, profile string
	env               []string
	log               *os.File
	started           []*exec.Cmd
}

func hostOf(exe, dir string, env []string) int {
	var status struct {
		PID int `json:"pid"`
	}
	cmd := exec.Command(exe, "host", "status", "--json", "--dir", dir)
	cmd.Dir, cmd.Env = dir, env
	out, err := cmd.Output()
	if err != nil || json.Unmarshal(out, &status) != nil {
		return 0
	}
	return status.PID
}

func (h *hosts) current() (int, error) {
	serving := hostOf(h.exe, h.dir, h.env)
	ours := slices.IndexFunc(h.started, func(cmd *exec.Cmd) bool { return cmd.Process.Pid == serving })
	switch {
	case serving != 0 && ours >= 0:
		return serving, nil
	case serving != 0:
		return 0, fmt.Errorf("host %d serves the project and this run did not start it, so it inherited the app's TOFU_PPROF and their profiles share one folder", serving)
	}
	profile := filepath.Join(h.profile, "host-"+strconv.Itoa(len(h.started)+1))
	hosted := exec.Command(h.exe, "host", "run", "--dir", h.dir)
	hosted.Dir, hosted.Env, hosted.Stdout, hosted.Stderr = h.dir, append(slices.Clone(h.env), "TOFU_PPROF="+profile), h.log, h.log
	if err := hosted.Start(); err != nil {
		return 0, err
	}
	h.started = append(h.started, hosted)
	for deadline := time.Now().Add(hostReady); hostOf(h.exe, h.dir, h.env) != hosted.Process.Pid; time.Sleep(time.Second) {
		if time.Now().After(deadline) {
			return 0, fmt.Errorf("host %d did not answer within %s, its log is %s", hosted.Process.Pid, hostReady, h.log.Name())
		}
	}
	fmt.Printf("host %d started by this run, profile in %s\n", hosted.Process.Pid, profile)
	return hosted.Process.Pid, nil
}

func (h *hosts) stop() {
	_ = tofu(h.exe, h.dir, h.env, "", "host", "stop", "--dir", h.dir)
	for _, hosted := range h.started {
		exited := make(chan error, 1)
		go func() { exited <- hosted.Wait() }()
		select {
		case <-exited:
		case <-time.After(hostLeaves):
			_ = hosted.Process.Kill()
			<-exited
		}
	}
}
