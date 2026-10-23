package anthropic

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"testing"

	"tofu/internal/llm"
)

func TestASignatureWithNoTextSendsNoThinkingBlock(t *testing.T) {
	messages := encodedThinkingMessages(t, llm.Thinking{Signature: "sig-only"})
	for _, block := range messages[1].Content {
		if block.Type == "thinking" {
			t.Fatalf("a signature with no text produced a thinking block: %+v", block)
		}
	}
}

func TestTextWithNoSignatureSendsAThinkingBlockWithNoSignature(t *testing.T) {
	block := encodedThinkingMessages(t, llm.Thinking{Text: "reasoning about it"})[1].Content[0]
	if block.Type != "thinking" || block.Thinking != "reasoning about it" || block.Signature != "" {
		t.Fatalf("text with no signature encoded as %+v", block)
	}
}

func TestAPictureAToolReturnedTravelsInsideItsResult(t *testing.T) {
	picture := llm.Image{MediaType: "image/png", Data: []byte("made-up png")}
	request := minimalRequest()
	request.Messages = append(request.Messages,
		llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "toolu_1", Name: "read"}, {ID: "toolu_2", Name: "read"}}},
		llm.Message{Role: llm.RoleTool, ToolCallID: "toolu_1", Content: "a.png is a png", Images: []llm.Image{picture}},
		llm.Message{Role: llm.RoleTool, ToolCallID: "toolu_2", Content: "b.png is a png", Images: []llm.Image{picture}})
	body, err := request.Encode(false)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Messages []struct {
			Content []struct {
				Type    string          `json:"type"`
				Content json.RawMessage `json:"content"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	results := decoded.Messages[len(decoded.Messages)-1].Content
	if len(results) != 2 {
		t.Fatalf("the two results became %d blocks, want two tool_result blocks with the pictures inside", len(results))
	}
	for _, result := range results {
		var inner []struct {
			Type   string `json:"type"`
			Source struct {
				MediaType string `json:"media_type"`
				Data      string `json:"data"`
			} `json:"source"`
		}
		if err := json.Unmarshal(result.Content, &inner); err != nil {
			t.Fatalf("a %s carries %s, want an array of a text and an image: %v", result.Type, result.Content, err)
		}
		if len(inner) != 2 || inner[0].Type != "text" || inner[1].Type != "image" || inner[1].Source.MediaType != "image/png" || inner[1].Source.Data == "" {
			t.Fatalf("a %s carries %+v, want the text then the picture", result.Type, inner)
		}
	}
}

func encodedThinkingMessages(t *testing.T, thinking llm.Thinking) []wireMessage {
	t.Helper()
	request := minimalRequest()
	request.Messages = append(request.Messages,
		llm.Message{Role: llm.RoleAssistant, Thinking: thinking,
			ToolCalls: []llm.ToolCall{{ID: "toolu_1", Name: "read"}}},
		llm.Message{Role: llm.RoleTool, ToolCallID: "toolu_1", Content: "a file"})

	body, err := request.Encode(true)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	var decoded struct {
		Messages []wireMessage `json:"messages"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	return decoded.Messages
}

func TestAPastedPicturePastTheLimitsIsSentFittedAndOneInsideThemIsSentAsItIs(t *testing.T) {
	encodedPNG := func(width, height int) []byte {
		var out bytes.Buffer
		if err := png.Encode(&out, image.NewGray(image.Rect(0, 0, width, height))); err != nil {
			t.Fatal(err)
		}
		return out.Bytes()
	}
	huge, small, opaque := encodedPNG(4000, 3000), encodedPNG(300, 200), []byte("made-up png")
	request := minimalRequest()
	request.Messages = append(request.Messages, llm.Message{Role: llm.RoleUser, Content: "look", Images: []llm.Image{
		{MediaType: "image/png", Data: huge}, {MediaType: "image/png", Data: small}, {MediaType: "image/png", Data: opaque}}})
	body, err := request.Encode(true)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Messages []wireMessage `json:"messages"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	var sent [][]byte
	for _, block := range decoded.Messages[len(decoded.Messages)-1].Content {
		if block.Type == "image" {
			data, err := base64.StdEncoding.DecodeString(block.Source.Data)
			if err != nil {
				t.Fatal(err)
			}
			sent = append(sent, data)
		}
	}
	if len(sent) != 3 {
		t.Fatalf("three pictures went in and %d came out", len(sent))
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(sent[0]))
	if err != nil || max(config.Width, config.Height) > llm.ImageLongEdgePixels || config.Width*config.Height > llm.ImagePixels {
		t.Errorf("a 4000x3000 paste was sent at %dx%d (%v), past the %d pixel long edge", config.Width, config.Height, err, llm.ImageLongEdgePixels)
	}
	if !bytes.Equal(sent[1], small) || !bytes.Equal(sent[2], opaque) {
		t.Errorf("a picture inside the limits, or one that does not decode, was re-encoded: %d and %d bytes sent for %d and %d", len(sent[1]), len(sent[2]), len(small), len(opaque))
	}
}
