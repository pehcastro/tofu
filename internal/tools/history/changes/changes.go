package changes

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"tofu/internal/agent/subagents"
	"tofu/internal/konst"
	"tofu/internal/models/llm/core"
	"tofu/internal/store/session"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/history/shadow"
)

const Name = "changes"

type Changes struct {
	Store    shadow.Store
	Sessions *session.Store
}

func (Changes) Name() string { return Name }

func (Changes) Definition() core.Tool {
	return core.Tool{
		Name: Name,
		Description: "lists what this turn changed in the project, read from the shadow store tofu commits at every turn's start and end, never from the project's own git: " +
			"each file with lines added and removed and the ids of the calls that named it, and every file outside the owns the turn holds. " +
			"a file the person changed between turns is not listed. " +
			fmt.Sprintf("turns n lists the last n ended turns of every session instead, newest first, each under its session id, at most %d. ", konst.ChangesTurnsShown) +
			"of parent lists what the turn that spawned this one has changed so far, while it is still running; a sub-agent reviewing the lead's work calls it that way.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"turns": map[string]any{"type": "integer", "minimum": 1, "maximum": konst.ChangesTurnsShown},
				"of":    map[string]any{"type": "string", "enum": []string{ofParent}},
			},
		},
	}
}

const ofParent = "parent"

func (c Changes) Run(ctx context.Context, raw json.RawMessage) (contract.Result, error) {
	var args struct {
		Turns int    `json:"turns"`
		Of    string `json:"of"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return contract.Result{}, fmt.Errorf("changes: arguments are not the expected shape: %w", err)
		}
	}
	if args.Turns < 0 || args.Turns > konst.ChangesTurnsShown {
		return contract.Result{}, fmt.Errorf("changes: turns is %d, and it lists 1 to %d turns", args.Turns, konst.ChangesTurnsShown)
	}
	timed, cancel := context.WithTimeout(ctx, konst.ShadowSnapshotTimeoutMillis*time.Millisecond)
	defer cancel()
	var turns []shadow.Changed
	var err error
	started, inTurn := shadow.StartedIn(ctx)
	switch {
	case args.Of == "":
	case args.Of != ofParent:
		return contract.Result{}, fmt.Errorf("changes: of is %q, and the only value is %s", args.Of, ofParent)
	case args.Turns != 0:
		return contract.Result{}, errors.New("changes: of parent reads one running turn, so it takes no turns")
	default:
		if started, inTurn = started.Parent(); !inTurn {
			return contract.Result{}, errors.New("changes: of parent reads the turn that spawned this one, and no turn spawned this one")
		}
	}
	command := "last ended turn"
	if args.Turns > 1 {
		command = fmt.Sprintf("last %d ended turns", args.Turns)
	}
	if inTurn && args.Turns == 0 {
		command = cmp.Or(args.Of, "this") + " turn"
		var live shadow.Changed
		live, err = started.Live(timed)
		turns = []shadow.Changed{live}
	} else {
		turns, err = c.Store.Turns(timed, cmp.Or(args.Turns, 1))
	}
	if err != nil {
		return contract.Result{}, fmt.Errorf("changes: %w", err)
	}
	if len(turns) == 0 {
		return contract.Result{Content: "no turn has been recorded in the shadow store at " + c.Store.Dir() + " yet\n", Command: command}, nil
	}
	var said strings.Builder
	for _, turn := range turns {
		state := "ended"
		switch {
		case inTurn && args.Turns == 0:
			state = "still running"
		case !turn.Ended:
			state = "started and never ended"
		}
		session := cmp.Or(turn.Turn.Session, "none")
		if named, err := c.Sessions.Identity(turn.Turn.Session); err == nil {
			session = named.Handle()
		}
		files := fmt.Sprintf("%d files", len(turn.Files))
		if len(turn.Files) == 1 {
			files = "1 file"
		}
		fmt.Fprintf(&said, "turn %s, session %s, agent %s, %s: %s\n", turn.Turn.ID, session, turn.Turn.Agent, state, files)
		var outside []string
		for _, file := range turn.Files {
			counts := fmt.Sprintf("+%d -%d", file.Added, file.Removed)
			if file.Binary {
				counts = "binary"
			}
			by := "no call named it"
			if len(file.By) > 0 {
				by = "by " + strings.Join(file.By, ", ")
			}
			fmt.Fprintf(&said, "  %s %s %s\n", file.Path, counts, by)
			if held, _ := subagents.Matches(file.Path, turn.Turn.Owns); len(turn.Turn.Owns) > 0 && !held {
				outside = append(outside, file.Path)
			}
		}
		if len(turn.Turn.Owns) > 0 {
			said.WriteString("  outside owns: " + cmp.Or(strings.Join(outside, ", "), "none") + "\n")
		}
	}
	return contract.Result{Content: said.String(), Command: command}, nil
}
