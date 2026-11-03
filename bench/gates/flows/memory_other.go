//go:build !windows

package main

import (
	"os/exec"
	"strconv"
	"strings"
)

const memoryKind = "resident"

func memoryMB(pid int) (float64, error) {
	out, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0, err
	}
	kilobytes, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	return kilobytes / 1024, err
}
