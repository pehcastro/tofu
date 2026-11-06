//go:build linux

package jobs

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const procListenState = "0A"

func listeners(context.Context) ([]listener, error) {
	ports := map[string]int{}
	for _, table := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		raw, err := os.ReadFile(table)
		if err != nil {
			continue
		}
		for line := range strings.Lines(string(raw)) {
			fields := strings.Fields(line)
			if len(fields) < 10 || fields[3] != procListenState {
				continue
			}
			if port, err := strconv.ParseInt(fields[1][strings.LastIndex(fields[1], ":")+1:], 16, 32); err == nil {
				ports["socket:["+fields[9]+"]"] = int(port)
			}
		}
	}
	links, err := filepath.Glob("/proc/[0-9]*/fd/*")
	if err != nil {
		return nil, err
	}
	var found []listener
	for _, link := range links {
		target, _ := os.Readlink(link)
		if port, held := ports[target]; held {
			pid, _ := strconv.Atoi(strings.Split(link, "/")[2])
			found = append(found, listener{port: port, pid: pid})
		}
	}
	return found, nil
}

func commandOf(_ context.Context, pid int) string {
	raw, _ := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	return strings.TrimSpace(strings.ReplaceAll(string(raw), "\x00", " "))
}
