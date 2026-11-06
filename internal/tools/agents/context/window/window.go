package window

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"tofu/internal/agent/context/recall"
	"tofu/internal/konst"
	"tofu/internal/models/llm/core"
	"tofu/internal/store/session"
	"tofu/internal/store/session/drain"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/history/shadow"
)

const Name = "context"

const leadAgent = "the lead"

type Asked struct {
	Header session.Header
	Handle string
	Agent  string
	Turn   string
	Events []session.Event
}

func (a Asked) Who() string { return cmp.Or(a.Agent, leadAgent) }

func Ask(ctx context.Context, store *session.Store, handle string) (Asked, error) {
	started, inTurn := shadow.StartedIn(ctx)
	if handle == "" && inTurn {
		handle = started.Turn.Session
	}
	headers, err := store.Resolve(cmp.Or(handle, session.HeadHandle))
	if errors.Is(err, fs.ErrNotExist) && cmp.Or(handle, session.HeadHandle) == session.HeadHandle {
		return Asked{}, nil
	}
	if err != nil {
		return Asked{}, err
	}
	if len(headers) != 1 {
		return Asked{}, fmt.Errorf("%q names %d sessions: give more of the id", handle, len(headers))
	}
	asked := Asked{Header: headers[0], Handle: headers[0].ID, Agent: contract.SubAgentAsking(ctx)}
	if identity, err := store.Identity(asked.Header.ID); err == nil {
		asked.Handle = identity.Handle()
	}
	if asked.Events, err = store.Events(asked.Header.ID); err != nil {
		return Asked{}, err
	}
	for _, event := range asked.Events {
		if event.Agent == asked.Agent && event.Kind == session.EventTurnStart {
			asked.Turn = event.Turn
		}
	}
	if inTurn && started.Turn.Session == asked.Header.ID {
		asked.Turn = started.Turn.ID
	}
	return asked, nil
}

type Window struct {
	Sessions *session.Store
	Root     contract.Root
}

func (Window) Name() string { return Name }

func (Window) Definition() core.Tool {
	return core.Tool{
		Name: Name,
		Description: "reports what fills your own context window, read from the session tofu records as you work: tokens used against your limit as your last request measured them, " +
			"what is in the window since your last compaction by kind, sized as tofu session drain sizes it, " +
			fmt.Sprintf("your %d biggest tool results with their call ids, and every file you read that changed on disk since. ", konst.ContextResultsShown) +
			"session names another recorded session by name, id or head; left out it is the one you run in.",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{"session": map[string]any{"type": "string"}},
		},
	}
}

type pathArgs struct {
	Path    string `json:"path"`
	Command string `json:"command"`
	Edits   []struct {
		Path string `json:"path"`
	} `json:"edits"`
}

type sizedResult struct {
	call, label string
	tokens      int
}

type seenFile struct {
	path, call string
	at         time.Time
}

func (w Window) Run(ctx context.Context, raw json.RawMessage) (contract.Result, error) {
	var args struct {
		Session string `json:"session"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return contract.Result{}, fmt.Errorf("context: arguments are not the expected shape: %w", err)
		}
	}
	asked, err := Ask(ctx, w.Sessions, args.Session)
	if err != nil {
		return contract.Result{}, fmt.Errorf("context: %w", err)
	}
	if asked.Header.ID == "" {
		return contract.Result{Content: "no session is recorded in this project yet, so nothing fills a window: 0 tokens used\n", Command: Name}, nil
	}
	estimate, err := recall.LoadConfig()
	if err != nil {
		return contract.Result{}, fmt.Errorf("context: %w", err)
	}
	limit, wire, from := cmp.Or(asked.Header.ContextCeiling, konst.ContextCeilingTokens), asked.Header.Wire, "the session started"
	var window []session.Event
	for _, event := range asked.Events {
		if event.Agent != asked.Agent {
			continue
		}
		switch event.Kind {
		case session.EventTurnStart:
			var start session.TurnStart
			if json.Unmarshal(event.Body, &start) == nil {
				limit, wire = cmp.Or(start.ContextCeiling, limit), cmp.Or(start.Wire, wire)
			}
		case session.EventCompaction:
			window, from = nil, "the compaction at "+event.At.Format(time.DateTime)
			continue
		case session.EventListChange:
			var change session.ListChangeBody
			if json.Unmarshal(event.Body, &change) == nil && change.Kind == "fork" {
				window, from = nil, "the fork at "+event.At.Format(time.DateTime)
				continue
			}
		}
		window = append(window, event)
	}
	measured, since, requested := 0, 0, false
	labels, named := map[string]string{}, map[string][]string{}
	var results []sizedResult
	var seen []seenFile
	for _, event := range window {
		switch event.Kind {
		case session.EventRequest:
			var body struct {
				Prompt     int `json:"prompt_tokens"`
				Completion int `json:"completion_tokens"`
				Read       int `json:"cache_read_tokens"`
				Write      int `json:"cache_write_tokens"`
			}
			if json.Unmarshal(event.Body, &body) == nil {
				measured, since, requested = max(0, core.PromptAccountingFor(wire).FreshTokens(body.Prompt, body.Read))+body.Read+body.Write+body.Completion, 0, true
			}
		case session.EventToolCall:
			var call session.CallBody
			var path pathArgs
			if json.Unmarshal(event.Body, &call) != nil {
				continue
			}
			_ = json.Unmarshal(call.Args, &path)
			said, _, _ := strings.Cut(strings.TrimSpace(cmp.Or(path.Path, path.Command)), "\n")
			labels[event.Call] = strings.TrimSpace(call.Tool + " " + said[:min(len(said), konst.ContextLabelChars)])
			if call.Tool == "read" || call.Tool == "write" || call.Tool == "edit" {
				for _, edit := range path.Edits {
					named[event.Call] = append(named[event.Call], edit.Path)
				}
				if path.Path != "" {
					named[event.Call] = append(named[event.Call], path.Path)
				}
			}
		case session.EventToolResult:
			var body struct {
				Bytes    int `json:"result_bytes"`
				Rendered int `json:"rendered_bytes"`
			}
			label, known := labels[event.Call]
			if !known || json.Unmarshal(event.Body, &body) != nil {
				continue
			}
			sized := sizedResult{call: event.Call, label: label, tokens: cmp.Or(body.Rendered, body.Bytes) * 1000 / max(estimate.BytesPerThousandTokens, 1)}
			results, since = append(results, sized), since+sized.tokens
			for _, path := range named[event.Call] {
				seen = append(slices.DeleteFunc(seen, func(file seenFile) bool { return file.path == path }), seenFile{path: path, call: event.Call, at: event.At})
			}
		}
	}
	var said strings.Builder
	fmt.Fprintf(&said, "context of session %s, %s, turn %s\n", asked.Handle, asked.Who(), cmp.Or(asked.Turn, "none"))
	if !requested {
		fmt.Fprintf(&said, "used 0 of %d tokens: no request is recorded since %s\n", limit, from)
	} else {
		fmt.Fprintf(&said, "used %d of %d tokens (%d%%): %d as the last request measured, and %d in results since\n", measured+since, limit, recall.FillPercent(measured+since, limit), measured, since)
	}
	resident := drain.Read(asked.Header.ID, window, drain.Input{Weights: func(string) drain.Weights { return drain.Weights{} }, BytesPerThousandTokens: estimate.BytesPerThousandTokens}).Residency
	fmt.Fprintf(&said, "in the window since %s, by kind: %d kinds\n", from, len(resident))
	for _, kind := range resident {
		fmt.Fprintf(&said, "  %s %d blocks %d tokens\n", kind.Kind, kind.Blocks, kind.Tokens)
	}
	slices.SortStableFunc(results, func(a, b sizedResult) int { return cmp.Compare(b.tokens, a.tokens) })
	fmt.Fprintf(&said, "biggest results: %d of %d\n", min(len(results), konst.ContextResultsShown), len(results))
	for _, result := range results[:min(len(results), konst.ContextResultsShown)] {
		fmt.Fprintf(&said, "  %s %s %d tokens\n", result.call, result.label, result.tokens)
	}
	var stale []string
	for _, file := range seen {
		path := file.path
		if !filepath.IsAbs(path) {
			path = filepath.Join(string(w.Root), path)
		}
		info, err := os.Stat(path)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			stale = append(stale, fmt.Sprintf("  %s seen by %s at %s, removed from disk since", file.path, file.call, file.at.Format(time.DateTime)))
		case err == nil && info.ModTime().After(file.at):
			stale = append(stale, fmt.Sprintf("  %s seen by %s at %s, changed on disk at %s", file.path, file.call, file.at.Format(time.DateTime), info.ModTime().Format(time.DateTime)))
		}
	}
	slices.Sort(stale)
	fmt.Fprintf(&said, "stale reads: %d of %d files seen\n", len(stale), len(seen))
	for _, line := range stale[:min(len(stale), konst.ContextStaleShown)] {
		said.WriteString(line + "\n")
	}
	return contract.Result{Content: said.String(), Command: Name}, nil
}
