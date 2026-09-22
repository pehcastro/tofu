package anthropic

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"tofu/internal/konst"
	"tofu/internal/llm"
)

const (
	documentedLookbackBlocks = 20
	exactLookbackBlocks      = 0
	charsPerToken            = 4
	replayBudget             = 2
)

type recordedEvent struct {
	Kind string          `json:"kind"`
	Body json.RawMessage `json:"body"`
}

type recordedCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type recordedMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	ToolCallID string         `json:"tool_call_id"`
	ToolCalls  []recordedCall `json:"tool_calls"`
}

type recordedHeader struct {
	Wire string `json:"wire"`
}

func sessionsRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		candidate := filepath.Join(dir, ".tofu", "sessions")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Skip("no .tofu/sessions above the package directory: the recorded corpus is not on this machine")
		}
		dir = parent
	}
}

func roleOf(recorded string) llm.Role {
	switch recorded {
	case "user":
		return llm.RoleUser
	case "assistant":
		return llm.RoleAssistant
	case "tool":
		return llm.RoleTool
	case "system":
		return llm.RoleSystem
	}
	return llm.RoleUnknown
}

func requestsIn(dir string) ([][]llm.Message, error) {
	body, err := os.ReadFile(filepath.Join(dir, "body.jsonl"))
	if err != nil {
		return nil, err
	}
	var history []llm.Message
	var requests [][]llm.Message
	for number, line := range strings.Split(string(body), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event recordedEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return nil, fmt.Errorf("line %d is not an event: %w", number+1, err)
		}
		if event.Kind != "message" {
			continue
		}
		var recorded recordedMessage
		if err := json.Unmarshal(event.Body, &recorded); err != nil {
			return nil, fmt.Errorf("line %d is not a message: %w", number+1, err)
		}
		role := roleOf(recorded.Role)
		if role == llm.RoleSystem || role == llm.RoleUnknown {
			continue
		}
		message := llm.Message{Role: role, Content: recorded.Content, ToolCallID: recorded.ToolCallID}
		for _, call := range recorded.ToolCalls {
			message.ToolCalls = append(message.ToolCalls,
				llm.ToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments})
		}
		if role == llm.RoleAssistant {
			requests = append(requests, append([]llm.Message(nil), history...))
		}
		history = append(history, message)
	}
	return requests, nil
}

func legacyHistoryCaching(messages []wireMessage, ttl string, budget int) {
	if budget < 1 || len(messages) < historyCacheMinMessages || historyChars(messages) < historyCacheMinPrefixChars {
		return
	}
	last := len(messages) - 1
	markPrefixEnd(messages[last].Content, ttl)
	if budget < 2 {
		return
	}
	for index := 1; index < last; index++ {
		if messages[index].Role == "user" {
			markPrefixEnd(messages[index].Content, ttl)
			return
		}
	}
}

type blockPrefix struct {
	hash  uint64
	chars int
}

func prefixWalk(messages []wireMessage) ([]blockPrefix, []int) {
	running := fnv.New64a()
	var prefixes []blockPrefix
	var marked []int
	chars := 0
	for _, message := range messages {
		for _, block := range message.Content {
			control := block.CacheControl
			block.CacheControl = nil
			encoded, err := json.Marshal(block)
			if err != nil {
				panic(err)
			}
			running.Write([]byte(message.Role))
			running.Write(encoded)
			chars += len(encoded)
			if control != nil {
				marked = append(marked, len(prefixes))
			}
			prefixes = append(prefixes, blockPrefix{hash: running.Sum64(), chars: chars})
		}
	}
	return prefixes, marked
}

func encodeAll(requests [][]llm.Message) ([][]wireMessage, error) {
	encoded := make([][]wireMessage, 0, len(requests))
	for _, history := range requests {
		messages, err := encodeMessages(history, true)
		if err != nil {
			return nil, err
		}
		encoded = append(encoded, messages)
	}
	return encoded, nil
}

func cachedCharsOver(requests [][]wireMessage, place func([]wireMessage, string, int), lookback int) (int, int) {
	cached := make(map[uint64]bool)
	read, widestTurn, previousBlocks := 0, 0, 0
	for _, messages := range requests {
		if len(messages) == 0 {
			continue
		}
		for _, message := range messages {
			for index := range message.Content {
				message.Content[index].CacheControl = nil
			}
		}
		place(messages, konst.SubscriptionCacheTTL, replayBudget)
		prefixes, marked := prefixWalk(messages)
		if grown := len(prefixes) - previousBlocks; grown > widestTurn {
			widestTurn = grown
		}
		previousBlocks = len(prefixes)
		best := 0
		for _, mark := range marked {
			for index := max(0, mark-lookback); index <= mark; index++ {
				if cached[prefixes[index].hash] && prefixes[index].chars > best {
					best = prefixes[index].chars
				}
			}
		}
		read += best
		for _, mark := range marked {
			cached[prefixes[mark].hash] = true
		}
	}
	return read, widestTurn
}

func percent(change, base int) float64 {
	if base == 0 {
		return 0
	}
	return 100 * float64(change) / float64(base)
}

type sessionResult struct {
	id          string
	requests    int
	widestTurn  int
	legacy      int
	moved       int
	legacyExact int
	movedExact  int
}

func TestTheMovedAnchorCachesMoreOfTheRecordedCorpus(t *testing.T) {
	root := sessionsRoot(t)
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}

	skips := make(map[string][]string)
	var results []sessionResult
	for _, entry := range entries {
		if !entry.IsDir() {
			if name, single := strings.CutSuffix(entry.Name(), ".json"); single {
				skips["the older single file format, which has no wire field"] =
					append(skips["the older single file format, which has no wire field"], name)
			}
			continue
		}
		dir := filepath.Join(root, entry.Name())
		raw, err := os.ReadFile(filepath.Join(dir, "header.json"))
		if err != nil {
			skips["no header.json"] = append(skips["no header.json"], entry.Name())
			continue
		}
		var header recordedHeader
		if err := json.Unmarshal(raw, &header); err != nil {
			skips["the header does not parse"] = append(skips["the header does not parse"], entry.Name())
			continue
		}
		switch header.Wire {
		case "":
			skips["no wire field on the header"] = append(skips["no wire field on the header"], entry.Name())
			continue
		case "anthropic":
		default:
			skips["another wire"] = append(skips["another wire"], entry.Name())
			continue
		}
		requests, err := requestsIn(dir)
		if err != nil {
			skips["the body does not replay"] = append(skips["the body does not replay"], entry.Name())
			continue
		}
		if len(requests) < 2 {
			skips["fewer than two requests"] = append(skips["fewer than two requests"], entry.Name())
			continue
		}
		encoded, err := encodeAll(requests)
		if err != nil {
			skips["a recorded message does not encode"] = append(skips["a recorded message does not encode"], entry.Name())
			continue
		}
		measured := sessionResult{id: entry.Name(), requests: len(requests)}
		for _, arm := range []struct {
			into     *int
			place    func([]wireMessage, string, int)
			lookback int
		}{
			{&measured.legacy, legacyHistoryCaching, documentedLookbackBlocks},
			{&measured.moved, applyHistoryCaching, documentedLookbackBlocks},
			{&measured.legacyExact, legacyHistoryCaching, exactLookbackBlocks},
			{&measured.movedExact, applyHistoryCaching, exactLookbackBlocks},
		} {
			*arm.into, measured.widestTurn = cachedCharsOver(encoded, arm.place, arm.lookback)
		}
		results = append(results, measured)
	}

	if len(results) == 0 {
		t.Skip("no recorded multi-turn anthropic session survived the skips")
	}
	sort.Slice(results, func(a, b int) bool { return results[a].id < results[b].id })

	var report strings.Builder
	var totals sessionResult
	worst, best := 0.0, 0.0
	fmt.Fprintf(&report, "%-30s %9s %12s %13s %11s %9s %13s %11s\n",
		"session", "requests", "widest turn", "current read", "moved read", "change", "current exact", "moved exact")
	for index, result := range results {
		share := percent(result.moved-result.legacy, result.legacy)
		if index == 0 || share < worst {
			worst = share
		}
		if index == 0 || share > best {
			best = share
		}
		fmt.Fprintf(&report, "%-30s %9d %12d %13d %11d %8.1f%% %13d %11d\n",
			result.id, result.requests, result.widestTurn,
			result.legacy/charsPerToken, result.moved/charsPerToken, share,
			result.legacyExact/charsPerToken, result.movedExact/charsPerToken)
		totals.requests += result.requests
		totals.legacy += result.legacy
		totals.moved += result.moved
		totals.legacyExact += result.legacyExact
		totals.movedExact += result.movedExact
		if result.moved < result.legacy {
			t.Errorf("%s caches %d tokens under the moved anchor against %d under the current one",
				result.id, result.moved/charsPerToken, result.legacy/charsPerToken)
		}
	}
	fmt.Fprintf(&report,
		"\n%d sessions, %d requests, %d chars per token\n"+
			"at the documented %d block lookback: current %d tokens, moved %d tokens, %+d tokens, %+.2f%%\n"+
			"at an exact breakpoint match: current %d tokens, moved %d tokens, %+d tokens, %+.2f%%\n"+
			"per session change at the documented lookback, from %+.1f%% to %+.1f%%\n",
		len(results), totals.requests, charsPerToken,
		documentedLookbackBlocks, totals.legacy/charsPerToken, totals.moved/charsPerToken,
		(totals.moved-totals.legacy)/charsPerToken, percent(totals.moved-totals.legacy, totals.legacy),
		totals.legacyExact/charsPerToken, totals.movedExact/charsPerToken,
		(totals.movedExact-totals.legacyExact)/charsPerToken, percent(totals.movedExact-totals.legacyExact, totals.legacyExact),
		worst, best)
	for reason, ids := range skips {
		fmt.Fprintf(&report, "skipped, %s: %d (%s)\n", reason, len(ids), strings.Join(ids, " "))
	}
	t.Log("\n" + report.String())
}
