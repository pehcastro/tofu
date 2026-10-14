//go:build !windows

package main

import (
	"errors"
	"io"
	"os/exec"
	"strings"
	"time"
)

const treeMethod = "not measured: the per-process tree split is written for Windows job objects only"

func observe(*exec.Cmd) (observation, error) {
	return observation{}, errors.New("bench/profile splits a process tree on Windows only")
}

const idleMethod = "not measured: the idle reading starts the command in a Windows pseudo console"

func idleOnce(*exec.Cmd, time.Duration, io.Writer) (idleReading, error) {
	return idleReading{}, errors.New("bench/profile reads an idle process on Windows only")
}

func splitCommandLine(line string) []string {
	return strings.Fields(line)
}
