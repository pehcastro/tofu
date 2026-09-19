package turn

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"boji/internal/llm"
	"boji/internal/recall"
)

const truncationMarker = "\n...(truncated)...\n"

const artifactHandleBytes = 16

type Artifacts struct {
	store   *recall.Store
	preview recall.Config
	handles bool
}

func NewArtifacts(dir string, handles bool) (Artifacts, error) {
	preview, err := recall.LoadConfig()
	if err != nil {
		return Artifacts{}, err
	}
	return Artifacts{store: recall.NewStore(dir), preview: preview, handles: handles}, nil
}

func (a Artifacts) Render(content string, bytesCap int) (string, string, error) {
	if len(content) <= bytesCap {
		return content, "", nil
	}
	if !a.handles {
		return truncateMiddle(content, bytesCap), "", nil
	}
	preview := a.preview
	preview.ElideAboveBytes = bytesCap
	elided, err := recall.Elide(a.store, preview, []byte(content), true)
	if err != nil {
		return truncateMiddle(content, bytesCap), "", err
	}
	reference := elided.Reference
	return fmt.Sprintf(
		"artifact %s holds this result whole: %d bytes, not pasted. the first %d and the last %d bytes follow. "+
			"call artifact_fetch with this handle, an offset and a length to read any other range.\n%s%s%s",
		reference.ID, reference.Bytes, len(reference.Head), len(reference.Tail),
		reference.Head, truncationMarker, reference.Tail,
	), reference.ID, nil
}

func truncateMiddle(content string, bytesCap int) string {
	head := bytesCap / 2
	return content[:head] + truncationMarker + content[len(content)-(bytesCap-head):]
}

func (a Artifacts) FetchTool() FetchTool {
	return FetchTool{store: a.store}
}

type FetchTool struct {
	store *recall.Store
}

func (t FetchTool) Name() string { return "artifact_fetch" }

func (t FetchTool) Definition() llm.Tool {
	return llm.Tool{
		Name: "artifact_fetch",
		Description: "reads a byte range out of a tool result that was stored whole instead of pasted. " +
			"the handle comes from the artifact line in that result",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"handle": map[string]any{"type": "string"},
				"offset": map[string]any{"type": "integer"},
				"length": map[string]any{"type": "integer"},
			},
			"required": []string{"handle", "offset", "length"},
		},
	}
}

type fetchArgs struct {
	Handle string `json:"handle"`
	Offset int    `json:"offset"`
	Length int    `json:"length"`
}

func (t FetchTool) Run(_ context.Context, raw json.RawMessage) (Result, error) {
	var args fetchArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("artifact_fetch: arguments are not the expected shape: %w", err)
	}
	decoded, err := hex.DecodeString(args.Handle)
	if err != nil || len(decoded) != artifactHandleBytes {
		return Result{}, fmt.Errorf("artifact_fetch: %q is not an artifact handle", args.Handle)
	}
	if args.Length <= 0 {
		return Result{}, fmt.Errorf("artifact_fetch: length %d is not a range, ask for at least one byte", args.Length)
	}
	if args.Offset < 0 {
		return Result{}, fmt.Errorf("artifact_fetch: offset %d is before the start of artifact %s", args.Offset, args.Handle)
	}
	if t.store == nil {
		return Result{}, fmt.Errorf("artifact_fetch: this turn stored no artifacts, so handle %s cannot be read", args.Handle)
	}
	body, err := t.store.Fetch(args.Handle)
	if err != nil {
		return Result{}, fmt.Errorf("artifact_fetch: no stored result for handle %s: %w", args.Handle, err)
	}
	if args.Offset >= len(body) {
		return Result{}, fmt.Errorf("artifact_fetch: offset %d is past the end of artifact %s, which is %d bytes",
			args.Offset, args.Handle, len(body))
	}
	end := min(args.Offset+args.Length, len(body))
	return Result{
		Content: string(body[args.Offset:end]),
		Command: fmt.Sprintf("artifact_fetch %s bytes %d to %d of %d", args.Handle, args.Offset, end, len(body)),
	}, nil
}
