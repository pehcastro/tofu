package anthropic

import (
	"encoding/json"
	"strings"
	"testing"

	"boji/internal/llm"
	"boji/internal/transport"
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

func TestEncodeGeneratesTheMetadataEnvelopeOnOAuthOnly(t *testing.T) {
	request := minimalRequest()
	request.SessionID = "11111111-2222-3333-4444-555555555555"
	request.AccountID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	request.InstallID = "install-1"

	body, err := request.Encode(true)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	var decoded struct {
		Metadata *wireMetadata `json:"metadata"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if decoded.Metadata == nil {
		t.Fatal("an oauth request carries metadata")
	}
	var userID claudeUserID
	if err := json.Unmarshal([]byte(decoded.Metadata.UserID), &userID); err != nil {
		t.Fatalf("user_id is not the json envelope: %q", decoded.Metadata.UserID)
	}
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
