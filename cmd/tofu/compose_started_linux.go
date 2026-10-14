package main

import (
	"bytes"
	"cmp"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const (
	auxvClockTicksKey    = 17
	statStartTicksColumn = 19
)

func processStarted() (time.Time, bool) {
	stat, statErr := os.ReadFile("/proc/self/stat")
	uptime, uptimeErr := os.ReadFile("/proc/uptime")
	vector, vectorErr := unix.Auxv()
	now := time.Now()
	if cmp.Or(statErr, uptimeErr, vectorErr) != nil {
		return time.Time{}, false
	}
	columns := strings.Fields(string(stat[bytes.LastIndexByte(stat, ')')+1:]))
	clock := slices.IndexFunc(vector, func(pair [2]uintptr) bool { return pair[0] == auxvClockTicksKey })
	if len(columns) <= statStartTicksColumn || clock < 0 || vector[clock][1] == 0 {
		return time.Time{}, false
	}
	ticks, ticksErr := strconv.ParseFloat(columns[statStartTicksColumn], 64)
	up, upErr := strconv.ParseFloat(strings.Fields(string(uptime))[0], 64)
	if cmp.Or(ticksErr, upErr) != nil {
		return time.Time{}, false
	}
	return now.Add(-time.Duration((up - ticks/float64(vector[clock][1])) * float64(time.Second))), true
}
