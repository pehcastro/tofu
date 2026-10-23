package turn

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
	"strings"
	"sync"

	"tofu/internal/llm"
)

const readRepeatsBeforeNote = 3

type readSeen struct {
	hash  [sha256.Size]byte
	times int
}

type ReadTool struct {
	root   Root
	ledger *ReadLedger
	mutex  sync.Mutex
	seen   map[string]readSeen
}

func NewReadTool(root string) (*ReadTool, error) {
	resolved, err := NewRoot(root)
	if err != nil {
		return nil, err
	}
	return &ReadTool{root: resolved, seen: map[string]readSeen{}}, nil
}

func (t *ReadTool) Reading(ledger *ReadLedger) *ReadTool {
	t.ledger = ledger
	return t
}

func (t *ReadTool) Name() string { return "read" }

func (t *ReadTool) Definition() llm.Tool {
	return llm.Tool{
		Name: "read",
		Description: "reads a file inside the turn's working directory, whole, or only the lines from start_line to end_line, counted from 1 and both included. " +
			"a path that does not exist is repaired when exactly one file under the working directory carries that name and refused when two do, and a repair is named at the top of the result. " +
			"a png, jpeg, gif or webp comes back as a picture you can see, shrunk to fit when it is large, with its format and size as text",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":       map[string]any{"type": "string"},
				"start_line": map[string]any{"type": "integer"},
				"end_line":   map[string]any{"type": "integer"},
			},
			"required": []string{"path"},
		},
	}
}

type readArgs struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
}

func (t *ReadTool) Run(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args readArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("read: arguments are not the expected shape: %w", err)
	}
	resolved, err := t.root.Resolve(args.Path)
	if err != nil {
		return Result{}, fmt.Errorf("read: %w", err)
	}
	repair := ""
	content, err := os.ReadFile(resolved)
	if errors.Is(err, fs.ErrNotExist) {
		found, listErr := t.root.lookalikes(args.Path)
		if listErr != nil {
			return Result{}, fmt.Errorf("read: %w", listErr)
		}
		repaired, note, refusal := RepairPath(args.Path, "file", found)
		if refusal != nil {
			return Result{}, fmt.Errorf("read: %w", refusal)
		}
		args.Path, repair = repaired, note+"\n"
		if resolved, err = t.root.Resolve(repaired); err == nil {
			content, err = os.ReadFile(resolved)
		}
	}
	if err != nil {
		return Result{}, fmt.Errorf("read: %w", err)
	}
	switch media := http.DetectContentType(content); media {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		t.ledger.Mark(ctx, args.Path, content)
		picture, said, err := lookAt(media, content)
		if err != nil {
			return Result{}, fmt.Errorf("read: %s: %w", args.Path, err)
		}
		return Result{Content: repair + args.Path + ": " + said, Images: []llm.Image{picture}, Command: args.Path}, nil
	}
	if args.StartLine <= 0 && args.EndLine <= 0 {
		t.ledger.Mark(ctx, args.Path, content)
		return Result{Content: repair + t.noteRepeat(ctx, args.Path, content, string(content)), Command: args.Path}, nil
	}

	lines := strings.Split(strings.TrimSuffix(string(content), "\n"), "\n")
	start, end := max(args.StartLine, 1), args.EndLine
	if end <= 0 || end > len(lines) {
		end = len(lines)
	}
	if start > len(lines) {
		return Result{}, fmt.Errorf("read: %s has %d lines and start_line is %d", args.Path, len(lines), start)
	}
	if end < start {
		return Result{}, fmt.Errorf("read: %s end_line %d is before start_line %d", args.Path, end, start)
	}
	span := fmt.Sprintf("%s lines %d-%d of %d", args.Path, start, end, len(lines))
	t.ledger.MarkLines(ctx, args.Path, content, start, end)
	return Result{
		Content: repair + t.noteRepeat(ctx, args.Path, content, span+"\n"+strings.Join(lines[start-1:end], "\n")),
		Command: span,
	}, nil
}

type uncountedReadKey struct{}

func uncountedRead(ctx context.Context) context.Context {
	return context.WithValue(ctx, uncountedReadKey{}, true)
}

func (t *ReadTool) noteRepeat(ctx context.Context, path string, content []byte, text string) string {
	if uncounted, _ := ctx.Value(uncountedReadKey{}).(bool); uncounted {
		return text
	}
	t.mutex.Lock()
	defer t.mutex.Unlock()
	key, hash := shellOwnerFrom(ctx)+" "+ledgerKey(path), sha256.Sum256(append([]byte(text), content...))
	seen := t.seen[key]
	if seen.hash != hash {
		seen = readSeen{hash: hash}
	}
	seen.times++
	t.seen[key] = seen
	if seen.times < readRepeatsBeforeNote {
		return text
	}
	return text + fmt.Sprintf("\n\nthis read has returned this same text %d times and the file has not changed, so reading it again will not change it: read a narrower range with start_line and end_line, or go on with the change", seen.times)
}

func lookAt(media string, content []byte) (llm.Image, string, error) {
	format := strings.TrimPrefix(media, "image/")
	fit, err := llm.FitImage(llm.Image{MediaType: media, Data: content})
	if err != nil {
		return llm.Image{}, "", err
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
		return llm.Image{}, "", fmt.Errorf("it looks like a %s and does not decode: %w", format, err)
	}
	return fit.Image, said + ", attached as a picture", nil
}
