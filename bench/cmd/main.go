package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	benchapi "tofu/bench/api"
	benchcost "tofu/bench/cost"
	"tofu/bench/report"
	benchturn "tofu/bench/turn"
	benchwording "tofu/bench/wording"
	"tofu/internal/judge/jev"
	"tofu/internal/judge/jev/wire/openrouter"
)

const (
	exitOK    = 0
	exitUsage = 2
)

const usage = `bench measures the instrument and writes a report.

Usage:
  bench <target> [arguments]

Targets:
  api       latency, rerun agreement and cost per decision, from this machine
  cost      jev against two frontier models and a plain regular expression
  wording   the same decision under every wording of its question
  turn      tokens, wall clock and context occupancy for one turn
  harness   run one arm on one task and score it

Arguments:
  --offline   write nothing and call nothing, for api, cost, wording and turn
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, out, errOut io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprint(errOut, usage)
		return exitUsage
	}
	target, rest := args[0], args[1:]
	switch target {
	case "help", "-h", "--help":
		_, _ = fmt.Fprint(out, usage)
		return exitOK
	case "harness":
		return benchHarness(out, errOut, rest)
	}
	produce := reportOf(target)
	if produce == nil {
		return fail(errOut, "bench", fmt.Errorf("unknown target %q", target))
	}
	offline := false
	for _, arg := range rest {
		if arg != "--offline" {
			return fail(errOut, target, fmt.Errorf("unknown argument %q", arg))
		}
		offline = true
	}
	if offline {
		_, _ = fmt.Fprintf(out, "bench %s: skipped, --offline was set, no network call was made\n", target)
		return exitOK
	}
	return writeReport(out, errOut, target, produce)
}

type producer func(ctx context.Context, key string) (path, body string, err error)

func reportOf(target string) producer {
	switch target {
	case "api":
		return func(ctx context.Context, key string) (string, string, error) {
			wire, err := benchapi.NewWire(key)
			if err != nil {
				return "", "", err
			}
			result, err := benchapi.Run(ctx, wire)
			if err != nil {
				return "", "", err
			}
			path := filepath.Join("bench", "report", report.Filename(result.GeneratedAt))
			return path, report.Render(result, conditions(result.GeneratedAt)), nil
		}
	case "cost":
		return func(ctx context.Context, key string) (string, string, error) {
			result, err := benchcost.Run(ctx, key)
			if err != nil {
				return "", "", err
			}
			path := filepath.Join("bench", "cost", benchcost.Filename(result.GeneratedAt))
			return path, benchcost.Render(result, conditions(result.GeneratedAt)), nil
		}
	case "wording":
		return func(ctx context.Context, key string) (string, string, error) {
			result, err := benchwording.Run(ctx, key)
			if err != nil {
				return "", "", err
			}
			path := filepath.Join("bench", "wording", benchwording.Filename(result.GeneratedAt))
			return path, benchwording.Render(result, conditions(result.GeneratedAt)), nil
		}
	case "turn":
		return func(ctx context.Context, key string) (string, string, error) {
			result, err := benchturn.Run(ctx, key)
			if err != nil {
				return "", "", err
			}
			path := filepath.Join("bench", "turn", benchturn.Filename(result.GeneratedAt))
			return path, benchturn.Render(result, conditions(result.GeneratedAt)), nil
		}
	}
	return nil
}

func writeReport(out, errOut io.Writer, target string, produce producer) int {
	key, err := jev.Key(".env")
	if err != nil {
		return fail(errOut, target, err)
	}
	path, body, err := produce(context.Background(), key)
	if err != nil {
		return fail(errOut, target, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fail(errOut, target, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return fail(errOut, target, err)
	}
	_, _ = fmt.Fprint(out, body)
	return exitOK
}

func conditions(generatedAt time.Time) report.Conditions {
	host, err := os.Hostname()
	if err != nil {
		host = "unknown"
	}
	return report.Conditions{
		Machine:        host,
		CredentialKind: "key",
		Wire:           openrouter.Name,
		Date:           generatedAt.Format("2006-01-02"),
	}
}

func fail(errOut io.Writer, target string, err error) int {
	_, _ = fmt.Fprintf(errOut, "bench %s: %v\n", target, err)
	return exitUsage
}

func nextArg(args []string, i *int, flag string) (string, error) {
	*i++
	if *i >= len(args) {
		return "", errors.New(flag + " needs a value")
	}
	return args[*i], nil
}

func nextInt(args []string, i *int, flag string) (int, error) {
	raw, err := nextArg(args, i, flag)
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s %q is not a number", flag, raw)
	}
	return n, nil
}
