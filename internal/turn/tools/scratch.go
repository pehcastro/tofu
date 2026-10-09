package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"tofu/internal/judge/ledger"
	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/sys"
	"tofu/internal/turn"
)

const scratchIndex = "INDEX.txt"

type crossProjectReads struct {
	ask     turn.Person
	mu      sync.Mutex
	allowed []string
}

type scratchTool struct {
	name  string
	reads *crossProjectReads
}

func ScratchTools(ask turn.Person) []turn.Tool {
	reads := &crossProjectReads{ask: ask}
	return []turn.Tool{scratchTool{turn.ScratchPathToolName, reads}, scratchTool{turn.ScratchShareToolName, reads}, scratchTool{turn.ScratchListToolName, reads}, scratchTool{turn.ScratchReadToolName, reads}}
}

func (t scratchTool) Name() string { return t.name }

func (t scratchTool) Definition() llm.Tool {
	text := func(description string) map[string]any {
		return map[string]any{"type": "string", "description": description}
	}
	object := func(required []string, properties map[string]any) map[string]any {
		return map[string]any{"type": "object", "properties": properties, "required": required}
	}
	switch t.name {
	case turn.ScratchPathToolName:
		return llm.Tool{Name: t.name, Description: "returns the absolute folder to use, outside the project, so you never guess one: tmp for probes and temporary files, out for what you produce, logs, shared for the notes every agent of this project reads, or cache for build caches",
			Parameters: object([]string{"kind"}, map[string]any{"kind": map[string]any{"type": "string", "enum": []string{"tmp", "out", "logs", "shared", "cache"}}})}
	case turn.ScratchShareToolName:
		return llm.Tool{Name: t.name, Description: "copies a file from your own scratch folder into the project's shared folder with one line saying what it is, so later agents and sessions find it. only the lead shares",
			Parameters: object([]string{"path", "note"}, map[string]any{"path": text("the absolute path of the file in your scratch folder"), "note": text("one line on what the file is")})}
	case turn.ScratchListToolName:
		return llm.Tool{Name: t.name, Description: "lists the files in your own scratch folder (mine), in every agent's folder of this session (session), or in the project's shared folder (shared), with sizes and the agent that wrote each",
			Parameters: object([]string{"scope"}, map[string]any{"scope": map[string]any{"type": "string", "enum": []string{"mine", "session", "shared"}}})}
	}
	return llm.Tool{Name: t.name, Description: "reads a file another agent of this session wrote, by its path relative to the session folder as scratch_list prints it, or with project set to another project's directory, a file in that project's shared folder. reading another project asks the person once per project, through the lead",
		Parameters: object([]string{"path"}, map[string]any{"path": text("the path relative to the session folder, or to the other project's shared folder"), "project": text("another project's directory, to read its shared folder")})}
}

func (t scratchTool) Run(ctx context.Context, raw json.RawMessage) (turn.Result, error) {
	var args struct{ Kind, Path, Note, Scope, Project string }
	if err := json.Unmarshal(raw, &args); err != nil {
		return turn.Result{}, fmt.Errorf("%s: arguments are not the expected shape: %w", t.name, err)
	}
	place, found := sys.ScratchOf(ctx)
	if !found {
		return turn.Result{}, fmt.Errorf("%s: this run has no scratchpad", t.name)
	}
	switch t.name {
	case turn.ScratchPathToolName:
		return scratchPath(place, args.Kind)
	case turn.ScratchShareToolName:
		return scratchShare(ctx, place, args.Path, args.Note)
	case turn.ScratchListToolName:
		return scratchList(place, args.Scope)
	}
	return t.reads.read(ctx, place, args.Project, args.Path)
}

func scratchPath(place sys.ScratchPlace, kind string) (turn.Result, error) {
	folder := map[string]string{"tmp": place.Tmp(), "out": filepath.Join(place.Dir(), "out"), "logs": filepath.Join(place.Dir(), "logs"), "shared": place.Shared(), "cache": place.Cache()}[kind]
	if folder == "" {
		return turn.Result{}, fmt.Errorf("scratch_path: kind %q is none of tmp, out, logs, shared or cache", kind)
	}
	return turn.Result{Content: folder, Command: "scratch path " + kind}, nil
}

func scratchShare(ctx context.Context, place sys.ScratchPlace, path, note string) (turn.Result, error) {
	if turn.SubAgentAsking(ctx) != "" {
		return turn.Result{}, errors.New("scratch_share: only the lead shares: name the file and what it is in your report, and the lead shares it")
	}
	if !under(place.Dir(), path) || strings.TrimSpace(note) == "" {
		return turn.Result{}, fmt.Errorf("scratch_share: path must be a file inside %s, and note one line on what it is", place.Dir())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return turn.Result{}, fmt.Errorf("scratch_share: %w", err)
	}
	shared := filepath.Join(place.Shared(), filepath.Base(path))
	if err := os.WriteFile(shared, data, 0o644); err != nil {
		return turn.Result{}, fmt.Errorf("scratch_share: %w", err)
	}
	index, err := os.OpenFile(filepath.Join(place.Shared(), scratchIndex), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err == nil {
		_, err = fmt.Fprintf(index, "%s: %s\n", filepath.Base(path), strings.ReplaceAll(note, "\n", " "))
		err = errors.Join(err, index.Close())
	}
	return turn.Result{Content: "shared as " + shared, Command: "scratch share " + filepath.Base(path)}, err
}

func scratchList(place sys.ScratchPlace, scope string) (turn.Result, error) {
	from := map[string]string{"mine": place.Dir(), "session": place.SessionDir(), "shared": place.Shared()}[scope]
	if from == "" {
		return turn.Result{}, fmt.Errorf("scratch_list: scope %q is none of mine, session or shared", scope)
	}
	var lines []string
	err := filepath.WalkDir(from, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, path)
		writer := "lead"
		if parts := strings.Split(filepath.ToSlash(rel), "/"); scope == "session" && len(parts) > 1 && parts[0] == sys.ScratchAgents {
			writer = parts[1]
		}
		lines = append(lines, fmt.Sprintf("%s  %d bytes  by %s", filepath.ToSlash(rel), info.Size(), writer))
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) || err == nil && len(lines) == 0 {
		return turn.Result{Content: from + " holds no files", Command: "scratch list " + scope}, nil
	}
	return turn.Result{Content: from + "\n" + strings.Join(lines, "\n"), Command: "scratch list " + scope}, err
}

func (c *crossProjectReads) read(ctx context.Context, place sys.ScratchPlace, project, path string) (turn.Result, error) {
	from := place.SessionDir()
	if project != "" {
		root, err := sys.ScratchRootAt(project)
		if err != nil {
			return turn.Result{}, fmt.Errorf("scratch_read: %w", err)
		}
		if from = filepath.Join(root, sys.ScratchShared); from != place.Shared() {
			if err := c.allow(ctx, project); err != nil {
				return turn.Result{Content: err.Error() + ", so nothing was read", Command: "scratch read " + project}, nil
			}
		}
	}
	full := filepath.Join(from, path)
	if !under(from, full) || filepath.IsAbs(path) {
		return turn.Result{}, fmt.Errorf("scratch_read: %q is not a path inside %s", path, from)
	}
	file, err := os.Open(full)
	if err != nil {
		return turn.Result{}, fmt.Errorf("scratch_read: %w", err)
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, konst.ScratchReadBytes))
	return turn.Result{Content: string(data), Command: "scratch read " + full}, err
}

func (c *crossProjectReads) allow(ctx context.Context, project string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := filepath.Clean(project)
	if slices.Contains(c.allowed, key) {
		return nil
	}
	if c.ask == nil {
		return errors.New("reading another project's shared folder asks the person, and no person is here to answer")
	}
	asker := "the lead"
	if id := turn.SubAgentAsking(ctx); id != "" {
		asker = "the lead, for sub-agent " + id + ","
	}
	shown, err := json.Marshal(map[string]string{"project": project, "question": asker + " asks to read the shared folder of the project at " + project + ". Allow it for this session?"})
	if err != nil {
		return err
	}
	answer, err := c.ask(turn.AsTheLead(ctx), turn.GateRequest{Tool: turn.ScratchReadToolName, Args: shown}, turn.GateDecision{Verdict: ledger.VerdictAsk})
	switch {
	case err != nil:
		return fmt.Errorf("the person could not be asked: %w", err)
	case answer == turn.PersonDenied:
		return errors.New("the person refused the read of " + project + "'s shared folder")
	}
	c.allowed = append(c.allowed, key)
	return nil
}

func under(root, path string) bool {
	rel, err := filepath.Rel(root, filepath.Clean(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
