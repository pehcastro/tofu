package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	benchapi "boji/bench/api"
	benchcost "boji/bench/cost"
	"boji/bench/report"
	benchwording "boji/bench/wording"
	"boji/internal/judge/jev"
	"boji/internal/judge/jev/wire/openrouter"
)

func benchVerb(args []string, out, errOut io.Writer) int {
	if len(args) == 0 {
		return benchFail(errOut, "bench", errors.New(`a target is required, e.g. "boji bench api"`))
	}
	offline := false
	target := args[0]
	for _, arg := range args[1:] {
		if arg != "--offline" {
			return benchFail(errOut, "bench", fmt.Errorf("unknown argument %q", arg))
		}
		offline = true
	}
	switch target {
	case "api":
		return benchAPI(out, errOut, offline)
	case "cost":
		return benchCost(out, errOut, offline)
	case "wording":
		return benchWording(out, errOut, offline)
	}
	return benchFail(errOut, "bench", fmt.Errorf("unknown target %q", target))
}

func benchAPI(out, errOut io.Writer, offline bool) int {
	if offline {
		_, _ = fmt.Fprintln(out, "boji bench api: skipped, --offline was set, no network call was made")
		return exitOK
	}

	key, err := jev.Key(".env")
	if err != nil {
		return benchFail(errOut, "api", err)
	}
	wire, err := benchapi.NewWire(key)
	if err != nil {
		return benchFail(errOut, "api", err)
	}
	result, err := benchapi.Run(context.Background(), wire)
	if err != nil {
		return benchFail(errOut, "api", err)
	}

	body := report.Render(result, benchConditions(result.GeneratedAt))

	if err := os.MkdirAll("bench/report", 0o755); err != nil {
		return benchFail(errOut, "api", err)
	}
	path := "bench/report/" + report.Filename(result.GeneratedAt)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return benchFail(errOut, "api", err)
	}

	_, _ = fmt.Fprint(out, body)
	return exitOK
}

func benchCost(out, errOut io.Writer, offline bool) int {
	if offline {
		_, _ = fmt.Fprintln(out, "boji bench cost: skipped, --offline was set, no network call was made")
		return exitOK
	}

	key, err := jev.Key(".env")
	if err != nil {
		return benchFail(errOut, "cost", err)
	}
	result, err := benchcost.Run(context.Background(), key)
	if err != nil {
		return benchFail(errOut, "cost", err)
	}

	body := benchcost.Render(result, benchConditions(result.GeneratedAt))

	if err := os.MkdirAll("bench/cost", 0o755); err != nil {
		return benchFail(errOut, "cost", err)
	}
	path := "bench/cost/" + benchcost.Filename(result.GeneratedAt)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return benchFail(errOut, "cost", err)
	}

	_, _ = fmt.Fprint(out, body)
	return exitOK
}

func benchWording(out, errOut io.Writer, offline bool) int {
	if offline {
		_, _ = fmt.Fprintln(out, "boji bench wording: skipped, --offline was set, no network call was made")
		return exitOK
	}

	key, err := jev.Key(".env")
	if err != nil {
		return benchFail(errOut, "wording", err)
	}
	result, err := benchwording.Run(context.Background(), key)
	if err != nil {
		return benchFail(errOut, "wording", err)
	}

	body := benchwording.Render(result, benchConditions(result.GeneratedAt))

	if err := os.MkdirAll("bench/wording", 0o755); err != nil {
		return benchFail(errOut, "wording", err)
	}
	path := "bench/wording/" + benchwording.Filename(result.GeneratedAt)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return benchFail(errOut, "wording", err)
	}

	_, _ = fmt.Fprint(out, body)
	return exitOK
}

func benchConditions(generatedAt time.Time) report.Conditions {
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

func benchFail(errOut io.Writer, target string, err error) int {
	_, _ = fmt.Fprintf(errOut, "boji bench %s: %v\n", target, err)
	return exitUsage
}
