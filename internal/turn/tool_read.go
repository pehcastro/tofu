package turn

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"io/fs"
	"math"
	"net/http"
	"os"
	"strings"

	"tofu/internal/llm"
)

type ReadTool struct {
	root   Root
	ledger *ReadLedger
}

func NewReadTool(root string) (*ReadTool, error) {
	resolved, err := NewRoot(root)
	if err != nil {
		return nil, err
	}
	return &ReadTool{root: resolved}, nil
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

func (t *ReadTool) Run(_ context.Context, raw json.RawMessage) (Result, error) {
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
		t.ledger.Mark(args.Path, content)
		picture, said, err := lookAt(media, content)
		if err != nil {
			return Result{}, fmt.Errorf("read: %s: %w", args.Path, err)
		}
		return Result{Content: repair + args.Path + ": " + said, Images: []llm.Image{picture}, Command: args.Path}, nil
	}
	if args.StartLine <= 0 && args.EndLine <= 0 {
		t.ledger.Mark(args.Path, content)
		return Result{Content: repair + string(content), Command: args.Path}, nil
	}

	lines := strings.Split(strings.TrimSuffix(string(content), "\n"), "\n")
	start, end := args.StartLine, args.EndLine
	if start <= 0 {
		start = 1
	}
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
	t.ledger.MarkLines(args.Path, content, start, end)
	return Result{
		Content: repair + span + "\n" + strings.Join(lines[start-1:end], "\n"),
		Command: span,
	}, nil
}

func lookAt(media string, content []byte) (llm.Image, string, error) {
	format := strings.TrimPrefix(media, "image/")
	fits := base64.StdEncoding.EncodedLen(len(content)) <= llm.ImageEncodedBytes
	if media == "image/webp" {
		if !fits {
			return llm.Image{}, "", fmt.Errorf("a webp of %d bytes is past what a model takes and tofu cannot shrink a webp; convert it to png or jpeg", len(content))
		}
		return llm.Image{MediaType: media, Data: content}, fmt.Sprintf("webp, %d bytes, attached as a picture", len(content)), nil
	}
	decoded, _, err := image.Decode(bytes.NewReader(content))
	if err != nil {
		return llm.Image{}, "", fmt.Errorf("it looks like a %s and does not decode: %w", format, err)
	}
	width, height := decoded.Bounds().Dx(), decoded.Bounds().Dy()
	said := fmt.Sprintf("%s, %dx%d, %d bytes", format, width, height, len(content))
	fitWidth, fitHeight := fitted(width, height)
	if fits && fitWidth == width && fitHeight == height {
		return llm.Image{MediaType: media, Data: content}, said + ", attached as a picture", nil
	}
	small := shrunk(decoded, fitWidth, fitHeight)
	var encoded bytes.Buffer
	if media != "image/jpeg" {
		media, err = "image/png", png.Encode(&encoded, small)
	}
	if media == "image/jpeg" || base64.StdEncoding.EncodedLen(encoded.Len()) > llm.ImageEncodedBytes {
		encoded.Reset()
		media, err = "image/jpeg", jpeg.Encode(&encoded, small, nil)
	}
	if err != nil {
		return llm.Image{}, "", fmt.Errorf("shrinking it to %dx%d: %w", fitWidth, fitHeight, err)
	}
	return llm.Image{MediaType: media, Data: encoded.Bytes()}, fmt.Sprintf("%s, shrunk to %dx%d (%s, %d bytes) and attached as a picture",
		said, fitWidth, fitHeight, strings.TrimPrefix(media, "image/"), encoded.Len()), nil
}

func fitted(width, height int) (int, int) {
	if long := max(width, height); long > llm.ImageLongEdgePixels {
		width, height = max(1, width*llm.ImageLongEdgePixels/long), max(1, height*llm.ImageLongEdgePixels/long)
	}
	if width*height > llm.ImagePixels {
		scale := math.Sqrt(float64(llm.ImagePixels) / float64(width*height))
		width, height = max(1, int(float64(width)*scale)), max(1, int(float64(height)*scale))
	}
	return width, height
}

func shrunk(picture image.Image, width, height int) *image.RGBA {
	bounds := picture.Bounds()
	source := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(source, source.Bounds(), picture, bounds.Min, draw.Src)
	out := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		top, bottom := y*bounds.Dy()/height, (y+1)*bounds.Dy()/height
		for x := range width {
			left, right := x*bounds.Dx()/width, (x+1)*bounds.Dx()/width
			var sum [4]int
			for row := top; row < bottom; row++ {
				for i, value := range source.Pix[source.PixOffset(left, row):source.PixOffset(right, row)] {
					sum[i%4] += int(value)
				}
			}
			at, count := out.PixOffset(x, y), (bottom-top)*(right-left)
			for channel, total := range sum {
				out.Pix[at+channel] = uint8(total / count)
			}
		}
	}
	return out
}
