//go:build unix && !linux

package jobs

import (
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"

	"tofu/internal/platform/outside"
)

func listeners(ctx context.Context) ([]listener, error) {
	out, err := outside.Command(ctx, "lsof", "-nP", "-iTCP", "-sTCP:LISTEN", "-Fpn").Output()
	var nothingListens *exec.ExitError
	if err != nil && !errors.As(err, &nothingListens) {
		return nil, err
	}
	var found []listener
	pid := 0
	for line := range strings.Lines(string(out)) {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "p"):
			pid, _ = strconv.Atoi(line[1:])
		case strings.HasPrefix(line, "n"):
			if port, err := strconv.Atoi(line[strings.LastIndex(line, ":")+1:]); err == nil {
				found = append(found, listener{port: port, pid: pid})
			}
		}
	}
	return found, nil
}

func commandOf(ctx context.Context, pid int) string {
	out, _ := outside.Command(ctx, "ps", "-o", "command=", "-p", strconv.Itoa(pid)).Output()
	return strings.TrimSpace(string(out))
}
