package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"regexp/syntax"
	"strconv"
	"strings"
	"time"

	"tofu/internal/konst"
	"tofu/internal/tools/shell/process"
)

type waited struct {
	Met         bool     `json:"met"`
	AfterMillis int64    `json:"after_ms"`
	MatchedLine string   `json:"matched_line,omitempty"`
	Why         string   `json:"why,omitempty"`
	LastLines   []string `json:"last_lines,omitempty"`
}

type condition func(context.Context) (string, bool)

func parseFor(registry *process.Registry, entry process.Shell, spec string) (condition, error) {
	switch {
	case strings.HasPrefix(spec, "port:"):
		port, err := strconv.Atoi(strings.TrimPrefix(spec, "port:"))
		if err != nil || port <= 0 {
			return nil, fmt.Errorf("for %q names no port: write port:3000", spec)
		}
		return func(ctx context.Context) (string, bool) {
			for _, address := range process.Probe(ctx, port, konst.PortCheckTimeoutMillis*time.Millisecond) {
				if address.Open {
					return fmt.Sprintf("port %d is open on %s, held by %s", port, address.Host, address.Holder()), true
				}
			}
			return "", false
		}, nil
	case strings.HasPrefix(spec, "file:"):
		path := strings.TrimPrefix(spec, "file:")
		if !filepath.IsAbs(path) {
			path = filepath.Join(entry.Dir, path)
		}
		return func(context.Context) (string, bool) {
			info, err := os.Stat(path)
			if err != nil {
				return "", false
			}
			return fmt.Sprintf("%s is present, %d bytes", path, info.Size()), true
		}, nil
	case strings.HasPrefix(spec, "http://") || strings.HasPrefix(spec, "https://"):
		return func(ctx context.Context) (string, bool) {
			request, err := http.NewRequestWithContext(ctx, http.MethodGet, spec, nil)
			if err != nil {
				return "", false
			}
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				return "", false
			}
			_ = response.Body.Close()
			return "GET " + spec + " answered " + response.Status, response.StatusCode == http.StatusOK
		}, nil
	}
	return printedLine(registry, entry.Name, spec)
}

func printedLine(registry *process.Registry, name, spec string) (condition, error) {
	pattern, err := regexp.Compile(spec)
	var bad *syntax.Error
	if errors.As(err, &bad) {
		at := strings.Index(spec, bad.Expr)
		if bad.Code == syntax.ErrMissingParen {
			at = len(spec) - 1
		}
		return nil, fmt.Errorf("for %q is not a regex: %s at character %d", spec, bad.Code, at+1)
	}
	if err != nil {
		return nil, err
	}
	var cursor process.Cursor
	var partial string
	return func(context.Context) (string, bool) {
		text, err := registry.Follow(name, &cursor)
		if err != nil {
			return "", false
		}
		lines := strings.Split(partial+text, "\n")
		partial = lines[len(lines)-1]
		for _, line := range lines[:len(lines)-1] {
			if line = strings.TrimRight(line, "\r"); pattern.MatchString(line) {
				return line, true
			}
		}
		return "", false
	}, nil
}

func awaitFor(ctx context.Context, registry *process.Registry, entry process.Shell, met condition, within time.Duration) (string, *int, error) {
	began := time.Now()
	bounded, cancel := context.WithTimeout(ctx, within)
	defer cancel()
	for {
		if line, ok := met(bounded); ok {
			out, err := json.Marshal(waited{Met: true, AfterMillis: time.Since(began).Milliseconds(), MatchedLine: line})
			return string(out), nil, err
		}
		if entry.State != process.Running || entry.HeldByPerson() || bounded.Err() != nil {
			break
		}
		var err error
		if entry, err = registry.AwaitEnd(bounded, entry.Name, konst.ReadyPollMillis*time.Millisecond); err != nil {
			return "", nil, err
		}
	}
	why := endWords(entry)
	switch {
	case why != "":
	case ctx.Err() != nil:
		why = "the wait was cancelled, still running"
	default:
		why = fmt.Sprintf("timed out after %d ms, still running", within.Milliseconds())
	}
	tail, err := registry.Tail(entry.Name, konst.ShellWaitMissLines)
	if err != nil {
		return "", nil, err
	}
	out, err := json.Marshal(waited{AfterMillis: time.Since(began).Milliseconds(), Why: why, LastLines: strings.FieldsFunc(tail, func(r rune) bool { return r == '\n' })})
	return string(out), entry.ExitCode, err
}

func endWords(entry process.Shell) string {
	switch {
	case entry.HeldByPerson():
		return "was taken over by the person"
	case entry.State == process.Killed:
		return "was stopped"
	case entry.ExitCode != nil:
		return "exited " + strconv.Itoa(*entry.ExitCode)
	case entry.State == process.Exited:
		return "ended, with an exit code tofu did not see"
	}
	return ""
}
