package loop

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"time"
	"unicode/utf8"

	"tofu/internal/agent/subagents"
	"tofu/internal/konst"
	platformfs "tofu/internal/platform/fs"
	"tofu/internal/store/session"
)

const settledRunsName = "estimates.jsonl"

type historyRun struct {
	Project      string `json:"project,omitempty"`
	Model        string `json:"model"`
	Chars        int    `json:"chars"`
	TookMS       int64  `json:"took_ms"`
	Split        bool   `json:"split,omitempty"`
	TestMS       int64  `json:"test_ms,omitempty"`
	PermissionMS int64  `json:"permission_ms,omitempty"`
	TestCalls    int    `json:"test_calls,omitempty"`
}

func settledRuns(state string, keep func(project string) bool, model func(string) bool) ([]historyRun, int, error) {
	written, err := os.ReadFile(filepath.Join(state, settledRunsName))
	builtFrom := 0
	if errors.Is(err, fs.ErrNotExist) {
		written, builtFrom, err = buildSettled(state)
	}
	if err != nil {
		return nil, 0, err
	}
	var runs []historyRun
	for rest := bytes.TrimRight(written, "\n"); len(rest) > 0 && len(runs) < konst.SpawnEstimateRunsMost; {
		cut := bytes.LastIndexByte(rest, '\n')
		var run historyRun
		if json.Unmarshal(rest[cut+1:], &run) == nil && keep(run.Project) && model(run.Model) {
			runs = append(runs, run)
		}
		rest = rest[:max(cut, 0)]
	}
	return runs, builtFrom, nil
}

func historyElsewhere(state string, keep func(project string) bool, model func(string) bool) ([]historyRun, int, error) {
	root := filepath.Dir(state)
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, 0, err
	}
	type project struct {
		state   string
		changed time.Time
	}
	var projects []project
	for _, entry := range entries {
		dir := filepath.Join(root, entry.Name())
		if sessions, err := os.Stat(session.SessionsDir(dir)); err == nil && entry.IsDir() && dir != filepath.Clean(state) {
			projects = append(projects, project{state: dir, changed: sessions.ModTime()})
		}
	}
	slices.SortFunc(projects, func(a, b project) int { return b.changed.Compare(a.changed) })
	found, builtFrom, failed := make([][]historyRun, len(projects)), make([]int, len(projects)), make([]error, len(projects))
	readAtOnce(len(projects), func(at int) { found[at], builtFrom[at], failed[at] = settledRuns(projects[at].state, keep, model) })
	var runs []historyRun
	for at := range projects {
		if failed[at] != nil {
			return nil, 0, failed[at]
		}
		if runs = append(runs, found[at]...); len(runs) >= konst.SpawnEstimateRunsMost {
			break
		}
	}
	built := 0
	for _, sessions := range builtFrom {
		built += sessions
	}
	return runs[:min(len(runs), konst.SpawnEstimateRunsMost)], built, nil
}

func readAtOnce(count int, read func(at int)) {
	reading, readers := sync.WaitGroup{}, make(chan struct{}, runtime.GOMAXPROCS(0))
	for at := range count {
		reading.Go(func() {
			readers <- struct{}{}
			read(at)
			<-readers
		})
	}
	reading.Wait()
}

func buildSettled(state string) ([]byte, int, error) {
	store := session.OpenAt(state)
	listing, err := store.Listing()
	if err != nil {
		return nil, 0, err
	}
	found, failed := make([][]historyRun, len(listing.Sessions)), make([]error, len(listing.Sessions))
	readAtOnce(len(listing.Sessions), func(at int) { found[at], failed[at] = runsIn(store, listing.Sessions[at]) })
	if err := errors.Join(failed...); err != nil {
		return nil, 0, err
	}
	var written bytes.Buffer
	encoder := json.NewEncoder(&written)
	for _, run := range slices.Backward(slices.Concat(found...)) {
		if err := encoder.Encode(run); err != nil {
			return nil, 0, err
		}
	}
	return written.Bytes(), len(listing.Sessions), platformfs.WriteFile(filepath.Join(state, settledRunsName), written.Bytes(), 0o644)
}
func runsIn(store *session.Store, header session.Header) ([]historyRun, error) {
	events, err := store.EventsOf(header.ID, session.EventTurnEnd, session.EventToolCall, session.EventEstimate, session.EventEstimateSettled)
	if err != nil {
		return nil, err
	}
	estimated, err := session.Estimates(events)
	if err != nil {
		return nil, err
	}
	var runs []historyRun
	counted := map[string]bool{}
	for _, one := range estimated {
		if one.Settled != nil && one.Settled.Outcome == session.EstimateDone && one.Scope != session.EstimateCall {
			runs = append(runs, historyRun{Project: header.Project, Model: one.Model, Chars: one.PromptChars, TookMS: one.Settled.TookMS, Split: true,
				TestMS: one.Settled.TestMS, PermissionMS: one.Settled.PermissionMS, TestCalls: one.Settled.TestCalls})
			counted[cmp.Or(one.Agent, one.Event.Turn)] = true
		}
	}
	briefs := map[string]int{}
	for _, event := range events {
		switch event.Kind {
		case session.EventToolCall:
			var call session.CallBody
			var args spawnArgs
			if json.Unmarshal(event.Body, &call) == nil && call.Tool == SpawnToolName && json.Unmarshal(call.Args, &args) == nil {
				briefs[event.Call] = utf8.RuneCountInString(args.Task)
			}
		case session.EventTurnEnd:
			var ended struct {
				Task        string          `json:"task"`
				Model       string          `json:"model"`
				Outcome     session.Outcome `json:"outcome"`
				WallClockMS int64           `json:"wall_clock_ms"`
			}
			if event.Agent == "" && !counted[event.Turn] && json.Unmarshal(event.Body, &ended) == nil && ended.Outcome == session.OutcomeStopped {
				runs = append(runs, historyRun{Project: header.Project, Model: ended.Model, Chars: utf8.RuneCountInString(ended.Task), TookMS: ended.WallClockMS})
			}
		}
	}
	for _, run := range header.Agents {
		if !counted[run.Agent] && run.EndedAt != nil && run.Status == subagents.Finished.String() {
			runs = append(runs, historyRun{Project: header.Project, Model: run.Model, Chars: briefs[run.SpawnCall], TookMS: run.EndedAt.Sub(run.StartedAt).Milliseconds()})
		}
	}
	return runs, nil
}

func appendSettled(state string, run historyRun) error {
	file, err := os.OpenFile(filepath.Join(state, settledRunsName), os.O_APPEND|os.O_WRONLY, 0)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	line, err := json.Marshal(run)
	if err == nil {
		_, err = file.Write(append(line, '\n'))
	}
	return errors.Join(err, file.Close())
}
