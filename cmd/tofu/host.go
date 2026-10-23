package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"time"

	"tofu/internal/host"
)

const projectHostUsage = `usage: tofu host ensure|status|stop|run [--dir PATH] [--json]

  ensure  start this project's host unless one answers, then print its status
  status  print the host's pid, socket, project, version and members
  stop    ask the host to exit now
  run     be the host: serve the project's socket until no member is left;
          ensure starts it, nobody needs to
`

func hostRuns(args []string) bool {
	return len(args) > 1 && args[0] == host.HubVerb && args[1] == host.HubRunVerb
}

func projectHostVerb(args []string, out, errOut io.Writer) int {
	usageFailed := func(why string) int {
		_, _ = fmt.Fprintf(errOut, "tofu host: %s\n\n%s", why, projectHostUsage)
		return exitUsage
	}
	if len(args) == 0 {
		return usageFailed("name ensure, status, stop or run")
	}
	asJSON := slices.Contains(args, "--json")
	rest := slices.DeleteFunc(slices.Clone(args[1:]), func(arg string) bool { return arg == "--json" })
	dir := "."
	if len(rest) == 2 && rest[0] == "--dir" {
		dir = rest[1]
	} else if len(rest) > 0 {
		return usageFailed("unknown argument " + strconv.Quote(rest[0]))
	}
	var status host.HubStatus
	var err error
	switch args[0] {
	case "ensure":
		status, err = host.EnsureHub(dir)
	case "status":
		status, err = host.HubStatusOf(dir)
	case "stop":
		status, err = host.StopHub(dir)
	case host.HubRunVerb:
		say := func(line string) {
			_, _ = fmt.Fprintln(errOut, time.Now().Format(time.DateTime)+" tofu host: "+line)
		}
		if err = runHub(dir, say); err != nil {
			say(err.Error())
		}
		if err != nil && !errors.Is(err, host.ErrHubRunning) {
			return exitVerdict
		}
		return exitOK
	default:
		return usageFailed("unknown subcommand " + strconv.Quote(args[0]))
	}
	if err != nil {
		_, _ = fmt.Fprintln(errOut, "tofu host: "+err.Error())
		return exitVerdict
	}
	if asJSON {
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		_ = encoder.Encode(status)
		return exitOK
	}
	_, _ = fmt.Fprintf(out, "pid      %d\nsocket   %s\nproject  %s\nversion  %s\nmembers  %d\nstarted  %s\n",
		status.PID, status.Socket, status.Project, status.Version, status.Members, status.Started.Format(time.DateTime))
	return exitOK
}
