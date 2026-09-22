package anthropic

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"tofu/internal/llm"
	"tofu/internal/transport"
)

func minimalRequest() Request {
	return Request{
		Model:    "claude-opus-4-1-20250805",
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "say ok"}},
	}
}

func TestEncodeInjectsTheBillingHeaderThenTheIdentityLine(t *testing.T) {
	body, err := minimalRequest().Encode(true)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	var decoded struct {
		System []systemBlock `json:"system"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(decoded.System) != 2 {
		t.Fatalf("system blocks are %+v", decoded.System)
	}
	if !strings.HasPrefix(decoded.System[0].Text, BillingHeaderPrefix) {
		t.Fatalf("block 0 is %q", decoded.System[0].Text)
	}
	if decoded.System[0].CacheControl != nil {
		t.Fatal("the per-request billing block must not be a cache breakpoint")
	}
	if decoded.System[1].Text != ClaudeCodeSystemIdentity {
		t.Fatalf("block 1 is %q", decoded.System[1].Text)
	}
	if decoded.System[1].CacheControl == nil {
		t.Fatal("the identity block carries the cache control")
	}
}

func TestEncodePutsTheCallersInstructionsAtBlockTwo(t *testing.T) {
	request := minimalRequest()
	request.System = []string{"be terse"}
	body, err := request.Encode(true)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	var decoded struct {
		System []systemBlock `json:"system"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(decoded.System) != 3 || decoded.System[2].Text != "be terse" {
		t.Fatalf("system blocks are %+v", decoded.System)
	}
}

func TestEncodeSkipsInjectionWhenTheCallerAlreadySuppliedABillingBlock(t *testing.T) {
	request := minimalRequest()
	request.System = []string{BillingHeaderPrefix + " cc_version=x; cc_entrypoint=cli; cch=00000;"}
	body, err := request.Encode(true)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	if strings.Count(string(body), ClaudeCodeSystemIdentity) != 0 {
		t.Fatalf("the identity block was injected on top of a caller block: %s", body)
	}
}

func TestEncodeSendsNoSystemBlocksOnAnAPIKey(t *testing.T) {
	body, err := minimalRequest().Encode(false)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	if strings.Contains(string(body), "system") {
		t.Fatalf("an api key request carried system blocks: %s", body)
	}
}

func TestEncodeSerializesMessagesBeforeSystem(t *testing.T) {
	body, err := minimalRequest().Encode(true)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	messages := strings.Index(string(body), `"messages"`)
	system := strings.Index(string(body), `"system"`)
	if messages < 0 || system < 0 || messages > system {
		t.Fatalf("messages at %d, system at %d: the anchor bound depends on this order", messages, system)
	}
}

func TestEncodeClampsMaxTokensOnOAuthOnly(t *testing.T) {
	request := minimalRequest()
	request.MaxTokens = 200000

	oauthBody, err := request.Encode(true)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	if !strings.Contains(string(oauthBody), `"max_tokens":64000`) {
		t.Fatalf("oauth body is %s", oauthBody)
	}

	keyBody, err := request.Encode(false)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	if !strings.Contains(string(keyBody), `"max_tokens":200000`) {
		t.Fatalf("api key body is %s", keyBody)
	}
}

func TestEncodePrefixesEveryToolAndAnchorsTheLastOne(t *testing.T) {
	request := minimalRequest()
	request.Tools = []llm.Tool{{Name: "_probe"}, {Name: "read"}, {Name: "web_search"}}
	body, err := request.Encode(true)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	var decoded struct {
		Tools []wireTool `json:"tools"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	names := []string{decoded.Tools[0].Name, decoded.Tools[1].Name, decoded.Tools[2].Name}
	if names[0] != "__probe" || names[1] != "_read" || names[2] != "web_search" {
		t.Fatalf("tool names are %v", names)
	}
	if decoded.Tools[2].CacheControl == nil || decoded.Tools[0].CacheControl != nil {
		t.Fatalf("the breakpoint sits on %+v", decoded.Tools)
	}
}

func TestEncodeMergesConsecutiveToolResultsIntoOneUserTurn(t *testing.T) {
	request := minimalRequest()
	request.Messages = append(request.Messages,
		llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
			{ID: "toolu_1", Name: "_probe", Arguments: json.RawMessage(`{"a":1}`)},
			{ID: "toolu_2", Name: "read"},
		}},
		llm.Message{Role: llm.RoleTool, ToolCallID: "toolu_1", Content: "one"},
		llm.Message{Role: llm.RoleTool, ToolCallID: "toolu_2", Content: "two"},
	)
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
	if len(decoded.Messages) != 3 {
		t.Fatalf("messages are %+v", decoded.Messages)
	}
	if len(decoded.Messages[2].Content) != 2 || decoded.Messages[2].Role != "user" {
		t.Fatalf("the tool results are %+v", decoded.Messages[2])
	}
	if decoded.Messages[1].Content[0].Name != "__probe" {
		t.Fatalf("a replayed tool call went out as %q", decoded.Messages[1].Content[0].Name)
	}
	if string(decoded.Messages[1].Content[1].Input) != "{}" {
		t.Fatalf("an argumentless call went out as %s", decoded.Messages[1].Content[1].Input)
	}
}

func TestEncodeRefusesASystemRoleMessage(t *testing.T) {
	request := minimalRequest()
	request.Messages = []llm.Message{{Role: llm.RoleSystem, Content: "x"}}
	if _, err := request.Encode(true); transport.KindOf(err) != transport.KindBadRequest {
		t.Fatalf("error is %v", err)
	}
}

func TestEncodeSendsAnImageAsAnImageBlock(t *testing.T) {
	request := minimalRequest()
	request.Messages = []llm.Message{{Role: llm.RoleUser, Content: "what is this",
		Images: []llm.Image{{MediaType: "image/png", Data: []byte("pretend png bytes")}}}}
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
	if len(decoded.Messages) != 1 || len(decoded.Messages[0].Content) != 2 {
		t.Fatalf("the message is %+v", decoded.Messages)
	}
	image := decoded.Messages[0].Content[1]
	if image.Type != "image" || image.Source == nil {
		t.Fatalf("the second block is %+v, want an image block", image)
	}
	if image.Source.Type != "base64" || image.Source.MediaType != "image/png" {
		t.Fatalf("the image source is %+v", image.Source)
	}
	if image.Source.Data != base64.StdEncoding.EncodeToString([]byte("pretend png bytes")) {
		t.Fatalf("the image data was not base64 encoded")
	}
}

func TestEncodeRefusesAnImageOverTheCap(t *testing.T) {
	request := minimalRequest()
	request.Messages = []llm.Message{{Role: llm.RoleUser, Content: "look",
		Images: []llm.Image{{MediaType: "image/png", Data: make([]byte, ImageBytesCap+1)}}}}
	_, err := request.Encode(true)
	if transport.KindOf(err) != transport.KindBadRequest {
		t.Fatalf("error is %v", err)
	}
	if !strings.Contains(err.Error(), strconv.Itoa(ImageBytesCap+1)) {
		t.Fatalf("the refusal does not name the size: %v", err)
	}
}

func TestEncodeLeavesAngleBracketsUnescaped(t *testing.T) {
	request := minimalRequest()
	request.Messages = []llm.Message{{Role: llm.RoleUser, Content: "a <b> & c"}}
	body, err := request.Encode(true)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	if !strings.Contains(string(body), "a <b> & c") {
		t.Fatalf("go escaped html where the vendor client does not: %s", body)
	}
}

func encodedUserID(t *testing.T, body []byte) claudeUserID {
	t.Helper()
	var envelope struct {
		Metadata *wireMetadata `json:"metadata"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("the request body is not json: %v", err)
	}
	if envelope.Metadata == nil {
		t.Fatal("the request carries no metadata")
	}
	var userID claudeUserID
	if err := json.Unmarshal([]byte(envelope.Metadata.UserID), &userID); err != nil {
		t.Fatalf("user_id is not the json envelope: %q", envelope.Metadata.UserID)
	}
	return userID
}

func TestEncodeGeneratesTheMetadataEnvelopeOnOAuthOnly(t *testing.T) {
	request := minimalRequest()
	request.SessionID = "11111111-2222-3333-4444-555555555555"
	request.AccountID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	request.InstallID = "install-1"

	body, err := request.Encode(true)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	userID := encodedUserID(t, body)
	if userID.SessionID != request.SessionID || userID.AccountUUID != request.AccountID || userID.DeviceID == "" {
		t.Fatalf("user_id is %+v", userID)
	}

	keyBody, err := request.Encode(false)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	if strings.Contains(string(keyBody), "metadata") {
		t.Fatalf("an api key request carried metadata: %s", keyBody)
	}
}

func TestEncodeKeepsACallerSessionIDInsteadOfDrawingAFreshOne(t *testing.T) {
	request := minimalRequest()
	request.UserID = `{"session_id":"kept"}`
	body, err := request.Encode(true)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	if !strings.Contains(string(body), `{\"session_id\":\"kept\"}`) {
		t.Fatalf("the caller envelope was overwritten: %s", body)
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

func TestEncodeBreaksTheCacheOnTheHistoryAndNotOnlyOnTheHead(t *testing.T) {
	indexes := messageBreakpoints(t, historyRequest(2))
	if len(indexes) != 2 || indexes[0] != 2 || indexes[1] != 4 {
		t.Fatalf("message breakpoints sit at %v, want the anchor at 2 and the moving one at 4", indexes)
	}
}

func TestEncodeLeavesAHistoryShorterThanTheCacheMinimumAlone(t *testing.T) {
	short := minimalRequest()
	short.Messages = append(short.Messages, exchange(1, "one")...)
	if indexes := messageBreakpoints(t, short); indexes != nil {
		t.Fatalf("a history well under %d characters was broken at %v", historyCacheMinPrefixChars, indexes)
	}

	oneExchange := minimalRequest()
	oneExchange.Messages = append(oneExchange.Messages,
		exchange(1, strings.Repeat("x", historyCacheMinPrefixChars))...)
	if indexes := messageBreakpoints(t, oneExchange); len(indexes) != 1 || indexes[0] != 2 {
		t.Fatalf("one exchange has no earlier prefix to anchor, breakpoints are %v", indexes)
	}
}

func TestEncodeMarksTheInstructionBlockOnTheOAuthPath(t *testing.T) {
	request := minimalRequest()
	request.System = []string{"the project instructions"}
	body, err := request.Encode(true)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	var decoded struct {
		System []systemBlock `json:"system"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	last := decoded.System[len(decoded.System)-1]
	if last.Text != "the project instructions" {
		t.Fatalf("the last system block is %q", last.Text)
	}
	if last.CacheControl == nil {
		t.Fatal("the instruction block sits outside the cached prefix on the first request")
	}
}

func TestEncodeSpendsTheFourBreakpointsHeadFirst(t *testing.T) {
	request := historyRequest(4)
	request.System = []string{"the project instructions"}
	request.Tools = []llm.Tool{{Name: "read"}, {Name: "write"}}
	body, err := request.Encode(true)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	if count := strings.Count(string(body), `"cache_control"`); count != cacheBreakpointsPerRequest {
		t.Fatalf("the request carries %d breakpoints, want exactly %d", count, cacheBreakpointsPerRequest)
	}
	if indexes := messageBreakpoints(t, request); len(indexes) != 1 || indexes[0] != len(request.Messages)-1 {
		t.Fatalf("the history keeps %v, want the moving breakpoint alone on the last message", indexes)
	}
}

func TestEncodeOffArmLeavesTheHistoryAloneAndKeepsTheHead(t *testing.T) {
	request := historyRequest(2)
	request.HistoryCacheOff = true
	if indexes := messageBreakpoints(t, request); indexes != nil {
		t.Fatalf("the off arm still broke the cache at %v", indexes)
	}
	body, err := request.Encode(true)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	if !strings.Contains(string(body), ClaudeCodeSystemIdentity) ||
		!strings.Contains(string(body), `"cache_control"`) {
		t.Fatalf("the head breakpoint went away with the history one: %s", body)
	}
}
