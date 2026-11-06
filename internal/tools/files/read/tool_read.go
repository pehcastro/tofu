package read

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"tofu/internal/konst"
	"tofu/internal/models/llm/core"
	"tofu/internal/tools/code/search/engine"
	"tofu/internal/tools/contract"
)

const (
	Name                  = "read"
	readRepeatsBeforeNote = 3
)

type readSeen struct {
	hash  [sha256.Size]byte
	times int
}

type ReadTool struct {
	root   contract.Root
	ledger *contract.ReadLedger
	mutex  sync.Mutex
	seen   map[string]readSeen
}

func NewReadTool(root string) (*ReadTool, error) {
	resolved, err := contract.NewRoot(root)
	if err != nil {
		return nil, err
	}
	return &ReadTool{root: resolved, seen: map[string]readSeen{}}, nil
}

func (t *ReadTool) Reading(ledger *contract.ReadLedger) *ReadTool {
	t.ledger = ledger
	return t
}

func (t *ReadTool) Root() contract.Root { return t.root }

func (t *ReadTool) Name() string { return Name }

func (t *ReadTool) Definition() core.Tool {
	return core.Tool{
		Name: Name,
		Description: "reads a file inside the turn's working directory, whole, or only the lines from start_line to end_line, counted from 1 and both included, " +
			fmt.Sprintf("or about %d lines around at, which is a line number or a name the file declares, such as Total or Order.Total in a .go, .rs, .ts, .tsx or .py file. ", konst.ReadWindowLines) +
			"a read of the same lines of a file that has not changed since your last read of them in this turn comes back as one line naming the step of that read, whose result is still above. " +
			"a path that does not exist is repaired when exactly one file under the working directory carries that name and refused when two do, and a repair is named at the top of the result. " +
			"a png, jpeg, gif or webp comes back as a picture you can see, shrunk to fit when it is large, with its format and size as text",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":       map[string]any{"type": "string"},
				"start_line": map[string]any{"type": "integer"},
				"end_line":   map[string]any{"type": "integer"},
				"at":         map[string]any{"type": "string", "description": "a line number, or a declared name such as Total or Order.Total; leave start_line and end_line out with it"},
			},
			"required": []string{"path"},
		},
	}
}

type ReadArgs struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	At        any    `json:"at,omitempty"`
}

func (t *ReadTool) Run(ctx context.Context, raw json.RawMessage) (contract.Result, error) {
	var args ReadArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return contract.Result{}, fmt.Errorf("read: arguments are not the expected shape: %w", err)
	}
	resolved, err := t.root.Resolve(args.Path)
	if err != nil {
		return contract.Result{}, fmt.Errorf("read: %w", err)
	}
	asked, repair := args.Path, ""
	content, err := os.ReadFile(resolved)
	if errors.Is(err, fs.ErrNotExist) {
		found, listErr := contract.Lookalikes(t.root, args.Path)
		if listErr != nil {
			return contract.Result{}, fmt.Errorf("read: %w", listErr)
		}
		repaired, note, refusal := contract.RepairPath(args.Path, "file", found)
		if refusal != nil {
			return contract.Result{}, fmt.Errorf("read: %w", refusal)
		}
		args.Path, repair = repaired, note+"\n"
		if resolved, err = t.root.Resolve(repaired); err == nil {
			content, err = os.ReadFile(resolved)
		}
	}
	if err != nil {
		return contract.Result{}, fmt.Errorf("read: %w", err)
	}
	switch media := http.DetectContentType(content); media {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		t.ledger.Mark(ctx, args.Path, content)
		picture, said, err := lookAt(media, content)
		if err != nil {
			return contract.Result{}, fmt.Errorf("read: %s: %w", args.Path, err)
		}
		return contract.Result{Content: repair + args.Path + ": " + said, Images: []core.Image{picture}, Command: args.Path}, nil
	}
	if args.At == nil && args.StartLine <= 0 && args.EndLine <= 0 {
		t.ledger.Mark(ctx, args.Path, content)
		return contract.Result{Content: repair + t.reply(ctx, asked, args.Path, content, string(content)), Command: args.Path}, nil
	}

	lines := strings.Split(strings.TrimSuffix(string(content), "\n"), "\n")
	start, end, around, err := spanOf(args, resolved, len(lines))
	if err != nil {
		return contract.Result{}, fmt.Errorf("read: %s %w", args.Path, err)
	}
	span := fmt.Sprintf("%s lines %d-%d of %d%s", args.Path, start, end, len(lines), around)
	t.ledger.MarkLines(ctx, args.Path, content, start, end)
	return contract.Result{
		Content: repair + t.reply(ctx, asked, args.Path, content, span+"\n"+strings.Join(lines[start-1:end], "\n")),
		Command: span,
	}, nil
}

func spanOf(args ReadArgs, resolved string, lines int) (int, int, string, error) {
	if args.At == nil {
		start, end := max(args.StartLine, 1), args.EndLine
		if end <= 0 || end > lines {
			end = lines
		}
		if start > lines {
			return 0, 0, "", fmt.Errorf("has %d lines and start_line is %d", lines, start)
		}
		if end < start {
			return 0, 0, "", fmt.Errorf("end_line %d is before start_line %d", end, start)
		}
		return start, end, "", nil
	}
	if args.StartLine > 0 || args.EndLine > 0 {
		return 0, 0, "", errors.New("is asked for with at and with start_line or end_line, which name two different stretches: give one")
	}
	switch at := args.At.(type) {
	case float64:
		return aroundLine(at, lines)
	case string:
		at = strings.TrimSpace(at)
		if line, err := strconv.Atoi(at); err == nil {
			return aroundLine(float64(line), lines)
		}
		return aroundSymbol(resolved, at, lines)
	default:
		return 0, 0, "", fmt.Errorf("is asked for at %v, which is neither a line number nor a declared name", at)
	}
}

func aroundLine(at float64, lines int) (int, int, string, error) {
	line := int(at)
	if float64(line) != at || line < 1 || line > lines {
		return 0, 0, "", fmt.Errorf("has %d lines and at is %v", lines, at)
	}
	first, last := window(line, line, lines)
	return first, last, fmt.Sprintf(", around line %d", line), nil
}

func aroundSymbol(resolved, name string, lines int) (int, int, string, error) {
	graph := engine.Index("", []string{filepath.ToSlash(resolved)}, func(string) bool { return true })
	if graph.Parsed == 0 {
		return 0, 0, "", errors.New("holds no declaration tofu can name, since it is not a .go, .rs, .ts, .tsx or .py file or it does not parse: give at a line number")
	}
	found := slices.DeleteFunc(slices.Clone(graph.Definitions), func(declared engine.Definition) bool {
		return declared.Symbol != name && !strings.HasSuffix(declared.Symbol, "."+name)
	})
	named := make([]string, len(found))
	for i, declared := range found {
		named[i] = fmt.Sprintf("%s %s at lines %d-%d", declared.Kind, declared.Symbol, declared.FirstLine, declared.LastLine)
	}
	switch len(found) {
	case 0:
		return 0, 0, "", fmt.Errorf("declares no %s: %s", name, engine.Closest(name, graph.Definitions))
	case 1:
		first, last := window(found[0].FirstLine, found[0].LastLine, lines)
		return first, last, ", " + named[0], nil
	default:
		return 0, 0, "", fmt.Errorf("declares %s %d times, as %s: name one as Receiver.Name, or give at a line number", name, len(found), strings.Join(named, " and "))
	}
}

func window(first, last, lines int) (int, int) {
	if spare := konst.ReadWindowLines - (last - first + 1); spare > 0 {
		first, last = first-spare/2, last+spare-spare/2
	} else {
		last = first + konst.ReadWindowLines - 1
	}
	if first < 1 {
		first, last = 1, last+1-first
	}
	if last > lines {
		first, last = max(first-(last-lines), 1), lines
	}
	return first, last
}

type uncountedReadKey struct{}

func Uncounted(ctx context.Context) context.Context {
	return context.WithValue(ctx, uncountedReadKey{}, true)
}

func (t *ReadTool) reply(ctx context.Context, asked, path string, content []byte, body string) string {
	if uncounted, _ := ctx.Value(uncountedReadKey{}).(bool); uncounted {
		return body
	}
	hash := sha256.Sum256(append([]byte(body), content...))
	if step, shown := contract.ReadWindowOf(ctx).ShownBefore(asked, hash, contract.StepOf(ctx)); shown {
		return fmt.Sprintf("%s: unchanged since your read at step %d, whose result is still above", path, step)
	}
	t.mutex.Lock()
	defer t.mutex.Unlock()
	key := contract.ShellOwnerFrom(ctx) + " " + contract.LedgerKey(path)
	seen := t.seen[key]
	if seen.hash != hash {
		seen = readSeen{hash: hash}
	}
	seen.times++
	t.seen[key] = seen
	if seen.times < readRepeatsBeforeNote {
		return body
	}
	return body + fmt.Sprintf("\n\nthis read has returned this same text %d times and the file has not changed, so reading it again will not change it: read a narrower range with start_line and end_line, or go on with the change", seen.times)
}

func lookAt(media string, content []byte) (core.Image, string, error) {
	format := strings.TrimPrefix(media, "image/")
	fit, err := core.FitImage(core.Image{MediaType: media, Data: content})
	if err != nil {
		return core.Image{}, "", err
	}
	if media == "image/webp" {
		return fit.Image, fmt.Sprintf("webp, %d bytes, attached as a picture", len(content)), nil
	}
	said := fmt.Sprintf("%s, %dx%d, %d bytes", format, fit.Was.X, fit.Was.Y, len(content))
	if fit.Shrunk {
		return fit.Image, fmt.Sprintf("%s, shrunk to %dx%d (%s, %d bytes) and attached as a picture",
			said, fit.Sent.X, fit.Sent.Y, strings.TrimPrefix(fit.MediaType, "image/"), len(fit.Data)), nil
	}
	if _, _, err := image.Decode(bytes.NewReader(content)); err != nil {
		return core.Image{}, "", fmt.Errorf("it looks like a %s and does not decode: %w", format, err)
	}
	return fit.Image, said + ", attached as a picture", nil
}
