package prefix

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"

	"tofu/internal/llm/wire/anthropic"
	"tofu/internal/rule"
	"tofu/internal/turn"
)

const laterEnvironment = "<env>\nworking directory: Q:\\code\\bench\nplatform: windows/amd64\n</env>"

const AddedRuleID = "bench_added_mid_session"

const addedRule = "id: " + AddedRuleID + "\ndomain: dev\nkind: human\nconcern: code_rules\ntext: name every file you touch in the reply.\n"

type LaterArm struct {
	RuleAdded     bool
	HeadRewritten bool
}

type LaterTurns struct {
	LaterArm
	Bodies      [3][]byte
	CacheRead   [2]int
	RequestSize [2]int
	Carries     [3]int
}

func (l LaterTurns) Share(turn int) float64 {
	return float64(l.CacheRead[turn-2]) / float64(l.RequestSize[turn-2])
}

func (l LaterTurns) SystemHeld() bool {
	return systemOf(l.Bodies[0]) == systemOf(l.Bodies[1]) && systemOf(l.Bodies[1]) == systemOf(l.Bodies[2])
}

func MeasureLaterTurns(arm LaterArm) (LaterTurns, error) {
	measured := LaterTurns{LaterArm: arm}
	var bodies [][]byte
	var mu sync.Mutex
	listening := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, body)
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range []string{
			`{"type":"message_start","message":{"id":"msg_1","model":"claude-test","usage":{"input_tokens":3}}}`,
			`{"type":"content_block_start","index":0,"content_block":{"type":"text"}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"done"}}`,
			`{"type":"content_block_stop","index":0}`,
			`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`,
			`{"type":"message_stop"}`,
		} {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", event)
		}
	}))
	defer listening.Close()
	wire, err := anthropic.New(anthropic.Config{BaseURL: listening.URL, Model: "claude-test", Proxy: true,
		Token: func(context.Context, string) (string, error) { return "sk-ant-oat01-bench", nil }})
	if err != nil {
		return measured, err
	}
	scratch, err := os.MkdirTemp("", "bench-prefix-")
	if err != nil {
		return measured, err
	}
	defer func() { _ = os.RemoveAll(scratch) }()
	if err := os.WriteFile(filepath.Join(scratch, AddedRuleID+"@1.yaml"), []byte(addedRule), 0o600); err != nil {
		return measured, err
	}
	shipped, err := RealRules()
	if err != nil {
		return measured, err
	}
	added, err := rule.LoadDir(scratch)
	if err != nil {
		return measured, err
	}
	config := turn.Config{Model: turn.Subscription{Wire: wire}, Spend: turn.SpendSubscription,
		Caps: turn.Caps{MaxSteps: 2}, ResultBytesCap: 1 << 16, ArtifactDir: filepath.Join(scratch, "artifacts"), NewID: func() string { return "turn-bench" }}
	if !arm.HeadRewritten {
		config.Prefix = &turn.Prefix{}
	}
	laterTasks := []string{RealTask, "now run the test it fixed and say what it printed", "summarise what changed"}
	for at, task := range laterTasks {
		rules := shipped
		if arm.RuleAdded && at > 0 {
			rules = append(rules[:len(rules):len(rules)], added...)
		}
		composed, err := turn.Compose(turn.ComposeSpec{Task: task, Environment: laterEnvironment, ToolGuidance: RealToolGuidance, Rules: rules})
		if err != nil {
			return measured, err
		}
		config.Task, config.System, config.Parts, config.Environment = task, composed.Head(), composed.Parts, composed.WithTaskRules(laterEnvironment)
		row, err := turn.Run(context.Background(), config)
		if err != nil {
			return measured, err
		}
		config.History = turn.Sendable(row.Conversation)
	}
	if len(bodies) != len(laterTasks) {
		return measured, fmt.Errorf("three turns sent %d requests, want one each", len(bodies))
	}
	for at, body := range bodies {
		measured.Bodies[at], measured.Carries[at] = body, strings.Count(string(body), "from the rule "+AddedRuleID)
		if at > 0 {
			measured.CacheRead[at-1], measured.RequestSize[at-1] = cacheReadBytes(bodies[at-1], body)
		}
	}
	return measured, nil
}

type cachedPiece struct {
	text       string
	breakpoint bool
}

func withoutBillingCheck(text string) string {
	return regexp.MustCompile(`cch=[0-9a-f]+`).ReplaceAllString(text, anthropic.BillingCheckPlaceholder)
}

func inCacheOrder(body []byte) []cachedPiece {
	var decoded struct {
		Tools    []map[string]any `json:"tools"`
		System   []map[string]any `json:"system"`
		Messages []struct {
			Content []map[string]any `json:"content"`
		} `json:"messages"`
	}
	_ = json.Unmarshal(body, &decoded)
	blocks := slices.Concat(decoded.Tools, decoded.System)
	for _, message := range decoded.Messages {
		blocks = append(blocks, message.Content...)
	}
	pieces := make([]cachedPiece, len(blocks))
	for at, block := range blocks {
		_, marked := block["cache_control"]
		delete(block, "cache_control")
		text, _ := json.Marshal(block)
		pieces[at] = cachedPiece{text: withoutBillingCheck(string(text)), breakpoint: marked}
	}
	return pieces
}

func cacheReadBytes(earlier, later []byte) (read, size int) {
	asked := inCacheOrder(later)
	for _, piece := range asked {
		size += len(piece.text)
	}
	matched := 0
	for at, piece := range inCacheOrder(earlier) {
		if at >= len(asked) || asked[at].text != piece.text {
			break
		}
		matched += len(piece.text)
		if piece.breakpoint {
			read = matched
		}
	}
	return read, size
}

func systemOf(body []byte) string {
	var decoded struct {
		System json.RawMessage `json:"system"`
	}
	_ = json.Unmarshal(body, &decoded)
	return withoutBillingCheck(string(decoded.System))
}
