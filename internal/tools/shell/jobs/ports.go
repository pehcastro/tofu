package jobs

import (
	"cmp"
	"context"
	"encoding/json"
	"slices"
	"strings"

	"tofu/internal/tools/shell/process"
)

type listener struct {
	port int
	pid  int
}

type portRow struct {
	Port      int    `json:"port"`
	Free      bool   `json:"free,omitempty"`
	PID       int    `json:"pid,omitempty"`
	Command   string `json:"command,omitempty"`
	StartedBy string `json:"started_by,omitempty"`
	Shell     string `json:"shell,omitempty"`
}

func ports(ctx context.Context, registry *process.Registry, port int) (string, error) {
	held, err := listeners(ctx)
	if err != nil {
		return "", err
	}
	shells, err := registry.List()
	if err != nil {
		return "", err
	}
	own, inTree := registry.Own(), treeOf()
	slices.SortFunc(held, func(a, b listener) int { return cmp.Or(cmp.Compare(a.port, b.port), cmp.Compare(a.pid, b.pid)) })
	var out strings.Builder
	lines := json.NewEncoder(&out)
	for _, one := range slices.Compact(held) {
		if port != 0 && one.port != port {
			continue
		}
		row := portRow{Port: one.port, PID: one.pid, Command: commandOf(ctx, one.pid), StartedBy: "outside"}
		if at := slices.IndexFunc(shells, func(shell process.Shell) bool { return inTree(shell.PID, one.pid) }); at >= 0 {
			row.Shell, row.StartedBy = shells[at].Name, "other_session"
			if slices.ContainsFunc(own, func(mine process.Shell) bool { return mine.Name == row.Shell }) {
				row.StartedBy = "this_session"
			}
		}
		_ = lines.Encode(row)
	}
	if port != 0 && out.Len() == 0 {
		_ = lines.Encode(portRow{Port: port, Free: true})
	}
	return out.String(), nil
}
