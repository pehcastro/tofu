package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	mechanism    = "cgroup v2"
	cgroupRoot   = "/sys/fs/cgroup"
	cgroupParent = "/sys/fs/cgroup/bench-minimal"
	cpuPeriodUS  = 100000
	kibibyte     = 1 << 10
	removeTries  = 50
)

type cage struct {
	dir string
	fd  *os.File
}

func writeKnob(dir, knob, value string) error {
	return os.WriteFile(filepath.Join(dir, knob), []byte(value), 0o644)
}

func confine(cmd *exec.Cmd, limit limits) (cage, error) {
	if err := os.MkdirAll(cgroupParent, 0o755); err != nil {
		return cage{}, err
	}
	controllers := "+cpu +cpuset +memory +pids"
	if err := errors.Join(writeKnob(cgroupRoot, "cgroup.subtree_control", controllers), writeKnob(cgroupParent, "cgroup.subtree_control", controllers)); err != nil {
		return cage{}, err
	}
	dir := filepath.Join(cgroupParent, fmt.Sprintf("run-%d", time.Now().UnixNano()))
	if err := os.Mkdir(dir, 0o755); err != nil {
		return cage{}, err
	}
	if limit.MemoryMB > 0 {
		if err := errors.Join(writeKnob(dir, "memory.max", strconv.Itoa(limit.MemoryMB*mebibyte)), writeKnob(dir, "memory.swap.max", "0")); err != nil {
			return cage{}, err
		}
	}
	if limit.CPU >= 0 {
		if err := errors.Join(writeKnob(dir, "cpu.max", fmt.Sprintf("%d %d", cpuPeriodUS, cpuPeriodUS)), writeKnob(dir, "cpuset.cpus", strconv.Itoa(limit.CPU))); err != nil {
			return cage{}, err
		}
	}
	fd, err := os.Open(dir)
	if err != nil {
		return cage{}, err
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{UseCgroupFD: true, CgroupFD: int(fd.Fd())}
	return cage{dir: dir, fd: fd}, cmd.Start()
}

func readNumber(path string) uint64 {
	raw, _ := os.ReadFile(path)
	value, _ := strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 64)
	return value
}

func keyed(path, key string) uint64 {
	raw, _ := os.ReadFile(path)
	for _, line := range strings.Split(string(raw), "\n") {
		if fields := strings.Fields(line); len(fields) > 1 && fields[0] == key {
			number, _ := strconv.ParseUint(fields[1], 10, 64)
			return number
		}
	}
	return 0
}

func (c cage) sample() reading {
	now := reading{chargedBytes: readNumber(filepath.Join(c.dir, "memory.current")), privateBytes: keyed(filepath.Join(c.dir, "memory.stat"), "anon"),
		rss: map[string]uint64{}, processes: map[string]int{}}
	raw, _ := os.ReadFile(filepath.Join(c.dir, "cgroup.procs"))
	for _, pid := range strings.Fields(string(raw)) {
		comm, err := os.ReadFile(filepath.Join("/proc", pid, "comm"))
		if err != nil {
			continue
		}
		name := strings.TrimSpace(string(comm))
		now.processes[name]++
		line, _ := os.ReadFile(filepath.Join("/proc", pid, "cmdline"))
		now.commands = append(now.commands, strings.TrimSpace(strings.ReplaceAll(string(line), "\x00", " ")))
		now.rss[name] += keyed(filepath.Join("/proc", pid, "status"), "VmRSS:") * kibibyte
	}
	return now
}

func (c cage) finish() finished {
	done := finished{
		cpu:       time.Duration(keyed(filepath.Join(c.dir, "cpu.stat"), "usage_usec")) * time.Microsecond,
		peakBytes: readNumber(filepath.Join(c.dir, "memory.peak")),
		oomKills:  int(keyed(filepath.Join(c.dir, "memory.events"), "oom_kill")),
	}
	_ = writeKnob(c.dir, "cgroup.kill", "1")
	_ = c.fd.Close()
	for range removeTries {
		if os.Remove(c.dir) == nil {
			break
		}
		time.Sleep(sampleEvery)
	}
	return done
}
