//go:build !windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const treeMethod = "The tree is every process under the root found in /proc at each sample: RSS is the sum of their resident pages, CPU is the root's rusage after wait, which counts every descendant it reaped; no commit peak"

type tree struct{ root int }

func startTree(cmd *exec.Cmd) (tree, error) {
	if err := cmd.Start(); err != nil {
		return tree{}, err
	}
	return tree{root: cmd.Process.Pid}, nil
}

func (t tree) sample() (rss uint64, members int) {
	stats, _ := filepath.Glob("/proc/[0-9]*/stat")
	parent := map[int]int{}
	resident := map[int]uint64{}
	for _, path := range stats {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		fields := strings.Fields(string(raw[strings.LastIndexByte(string(raw), ')')+2:]))
		pid, _ := strconv.Atoi(filepath.Base(filepath.Dir(path)))
		parent[pid], _ = strconv.Atoi(fields[1])
		pages, _ := strconv.ParseUint(fields[21], 10, 64)
		resident[pid] = pages * uint64(os.Getpagesize())
	}
	inTree := map[int]bool{t.root: true}
	for grew := true; grew; {
		grew = false
		for pid, up := range parent {
			if inTree[up] && !inTree[pid] {
				inTree[pid], grew = true, true
			}
		}
	}
	for pid := range inTree {
		if bytes, alive := resident[pid]; alive {
			rss += bytes
			members++
		}
	}
	return rss, members
}

func (t tree) totals(state *os.ProcessState) (cpu time.Duration, peakCommit uint64) {
	return state.UserTime() + state.SystemTime(), 0
}

func (t tree) close() {}
