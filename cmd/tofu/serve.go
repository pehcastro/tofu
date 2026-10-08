package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"tofu/internal/host"
	"tofu/internal/llm/quota"
	sessionstore "tofu/internal/session"
	"tofu/internal/turn"
)

const serveUsage = `usage: tofu serve --stdio [--dir PATH] [--cassette PATH]
       tofu serve --schema

serves one project to a frontend over standard input and output: JSON-RPC 2.0,
one object a line, in the protocol tofu.host/1. Standard error carries logs and
nothing else. Closing standard input stops the running turn and ends tofu.

--schema prints the JSON Schema of every line tofu writes, with the lines it
reads under $defs.clientMessage.

--cassette PATH answers every model call from a recorded cassette, as tofu
drive does, so a frontend is built and tested with no model and no network.
TOFU_DRIVE_CASSETTE names it when --cassette does not.
`

const percentOfOne = 100

type servePlan struct {
	stdio    bool
	schema   bool
	dir      string
	cassette string
}

func serveArgs(args []string) (servePlan, error) {
	plan := servePlan{cassette: os.Getenv(cassetteVariable)}
	valued := map[string]*string{"--dir": &plan.dir, "--cassette": &plan.cassette}
	for index := 0; index < len(args); index++ {
		target, takesValue := valued[args[index]]
		switch {
		case args[index] == "--stdio":
			plan.stdio = true
		case args[index] == "--schema":
			plan.schema = true
		case !takesValue:
			return plan, fmt.Errorf("unknown argument %q", args[index])
		case index+1 >= len(args):
			return plan, fmt.Errorf("%s wants a value", args[index])
		default:
			index++
			*target = args[index]
		}
	}
	if plan.stdio == plan.schema {
		return plan, errors.New("name exactly one of --stdio and --schema")
	}
	return plan, nil
}

func serveVerb(args []string, in io.Reader, out, errOut io.Writer) int {
	if slices.Contains(args, "--help") || slices.Contains(args, "-h") {
		_, _ = fmt.Fprint(out, serveUsage)
		return exitOK
	}
	plan, err := serveArgs(args)
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "tofu serve: %v\n\n%s", err, serveUsage)
		return exitUsage
	}
	if plan.schema {
		schema, err := host.Schema()
		if err != nil {
			return serveFail(errOut, err)
		}
		_, _ = fmt.Fprintln(out, string(schema))
		return exitOK
	}
	if plan.dir != "" {
		if err := os.Chdir(plan.dir); err != nil {
			return serveFail(errOut, err)
		}
	}
	dir, err := os.Getwd()
	if err != nil {
		return serveFail(errOut, err)
	}
	deck, err := readCassette(plan.cassette)
	if err != nil {
		return serveFail(errOut, err)
	}
	kept, err := quota.NewPoller(nil, time.Now, nil, recordQuotaReading)
	if err != nil {
		return serveFail(errOut, err)
	}
	open, polled := openAppWire, serveQuota(kept)
	if deck != nil {
		open, polled = driveWire(deck), nil
	}
	launch := launchOf(dir, sessionResume{}, true)
	engine := &appEngine{dir: dir, open: open, tabs: launch.tabs}
	live, troubles := host.New(host.Config{Dir: dir, Engine: engine, Shells: launch.registry, Check: cronChecker(dir)})
	for _, line := range append([]string{launch.note, "speaking " + host.Protocol + " for " + dir}, troubles...) {
		if line != "" {
			_, _ = fmt.Fprintln(errOut, "tofu serve: "+line)
		}
	}
	err = host.Serve(host.ServeConfig{Host: live, Dir: dir, In: in, Out: out, Shells: launch.registry, Carry: serveCarry, Verb: serveVerbs(errOut), Quota: polled})
	engine.warm.Close()
	live.Close()
	for _, warning := range turn.EndSession(context.Background(), dir, live.ID(), sessionEndExit) {
		_, _ = fmt.Fprintln(errOut, "tofu serve: "+warning)
	}
	leaveShells(launch.registry)
	if err != nil {
		return serveFail(errOut, err)
	}
	return exitOK
}

func serveFail(errOut io.Writer, err error) int {
	_, _ = fmt.Fprintf(errOut, "tofu serve: %v\n", err)
	return exitVerdict
}

func serveCarry(handle string) (host.Carry, error) {
	store, err := sessionstore.Open()
	if err != nil {
		return host.Carry{}, err
	}
	carry, err := resumeOf(store, handle)
	return carry.hosted(), err
}

func serveVerbs(errOut io.Writer) func([]string) (host.VerbResult, error) {
	return func(args []string) (host.VerbResult, error) {
		var out bytes.Buffer
		run(append(slices.Clone(args), jsonFlag), strings.NewReader(""), &out, errOut)
		var result host.VerbResult
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			return result, fmt.Errorf("tofu %s printed no report: %w", strings.Join(args, " "), err)
		}
		return result, nil
	}
}

func serveQuota(kept *quota.Poller) func() []host.QuotaWindow {
	return func() []host.QuotaWindow {
		var windows []host.QuotaWindow
		for _, read := range quotaFrames(pollCredentials(context.Background(), time.Now, kept)) {
			window := host.QuotaWindow{Account: read.Account, Window: read.Label, Percent: read.Fraction * percentOfOne, Reported: read.Reported}
			if !read.ResetsAt.IsZero() {
				window.ResetsAt = &read.ResetsAt
			}
			windows = append(windows, window)
		}
		return windows
	}
}
