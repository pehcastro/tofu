package anthropic

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"tofu/internal/golden"
	"tofu/internal/llm"
)

func TestTheCommittedBreakpointMovesForwardAsTheConversationGrows(t *testing.T) {
	var anchors, tips []int
	for exchanges := 2; exchanges <= 4; exchanges++ {
		indexes := messageBreakpoints(t, historyRequest(exchanges))
		if len(indexes) != 2 {
			t.Fatalf("%d exchanges broke the cache at %v, want an anchor and a tip", exchanges, indexes)
		}
		anchors = append(anchors, indexes[0])
		tips = append(tips, indexes[1])
	}
	for request := 1; request < len(anchors); request++ {
		if anchors[request] <= anchors[request-1] {
			t.Fatalf("the anchor sat at %v over three successive requests and never moved forward", anchors)
		}
		if tips[request] <= tips[request-1] {
			t.Fatalf("the tip sat at %v over three successive requests", tips)
		}
		if anchors[request] != tips[request]-2 {
			t.Fatalf("request %d anchored at %d with its tip at %d, want the anchor on the last committed message",
				request, anchors[request], tips[request])
		}
	}
}

func TestACommittedPrefixUnderTheMinimumGetsNoSecondBreakpoint(t *testing.T) {
	request := minimalRequest()
	request.Messages = append(request.Messages, exchange(1, "a short first result")...)
	request.Messages = append(request.Messages, exchange(2, strings.Repeat("x", historyCacheMinPrefixChars))...)

	indexes := messageBreakpoints(t, request)
	if len(indexes) != 1 || indexes[0] != len(request.Messages)-1 {
		t.Fatalf("a committed prefix under %d characters was anchored at %v",
			historyCacheCommittedMinChars, indexes)
	}
}

func TestALongConversationStaysInsideTheBreakpointAllowance(t *testing.T) {
	request := historyRequest(40)
	request.System = []string{"the project instructions"}
	request.Tools = []llm.Tool{{Name: "read"}, {Name: "write"}}
	body, err := request.Encode(true)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	if count := strings.Count(string(body), `"cache_control"`); count > cacheBreakpointsPerRequest {
		t.Fatalf("a 40 exchange conversation carries %d breakpoints, past the %d the vendor allows",
			count, cacheBreakpointsPerRequest)
	}
}

func TestTheEncodedRequestPlacesItsMarkersWhereTheGoldenSays(t *testing.T) {
	request := minimalRequest()
	for step := 1; step <= 3; step++ {
		request.Messages = append(request.Messages, exchange(step, strings.Repeat("read\n", 420))...)
	}
	body, err := request.Encode(true)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	var indented bytes.Buffer
	if err := json.Indent(&indented, body, "", "  "); err != nil {
		t.Fatalf("indenting: %v", err)
	}

	golden.Assert(t, "history-caching.golden", indented.String())
}

func TestTheWriteSplitFromMessageStartSurvivesTheClosingUsage(t *testing.T) {
	var stream strings.Builder
	for _, event := range []string{
		`{"type":"message_start","message":{"id":"msg_1","model":"claude-test","usage":{"input_tokens":3,"cache_read_input_tokens":7,"cache_creation_input_tokens":30,"cache_creation":{"ephemeral_5m_input_tokens":10,"ephemeral_1h_input_tokens":20}}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"done"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2,"cache_creation_input_tokens":30}}`,
		`{"type":"message_stop"}`,
	} {
		stream.WriteString("data: " + event + "\n\n")
	}
	result, err := ReadStream(strings.NewReader(stream.String()), true, nil, nil)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if want := (Usage{Input: 3, Output: 2, CacheRead: 7, CacheWrite: 30, CacheWrite5m: 10, CacheWrite1h: 20}); result.Usage != want {
		t.Fatalf("usage %+v, want %+v", result.Usage, want)
	}
}

func minimalRequest() Request {
	return Request{
		Model:    "claude-opus-4-1-20250805",
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "say ok"}},
	}
}

func exchange(step int, bulk string) []llm.Message {
	call := fmt.Sprintf("toolu_%d", step)
	return []llm.Message{
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: call, Name: "read"}}},
		{Role: llm.RoleTool, ToolCallID: call, Content: bulk},
	}
}

func historyRequest(exchanges int) Request {
	request := minimalRequest()
	bulk := strings.Repeat("a line of a file that was read\n", 200)
	for step := 1; step <= exchanges; step++ {
		request.Messages = append(request.Messages, exchange(step, bulk)...)
	}
	return request
}

func messageBreakpoints(t *testing.T, request Request) []int {
	t.Helper()
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
	var indexes []int
	for index, message := range decoded.Messages {
		for _, block := range message.Content {
			if block.CacheControl != nil {
				indexes = append(indexes, index)
			}
		}
	}
	return indexes
}
