package sift

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"time"
)

const repoRoot = "../.."

type RtkFilter string

const NoRtkFilter RtkFilter = ""

func RtkFilterFor(command string) RtkFilter {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return NoRtkFilter
	}
	switch fields[0] {
	case "git":
		return gitFilter(fields[1:])
	case "grep":
		return "grep"
	case "rg":
		return "rg"
	case "find":
		return "find"
	case "fd":
		return "fd"
	}
	return NoRtkFilter
}

func gitFilter(rest []string) RtkFilter {
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "-C", "-c", "--git-dir", "--work-tree":
			i++
		case "log":
			return "git-log"
		case "status":
			return "git-status"
		case "diff":
			return "git-diff"
		default:
			if !strings.HasPrefix(rest[i], "-") {
				return NoRtkFilter
			}
		}
	}
	return NoRtkFilter
}

type RtkRun struct {
	Filter  RtkFilter
	Output  string
	Elapsed time.Duration
}

func RunRtk(ctx context.Context, command, output string) (RtkRun, error) {
	filter := RtkFilterFor(command)
	if filter == NoRtkFilter {
		return RtkRun{Output: output}, nil
	}
	proxy := exec.CommandContext(ctx, "rtk", "pipe", "-f", string(filter))
	proxy.Dir = repoRoot
	proxy.Stdin = strings.NewReader(output)
	var out bytes.Buffer
	proxy.Stdout = &out
	started := time.Now()
	err := proxy.Run()
	elapsed := time.Since(started)
	if err != nil {
		return RtkRun{}, err
	}
	return RtkRun{Filter: filter, Output: out.String(), Elapsed: elapsed}, nil
}
