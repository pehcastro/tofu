package turn

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/recall"
)

const truncationMarker = "\n...(truncated)...\n"

const droppedMarker = "\n...(%s dropped from the middle of this result, which was not stored anywhere. " +
	"run a narrower command to read the part you need.)...\n"

const unstoredMarker = "\n...(%s dropped from the middle of this result. the whole output could not be stored, " +
	"so there is no artifact handle for it and artifact_fetch cannot reach the missing bytes. " +
	"run a narrower command to read the part you need.)...\n"

const lineCutMarker = "…(%d characters cut)…"

const escape = 0x1b

const artifactHandleBytes = 16

const heldDropLead = "bash: the command printed "

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

func (a Artifacts) Render(tool string, args json.RawMessage, content string, bytesCap int) (string, string, error) {
	shown, wideLines := content, 0
	if capsLineWidth(tool, args) {
		shown, wideLines = capLineWidths(content)
	}
	if wideLines == 0 && len(content) <= bytesCap {
		return content, "", nil
	}
	if !a.handles {
		return cutOnRuneBoundary(shown, bytesCap, droppedMarker), "", nil
	}
	stored, err := recall.Elide(a.store, recall.Config{}, []byte(content), true)
	if err != nil {
		return cutOnRuneBoundary(shown, bytesCap, unstoredMarker), "", err
	}
	holds := "this result whole"
	if strings.HasPrefix(content, heldDropLead) {
		holds = "what tofu kept of this result"
	}
	header := fmt.Sprintf("artifact %s holds %s: %d bytes, ", stored.Reference.ID, holds, len(content))
	if wideLines > 0 {
		header += fmt.Sprintf("and %d lines wider than %d bytes are cut to their start and end below. ", wideLines, konst.TurnResultLineWidth)
	}
	if len(shown) <= bytesCap {
		return header + "call artifact_fetch with this handle, an offset and a length to read what was cut.\n" + shown, stored.Reference.ID, nil
	}
	head, tail := runeSafeHead(shown, a.preview.HeadBytes), runeSafeTail(shown, a.preview.TailBytes)
	return fmt.Sprintf(
		"%sthe first %d and the last %d bytes follow. "+
			"call artifact_fetch with this handle, an offset and a length to read any other range.\n%s%s%s",
		header, len(head), len(tail), head, truncationMarker, tail,
	), stored.Reference.ID, nil
}

func capsLineWidth(tool string, args json.RawMessage) bool {
	switch tool {
	case "artifact_fetch", "search":
		return false
	case "read":
		var ranged readArgs
		return json.Unmarshal(args, &ranged) != nil || (ranged.StartLine <= 0 && ranged.EndLine <= 0)
	}
	return true
}

func capLineWidths(content string) (string, int) {
	var capped strings.Builder
	wide := 0
	for line := range strings.SplitAfterSeq(content, "\n") {
		body := strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if len(body) <= konst.TurnResultLineWidth {
			capped.WriteString(line)
			continue
		}
		wide++
		capped.WriteString(cutWideLine(body))
		capped.WriteString(line[len(body):])
	}
	if wide == 0 {
		return content, 0
	}
	return capped.String(), wide
}

func cutWideLine(line string) string {
	head := runeSafeHead(line, konst.TurnResultLineWidth/2)
	if at := strings.LastIndexByte(head, escape); at >= 0 && escapeEnd(line, at) > len(head) {
		head = head[:at]
	}
	tailStart := len(line) - len(runeSafeTail(line, konst.TurnResultLineWidth/2))
	if at := strings.LastIndexByte(line[:tailStart], escape); at >= 0 {
		tailStart = max(tailStart, escapeEnd(line, at))
	}
	return head + fmt.Sprintf(lineCutMarker, utf8.RuneCountInString(line[len(head):tailStart])) + line[tailStart:]
}

func escapeEnd(line string, at int) int {
	end := at + 1
	if end < len(line) && line[end] == '[' {
		end++
		for end < len(line) && line[end] >= ' ' && line[end] <= '?' {
			end++
		}
	}
	return min(end+1, len(line))
}

func cutOnRuneBoundary(content string, bytesCap int, marker string) string {
	if len(content) <= bytesCap {
		return content
	}
	head := runeSafeHead(content, bytesCap/2)
	tail := runeSafeTail(content, bytesCap-len(head))
	dropped := len(content) - len(head) - len(tail)
	amount := fmt.Sprintf("%d bytes", dropped)
	if dropped == 1 {
		amount = "1 byte"
	}
	return head + fmt.Sprintf(marker, amount) + tail
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
		Command: fmt.Sprintf("%s bytes %d to %d of %d", args.Handle, args.Offset, end, len(body)),
	}, nil
}
