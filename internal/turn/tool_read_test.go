package turn

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/llm"
)

func madeUpPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	picture := image.NewRGBA(image.Rect(0, 0, width, height))
	for i := range picture.Pix {
		picture.Pix[i] = byte(i >> 10)
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, picture); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

func TestReadHandsAPictureToTheModel(t *testing.T) {
	root := t.TempDir()
	small := madeUpPNG(t, 300, 200)
	webp := append([]byte("RIFF\x00\x00\x00\x00WEBPVP8 "), make([]byte, 32)...)
	files := map[string][]byte{
		"small.png":  small,
		"wide.png":   madeUpPNG(t, 3000, 1000),
		"square.png": madeUpPNG(t, 2000, 2000),
		"notes.png":  []byte("plain notes\n"),
		"noext":      small,
		"broken.png": []byte("\x89PNG\r\n\x1a\nnot a png at all"),
		"tiny.webp":  webp,
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(root, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	read, err := NewReadTool(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		args      string
		media     string
		sent      []byte
		width     int
		height    int
		said      []string
		refused   bool
		plainText string
	}{
		{args: `{"path":"small.png"}`, media: "image/png", sent: small, width: 300, height: 200, said: []string{"small.png", "300x200"}},
		{args: `{"path":"small.png","start_line":2,"end_line":3}`, media: "image/png", sent: small, width: 300, height: 200},
		{args: `{"path":"noext"}`, media: "image/png", sent: small, width: 300, height: 200},
		{args: `{"path":"wide.png"}`, media: "image/png", width: 1568, height: 522, said: []string{"3000x1000", "1568x522"}},
		{args: `{"path":"square.png"}`, media: "image/png", width: 1072, height: 1072, said: []string{"2000x2000", "1072x1072"}},
		{args: `{"path":"tiny.webp"}`, media: "image/webp", sent: webp, said: []string{"tiny.webp", "webp"}},
		{args: `{"path":"notes.png"}`, plainText: "plain notes"},
		{args: `{"path":"broken.png"}`, refused: true},
	} {
		t.Run(c.args, func(t *testing.T) {
			result, err := read.Run(context.Background(), json.RawMessage(c.args))
			if c.refused {
				if err == nil {
					t.Fatalf("a file that only looks like a png was sent as %d image(s) and %q, want a refusal", len(result.Images), result.Content)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if c.plainText != "" {
				if len(result.Images) != 0 || !strings.Contains(result.Content, c.plainText) {
					t.Fatalf("a text file named like a picture came back as %d image(s) and %q", len(result.Images), result.Content)
				}
				return
			}
			if len(result.Images) != 1 || result.Images[0].MediaType != c.media {
				t.Fatalf("got %d image(s) and %q, want one %s", len(result.Images), result.Content, c.media)
			}
			if c.sent != nil && !bytes.Equal(result.Images[0].Data, c.sent) {
				t.Errorf("a picture inside every limit was re-encoded: %d bytes sent for %d on disk", len(result.Images[0].Data), len(c.sent))
			}
			if c.width > 0 {
				config, _, err := image.DecodeConfig(bytes.NewReader(result.Images[0].Data))
				if err != nil || config.Width != c.width || config.Height != c.height {
					t.Errorf("sent a %dx%d picture (%v), want %dx%d", config.Width, config.Height, err, c.width, c.height)
				}
			}
			for _, said := range c.said {
				if !strings.Contains(result.Content, said) {
					t.Errorf("the text %q does not say %q", result.Content, said)
				}
			}
		})
	}
}

type replayedRead map[string]Result

func (r replayedRead) Name() string { return "read" }

func (r replayedRead) Definition() llm.Tool { return llm.Tool{Name: "read"} }

func (r replayedRead) Run(_ context.Context, raw json.RawMessage) (Result, error) {
	var args struct{ Call string }
	err := json.Unmarshal(raw, &args)
	return r[args.Call], err
}

func readOf(id string) llm.ToolCall {
	return llm.ToolCall{ID: id, Name: "read", Arguments: json.RawMessage(`{"path":"a.go","call":"` + id + `"}`)}
}

type sentAs struct {
	whole     string
	pointedAt string
}

func TestARepeatedReadPointsAtTheCopyTheNextRequestStillCarries(t *testing.T) {
	file := strings.Repeat("func f() int { return 1 }\n", 40)
	first, repeat := Result{Content: file}, Result{Content: file, Repeat: true}
	rows := []struct {
		name      string
		answers   replayedRead
		decisions []llm.Decision
		want      []sentAs
	}{
		{"a repeat in a later step", replayedRead{"call-1": first, "call-2": repeat},
			[]llm.Decision{toolCallDecision(readOf("call-1")), toolCallDecision(readOf("call-2")), messageDecision()},
			[]sentAs{{whole: file}, {pointedAt: "call-1"}}},
		{"a repeat in the same parallel wave", replayedRead{"call-1": first, "call-2": repeat},
			[]llm.Decision{toolCallDecision(readOf("call-1"), readOf("call-2")), messageDecision()},
			[]sentAs{{whole: file}, {pointedAt: "call-1"}}},
		{"a repeat whose copy this history never held", replayedRead{"call-1": repeat},
			[]llm.Decision{toolCallDecision(readOf("call-1")), messageDecision()},
			[]sentAs{{whole: file}}},
		{"a repeat whose earlier copy is not word for word the text", replayedRead{"call-1": {Content: file[:100]}, "call-2": repeat},
			[]llm.Decision{toolCallDecision(readOf("call-1")), toolCallDecision(readOf("call-2")), messageDecision()},
			[]sentAs{{whole: file[:100]}, {whole: file}}},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			model := &stubModel{decisions: row.decisions}
			_, err := Run(context.Background(), Config{Model: model, Spend: SpendAPIKey, Tools: NewRegistry(row.answers),
				Task: "read a.go", ResultBytesCap: 1 << 20, ArtifactDir: t.TempDir(), NoLastWord: true})
			if err != nil {
				t.Fatal(err)
			}
			var sent []string
			for _, message := range model.requests[len(model.requests)-1].Messages {
				if message.Role == llm.RoleTool {
					sent = append(sent, message.Content)
				}
			}
			if len(sent) != len(row.want) {
				t.Fatalf("the last request carries %d tool results, want %d", len(sent), len(row.want))
			}
			for i, want := range row.want {
				if want.whole != "" && !strings.HasSuffix(sent[i], want.whole) {
					t.Errorf("result %d is %q, want it to end with the whole text", i+1, sent[i])
				}
				if want.pointedAt != "" && (strings.Contains(sent[i], "\n") || !strings.Contains(sent[i], want.pointedAt)) {
					t.Errorf("result %d is %q, want one line naming %s", i+1, sent[i], want.pointedAt)
				}
			}
		})
	}
}
