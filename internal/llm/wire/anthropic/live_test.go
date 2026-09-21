package anthropic

import (
	"bytes"
	"cmp"
	"context"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"tofu/internal/llm"
	"tofu/internal/llm/cred"
)

const (
	liveModel                  = "claude-sonnet-4-5-20250929"
	headerOnlyCacheWriteTokens = 2997
)

func liveWire(t *testing.T) *Wire {
	t.Helper()
	if os.Getenv("TOFU_LIVE_ANTHROPIC") != "1" {
		t.Skip("set TOFU_LIVE_ANTHROPIC=1 to spend a turn of the subscription quota")
	}
	path, err := cred.Path()
	if err != nil {
		t.Fatalf("locating the credential store: %v", err)
	}
	store, err := cred.Open(path)
	if err != nil {
		t.Skipf("no credential store, run tofu login anthropic: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	spec, err := cred.Lookup("anthropic")
	if err != nil {
		t.Fatalf("looking up the anthropic spec: %v", err)
	}

	wire, err := New(Config{
		Model:     cmp.Or(os.Getenv("TOFU_LIVE_ANTHROPIC_MODEL"), liveModel),
		Token:     cred.NewManager(store, spec).Access,
		Watchdog:  2 * time.Minute,
		SessionID: "00000000-0000-4000-8000-000000000001",
		InstallID: "tofu-live-test",
	})
	if err != nil {
		t.Fatalf("building the wire: %v", err)
	}
	return wire
}

func TestLiveCompletion(t *testing.T) {
	wire := liveWire(t)
	result, dump, err := wire.Ask(context.Background(), Request{
		MaxTokens: 16,
		Messages:  []llm.Message{{Role: llm.RoleUser, Content: "Reply with the single word: ok"}},
	})
	t.Logf("request dump:\n%s", dump)
	if err != nil {
		t.Fatalf("asking: %v", err)
	}
	if dump.Attestation != AttestationPatched {
		t.Fatalf("attestation is %s", dump.Attestation)
	}
	t.Logf("model %s stop %s content %q usage %+v warnings %v",
		result.Model, result.Stop, result.Content, result.Usage, result.Warnings)
	if result.Content == "" {
		t.Fatal("the live turn returned no content")
	}
}

func TestLiveWritesTheInstructionPrefixOnTheFirstRequest(t *testing.T) {
	wire := liveWire(t)
	instructions, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "CLAUDE.md"))
	if err != nil {
		t.Fatalf("reading the project instructions: %v", err)
	}
	nonce := strconv.FormatInt(time.Now().UnixNano(), 36)
	system := []string{"conversation " + nonce + " starts here.\n" + string(instructions)}
	tools := []llm.Tool{{Name: "read", Description: "Read a file."}, {Name: "write", Description: "Write a file."}}
	messages := []llm.Message{{Role: llm.RoleUser, Content: "Reply with the single word: ok"}}

	first, dump, err := wire.Ask(context.Background(), Request{
		MaxTokens: 16, System: system, Tools: tools, Messages: messages})
	if err != nil {
		t.Fatalf("first send: %v", err)
	}
	t.Logf("system prefix %d bytes, breakpoints %d",
		len(system[0]), bytes.Count(dump.Body, []byte(`"cache_control"`)))
	t.Logf("send 1 step 1: cache_read %d cache_write %d in %d out %d",
		first.Usage.CacheRead, first.Usage.CacheWrite, first.Usage.Input, first.Usage.Output)
	if first.Usage.CacheWrite <= headerOnlyCacheWriteTokens {
		t.Fatalf("the first send wrote %d cached tokens, no more than the %d header-only figure",
			first.Usage.CacheWrite, headerOnlyCacheWriteTokens)
	}

	second, _, err := wire.Ask(context.Background(), Request{MaxTokens: 16, System: system, Tools: tools,
		Messages: append(messages,
			llm.Message{Role: llm.RoleAssistant, Content: first.Content},
			llm.Message{Role: llm.RoleUser, Content: "Reply with the single word: ok"})})
	if err != nil {
		t.Fatalf("second send: %v", err)
	}
	t.Logf("send 2 step 1: cache_read %d cache_write %d in %d out %d",
		second.Usage.CacheRead, second.Usage.CacheWrite, second.Usage.Input, second.Usage.Output)
	if second.Usage.CacheRead < first.Usage.CacheWrite {
		t.Fatalf("the second send read %d of the %d the first wrote",
			second.Usage.CacheRead, first.Usage.CacheWrite)
	}
}

func redSquarePNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: color.RGBA{R: 220, G: 20, B: 20, A: 255}}, image.Point{}, draw.Src)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encoding the probe image: %v", err)
	}
	return buf.Bytes()
}

func TestLiveDescribesAPastedImage(t *testing.T) {
	wire := liveWire(t)
	messages := []llm.Message{{Role: llm.RoleUser, Content: "What single color is this square? Reply with one word.",
		Images: []llm.Image{{MediaType: "image/png", Data: redSquarePNG(t)}}}}
	result, dump, err := wire.Ask(context.Background(), Request{MaxTokens: 16, Messages: messages})
	t.Logf("request dump:\n%s", dump)
	if err != nil {
		t.Fatalf("asking: %v", err)
	}
	t.Logf("model %s content %q usage %+v", result.Model, result.Content, result.Usage)
	if result.Content == "" {
		t.Fatal("the live turn returned no content")
	}
	if !strings.Contains(strings.ToLower(result.Content), "red") {
		t.Fatalf("the model described the red square as %q, want it naming red", result.Content)
	}
}

func TestLiveToolCallRoundTrip(t *testing.T) {
	wire := liveWire(t)
	tools := []llm.Tool{{
		Name:        "_probe",
		Description: "Return the answer to a fixed question.",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{"question": map[string]any{"type": "string"}},
			"required":   []string{"question"},
		},
	}}
	messages := []llm.Message{{Role: llm.RoleUser,
		Content: "Call the _probe tool once with question=\"colour\", then reply with its answer and nothing else."}}

	first, dump, err := wire.Ask(context.Background(), Request{MaxTokens: 256, Tools: tools, Messages: messages})
	t.Logf("request dump:\n%s", dump)
	if err != nil {
		t.Fatalf("first turn: %v", err)
	}
	if first.Stop != StopToolUse || len(first.ToolCalls) != 1 {
		t.Fatalf("the model did not call the tool once: %+v", first)
	}
	t.Logf("first turn stop %s call %s %s %s", first.Stop,
		first.ToolCalls[0].ID, first.ToolCalls[0].Name, first.ToolCalls[0].Arguments)
	if first.ToolCalls[0].Name != "_probe" {
		t.Fatalf("the tool came back as %q", first.ToolCalls[0].Name)
	}

	messages = append(messages,
		llm.Message{Role: llm.RoleAssistant, Content: first.Content, ToolCalls: first.ToolCalls},
		llm.Message{Role: llm.RoleTool, ToolCallID: first.ToolCalls[0].ID, Content: "chartreuse"},
	)
	second, secondDump, err := wire.Ask(context.Background(), Request{MaxTokens: 64, Tools: tools, Messages: messages})
	t.Logf("second request dump:\n%s", secondDump)
	if err != nil {
		t.Fatalf("second turn: %v", err)
	}
	t.Logf("second turn stop %s content %q usage %+v", second.Stop, second.Content, second.Usage)
	if second.Stop != StopEnd || second.Content == "" {
		t.Fatalf("the turn did not complete: %+v", second)
	}
}
