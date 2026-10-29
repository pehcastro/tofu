//go:build !windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	relayMethod  = "the command starts with its standard input and output as pipes, as Chrome starts a native host, and is sent one native message hello; after the settle time its VmRSS and its private pages from /proc/<pid>/smaps_rollup are read every 100 ms for the window and the median kept, its user and system ticks from /proc/<pid>/stat over the window divided by the wall time, and every thread's voluntary and involuntary context switches from /proc/<pid>/task/*/status at each end of the window, then its standard input is closed"
	ticksPerS    = 100
	kibibyte     = 1024.0
	statCPUAfter = 11
)

type procActivity struct {
	ticks, switches uint64
	threads         int
}

func procFields(path string) (map[string]uint64, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	fields := map[string]uint64{}
	for _, line := range strings.Split(string(raw), "\n") {
		name, value, found := strings.Cut(line, ":")
		if number, err := strconv.ParseUint(strings.TrimSuffix(strings.TrimSpace(value), " kB"), 10, 64); found && err == nil {
			fields[name] = number
		}
	}
	return fields, nil
}

func activityOf(pid int) (procActivity, error) {
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return procActivity{}, err
	}
	after := strings.Fields(string(stat[strings.LastIndexByte(string(stat), ')')+1:]))
	user, _ := strconv.ParseUint(after[statCPUAfter], 10, 64)
	system, _ := strconv.ParseUint(after[statCPUAfter+1], 10, 64)
	tasks, err := filepath.Glob(fmt.Sprintf("/proc/%d/task/*/status", pid))
	a := procActivity{ticks: user + system, threads: len(tasks)}
	for _, task := range tasks {
		fields, err := procFields(task)
		if err != nil {
			return procActivity{}, err
		}
		a.switches += fields["voluntary_ctxt_switches"] + fields["nonvoluntary_ctxt_switches"]
	}
	return a, err
}

func readRelay(pid int, window time.Duration) (idleReading, error) {
	before, err := activityOf(pid)
	if err != nil {
		return idleReading{}, err
	}
	var working, private []float64
	var peak float64
	windowStarted := time.Now()
	for range int(window / sampleEvery) {
		status, err := procFields(fmt.Sprintf("/proc/%d/status", pid))
		if err != nil {
			return idleReading{}, fmt.Errorf("process %d exited before it was read: %w", pid, err)
		}
		rollup, err := procFields(fmt.Sprintf("/proc/%d/smaps_rollup", pid))
		if err != nil {
			return idleReading{}, err
		}
		working = append(working, float64(status["VmRSS"])/kibibyte)
		private = append(private, float64(rollup["Private_Clean"]+rollup["Private_Dirty"])/kibibyte)
		peak = float64(status["VmHWM"]) / kibibyte
		time.Sleep(sampleEvery)
	}
	after, err := activityOf(pid)
	if err != nil {
		return idleReading{}, err
	}
	seconds := time.Since(windowStarted).Seconds()
	slices.Sort(working)
	slices.Sort(private)
	return idleReading{WorkingSetMB: working[len(working)/2], PrivateWorkingSetMB: private[len(private)/2], PeakWorkingSetMB: peak, CPUPercent: 100 * float64(after.ticks-before.ticks) / ticksPerS / seconds,
		SwitchesPerS: float64(after.switches-before.switches) / seconds, Threads: after.threads}, nil
}
