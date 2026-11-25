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
	platformfs "tofu/internal/platform/fs"
	"tofu/internal/store/session"
	"tofu/internal/store/walls"
)

const settledRunsName = "settled-runs.jsonl"

type historyRun struct {
	At      time.Time             `json:"at"`
	Project string                `json:"project,omitempty"`
	Model   string                `json:"model"`
	Scope   session.EstimateScope `json:"scope"`
	Agent   string                `json:"agent,omitempty"`
	Chars   int                   `json:"chars"`
	TookMS  int64                 `json:"took_ms"`
	session.EstimateSplit
}

func settledRuns(state string, keep func(project string) bool) ([]historyRun, int, error) {
	written, err := os.ReadFile(filepath.Join(state, settledRunsName))
	builtFrom := 0
	if errors.Is(err, fs.ErrNotExist) {
		written, builtFrom, err = buildSettled(state)
	}
	if err != nil {
		return nil, 0, err
	}
	var runs []historyRun
	for rest := bytes.TrimRight(written, "\n"); len(rest) > 0; {
		cut := bytes.LastIndexByte(rest, '\n')
		var run historyRun
		if json.Unmarshal(rest[cut+1:], &run) == nil && keep(run.Project) {
			runs = append(runs, run)
		}
		rest = rest[:max(cut, 0)]
	}
	return runs, builtFrom, nil
}

func historyElsewhere(state string, keep func(project string) bool) ([]historyRun, int, error) {
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
	readAtOnce(len(projects), func(at int) { found[at], builtFrom[at], failed[at] = settledRuns(projects[at].state, keep) })
	built := 0
	for _, sessions := range builtFrom {
		built += sessions
	}
	return slices.Concat(found...), built, errors.Join(failed...)
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
	for _, run := range slices.SortedStableFunc(slices.Values(slices.Concat(found...)), func(a, b historyRun) int { return a.At.Compare(b.At) }) {
		if err := encoder.Encode(run); err != nil {
			return nil, 0, err
		}
	}
	return written.Bytes(), len(listing.Sessions), platformfs.WriteFile(filepath.Join(state, settledRunsName), written.Bytes(), 0o644)
}

func runsIn(store *session.Store, header session.Header) ([]historyRun, error) {
	events, err := store.EventsOf(header.ID, session.EventTurnEnd, session.EventToolCall, session.EventToolResult, session.EventWall)
	if err != nil {
		return nil, err
	}
	tools, briefs, turnMS := map[string]string{}, map[string]int{}, map[string]int64{}
	splits, shells := map[string]*session.EstimateSplit{}, map[string]map[string]checkKind{}
	var runs []historyRun
	for _, event := range events {
		ran := cmp.Or(event.Agent, "turn "+event.Turn)
		switch event.Kind {
		case session.EventWall:
			if wall, err := session.DecodeWall(event); err == nil && wall.Op == string(walls.OpTurn) && event.Agent == "" {
				turnMS[event.Turn] = wall.MS
			}
		case session.EventToolCall:
			var call session.CallBody
			var args spawnArgs
			if json.Unmarshal(event.Body, &call) != nil {
				continue
			}
			if tools[event.Call] = call.Tool; call.Tool == SpawnToolName && json.Unmarshal(call.Args, &args) == nil {
				briefs[event.Call] = utf8.RuneCountInString(args.Task)
			}
		case session.EventToolResult:
			var row ToolCallRow
			if json.Unmarshal(event.Body, &row) != nil {
				continue
			}
			if splits[ran] == nil {
				splits[ran], shells[ran] = &session.EstimateSplit{}, map[string]checkKind{}
			}
			addCall(splits[ran], shells[ran], tools[event.Call], row)
		case session.EventTurnEnd:
			var ended struct {
				Task        string          `json:"task"`
				Model       string          `json:"model"`
				Outcome     session.Outcome `json:"outcome"`
				WallClockMS int64           `json:"wall_clock_ms"`
			}
			if event.Agent == "" && json.Unmarshal(event.Body, &ended) == nil && ended.Outcome == session.OutcomeStopped {
				runs = append(runs, historyRun{At: event.At, Project: header.Project, Model: ended.Model, Scope: session.EstimateTurn, Chars: utf8.RuneCountInString(ended.Task),
					TookMS: cmp.Or(turnMS[event.Turn], ended.WallClockMS), EstimateSplit: *cmp.Or(splits[ran], &session.EstimateSplit{})})
			}
		}
	}
	for _, run := range header.Agents {
		if run.EndedAt != nil && run.Status == subagents.Finished.String() {
			runs = append(runs, historyRun{At: *run.EndedAt, Project: header.Project, Model: run.Model, Scope: session.EstimateAgent, Agent: run.Definition,
				Chars: briefs[run.SpawnCall], TookMS: run.EndedAt.Sub(run.StartedAt).Milliseconds(), EstimateSplit: *cmp.Or(splits[run.Agent], &session.EstimateSplit{})})
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
