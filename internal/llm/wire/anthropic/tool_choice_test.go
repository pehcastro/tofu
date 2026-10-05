package anthropic

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"tofu/internal/konst"
	"tofu/internal/llm"
)

func cappedTurnRequest(step int) Request {
	schema := map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}, "required": []string{"path"}}
	messages := []llm.Message{{Role: llm.RoleUser, Content: "read the five notes, a to e, and say what each holds"}}
	for read := 1; read < step; read++ {
		id := "toolu_" + strconv.Itoa(read)
		messages = append(messages,
			llm.Message{Role: llm.RoleAssistant, Content: "reading note " + strconv.Itoa(read), ToolCalls: []llm.ToolCall{{ID: id, Name: "read", Arguments: json.RawMessage(`{"path":"` + strconv.Itoa(read) + `.txt"}`)}}},
			llm.Message{Role: llm.RoleTool, ToolCallID: id, Content: strings.Repeat("note "+strconv.Itoa(read)+" ", 300)})
	}
	return Request{
		Model:     "claude-opus-5",
		System:    []string{"you are a coding agent", strings.Repeat("work in small steps. ", 200)},
		Messages:  messages,
		Tools:     []llm.Tool{{Name: "read", Description: "read a file", Parameters: schema}, {Name: "bash", Description: "run a command", Parameters: schema}, {Name: "edit", Description: "edit a file", Parameters: schema}},
		Effort:    llm.EffortHigh,
		SessionID: "0b8e2f4a-1c3d-4e5f-8a9b-0c1d2e3f4a5b",
		AccountID: "7d6c5b4a-3e2f-4a1b-9c8d-7e6f5a4b3c2d",
		InstallID: "install-1",
		CacheTTL:  konst.SubscriptionCacheTTL,
	}
}

func TestTheLastStepOfACappedTurnAddsOnlyToolChoiceNoneToTheBodySentBefore(t *testing.T) {
	sentAt21f4d95 := []string{
		"6da976c4eb531dd795bebfa7c5a860ba3ee28b813147d6e15928dc2d4a197154",
		"52650ccfc3834c118e9c53fa29f4dd769adf2c4da81dabe56c6e308e7e197a10",
		"a0e7be4602242ef5a8c463a074ba30e63b7dc17a0c34deb27fc5a1ad9526130e",
		"42a2b6d24493749b891f0504297037602ffad0c3fc0f6398c015bd2c2a696aa8",
		"b413a92247eb694ea1ea814b9ed11d4865d353eed7f93d3071103c89a5299003",
	}
	bodies := make([][]byte, len(sentAt21f4d95))
	for step := range sentAt21f4d95 {
		request := cappedTurnRequest(step + 1)
		if step == len(sentAt21f4d95)-1 {
			request.ToolChoice = llm.ToolChoiceNone
		}
		body, err := request.Encode(true)
		if err != nil {
			t.Fatal(err)
		}
		bodies[step] = body
		asBefore := bytes.Replace(body, []byte(`"tool_choice":{"type":"none"},`), nil, 1)
		if step == len(sentAt21f4d95)-1 && bytes.Equal(asBefore, body) {
			t.Errorf("request %d carries no tool_choice none", step+1)
		}
		sum := sha256.Sum256(asBefore)
		if got := hex.EncodeToString(sum[:]); got != sentAt21f4d95[step] {
			t.Errorf("request %d hashes to %s with tool_choice taken out, and to %s at 21f4d95", step+1, got, sentAt21f4d95[step])
		}
	}
	var fourth, fifth struct {
		Tools      json.RawMessage `json:"tools"`
		ToolChoice json.RawMessage `json:"tool_choice"`
	}
	if json.Unmarshal(bodies[3], &fourth) != nil || json.Unmarshal(bodies[4], &fifth) != nil {
		t.Fatal("a body does not decode")
	}
	t.Logf("request 4: tool_choice %s, tools %d bytes\nrequest 5: tool_choice %s, tools %d bytes", fourth.ToolChoice, len(fourth.Tools), fifth.ToolChoice, len(fifth.Tools))
	if !bytes.Equal(fourth.Tools, fifth.Tools) || fourth.ToolChoice != nil || string(fifth.ToolChoice) != `{"type":"none"}` {
		t.Errorf("request 5 does not keep request 4's tools and add tool_choice none alone")
	}
}
