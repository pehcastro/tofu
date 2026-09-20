package recall_test

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"tofu/internal/recall"
	"tofu/internal/session"
)

const (
	compactionDecidesAboveTokens = 5000
	marginPercent                = 15
	worstRowsListed              = 10
)

type recordedCall struct {
	Args          json.RawMessage `json:"args"`
	RenderedBytes int             `json:"rendered_bytes"`
}

type recordedStep struct {
	Index            int               `json:"index"`
	AssistantText    string            `json:"assistant_text"`
	ToolCalls        []recordedCall    `json:"tool_calls"`
	PromptTokens     int               `json:"prompt_tokens"`
	CacheReadTokens  int               `json:"cache_read_tokens"`
	CacheWriteTokens int               `json:"cache_write_tokens"`
	Occupancy        *recall.Occupancy `json:"occupancy"`
}

func (s recordedStep) reported() recall.Bill {
	return recall.Bill{CacheRead: s.CacheReadTokens, Fresh: s.PromptTokens + s.CacheWriteTokens}
}

type recordedToolCall struct {
	Arguments json.RawMessage `json:"arguments"`
}

type recordedMessage struct {
	Role      string             `json:"role"`
	Content   string             `json:"content"`
	ToolCalls []recordedToolCall `json:"tool_calls"`
}

type recordedRequest struct {
	Session      string
	Step         int
	Rebuilt      string
	Conversation recall.Conversation
	Reported     recall.Bill
	CachedBefore int
	Recorded     int
}

func filler(bytes int) string {
	if bytes <= 0 {
		return ""
	}
	return strings.Repeat("x", bytes)
}

func entriesOfMessages(messages []recordedMessage) []recall.Entry {
	entries := make([]recall.Entry, 0, len(messages))
	for _, message := range messages {
		text := message.Content
		for _, call := range message.ToolCalls {
			text += string(call.Arguments)
		}
		entries = append(entries, recall.Entry{Text: text})
	}
	return entries
}

func sessionRequests(t *testing.T, cfg recall.Config, header session.Header, events []session.Event) []recordedRequest {
	t.Helper()
	var requests []recordedRequest
	var messages []recordedMessage
	replayed := []recall.Entry{{Text: header.Task}}
	cached := 0
	for _, event := range events {
		switch event.Kind {
		case session.EventOutcome:
			messages, replayed = nil, []recall.Entry{{Text: header.Task}}
		case session.EventMessage:
			var message recordedMessage
			if err := json.Unmarshal(event.Body, &message); err != nil {
				t.Fatalf("%s: a recorded message does not parse: %v", header.ID, err)
			}
			messages = append(messages, message)
		case session.EventStep:
			var step recordedStep
			if err := json.Unmarshal(event.Body, &step); err != nil {
				t.Fatalf("%s: a recorded step does not parse: %v", header.ID, err)
			}
			sent, rebuilt := replayed, "step rows"
			if last := lastAssistant(messages); last >= 0 {
				sent, rebuilt = entriesOfMessages(messages[:last]), "messages"
			}
			reported := step.reported()
			if step.Occupancy != nil && reported.Total() > 0 {
				requests = append(requests, recordedRequest{
					Session:      header.ID,
					Step:         step.Index,
					Rebuilt:      rebuilt,
					Conversation: recall.Conversation{Entries: sent},
					Reported:     reported,
					CachedBefore: cached,
					Recorded:     step.Occupancy.Total(),
				})
			}
			if reported.Total() > 0 {
				cached = step.CacheReadTokens + step.CacheWriteTokens
			}
			answered := recall.Entry{Text: step.AssistantText}
			for _, call := range step.ToolCalls {
				answered.Text += string(call.Args)
			}
			replayed = append(replayed, answered)
			for _, call := range step.ToolCalls {
				replayed = append(replayed, recall.Entry{Text: filler(call.RenderedBytes)})
			}
		}
	}
	return withPrefix(cfg, requests)
}

func lastAssistant(messages []recordedMessage) int {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" {
			return i
		}
	}
	return -1
}

func withPrefix(cfg recall.Config, requests []recordedRequest) []recordedRequest {
	if len(requests) == 0 {
		return nil
	}
	bands := recall.ShippedBands()
	prefix := requests[0].Reported.Total() - recall.Measure(cfg, bands, requests[0].Conversation).Total()
	for i := range requests {
		requests[i].Conversation.Instructions = filler(prefix * cfg.BytesPerThousandTokens / 1000)
	}
	return requests
}

func recordedRequests(t *testing.T) (recall.Config, []recordedRequest) {
	t.Helper()
	cfg := shippedConfig(t)
	store := session.NewStore(filepath.Join("..", "..", ".tofu", "sessions"))
	listing, err := store.Listing()
	if err != nil {
		t.Skipf("the recorded sessions are not readable from here: %v", err)
	}
	var requests []recordedRequest
	for _, header := range listing.Sessions {
		events, err := store.Body(header.ID)
		if err != nil {
			continue
		}
		requests = append(requests, sessionRequests(t, cfg, header, events)...)
	}
	if len(requests) < 3 {
		t.Skipf("%d recorded steps carry both an occupancy and a billed count, and three are needed", len(requests))
	}
	return cfg, requests
}

func offBy(estimate, billed int) int {
	return (estimate - billed) * 100 / billed
}

func TestOurEstimateOfAContextAgainstWhatTheProviderBilledForTheSameOne(t *testing.T) {
	cfg, requests := recordedRequests(t)
	bands := recall.ShippedBands()

	type row struct {
		request  recordedRequest
		estimate int
		off      int
		was      int
	}
	rows := make([]row, 0, len(requests))
	low, fromMessages := 0, 0
	for _, request := range requests {
		estimate := recall.Measure(cfg, bands, request.Conversation).Total()
		billed := request.Reported.Total()
		if request.Rebuilt == "messages" {
			fromMessages++
		}
		if estimate < billed {
			low++
		}
		rows = append(rows, row{request, estimate, offBy(estimate, billed), offBy(request.Recorded, billed)})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].off < rows[j].off })

	listed := min(worstRowsListed, len(rows))
	for _, r := range append(append([]row{}, rows[:listed]...), rows[len(rows)-listed:]...) {
		t.Logf("%s step %2d, rebuilt from %-9s: we estimate %6d, the provider billed %6d, %4d percent out, where the build recorded %6d, %4d percent out",
			r.request.Session[len(r.request.Session)-8:], r.request.Step, r.request.Rebuilt,
			r.estimate, r.request.Reported.Total(), r.off, r.request.Recorded, r.was)
	}

	worst, worstAt, wasWorst, wasWorstAt := 0, 0, 0, 0
	for i, r := range rows {
		if r.request.Reported.Total() < compactionDecidesAboveTokens {
			continue
		}
		if abs(r.off) > worst {
			worst, worstAt = abs(r.off), i
		}
		if abs(r.was) > wasWorst {
			wasWorst, wasWorstAt = abs(r.was), i
		}
	}
	t.Logf("%d recorded requests, %d rebuilt from the messages the session kept and %d from the step rows; we read low on %d of them; above %d billed tokens the worst is %d percent, %d against %d, where the estimator that recorded these sessions was %d percent out at worst, %d against %d",
		len(rows), fromMessages, len(rows)-fromMessages, low, compactionDecidesAboveTokens, worst,
		rows[worstAt].estimate, rows[worstAt].request.Reported.Total(),
		wasWorst, rows[wasWorstAt].request.Recorded, rows[wasWorstAt].request.Reported.Total())

	if worst > marginPercent {
		t.Fatalf("on a request of %d billed tokens we estimate %d, %d percent out, past the %d percent this bound allows: every threshold in context-budget.md is written in our units, so this is the size of the error in all of them",
			rows[worstAt].request.Reported.Total(), rows[worstAt].estimate, worst, marginPercent)
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func TestTheEstimateCountsTheToolSchemasEveryRequestCarriesAndNotOnlyTheInstructions(t *testing.T) {
	cfg := recall.Config{BytesPerThousandTokens: 1000}
	bands := recall.ShippedBands()
	instructions := recall.Conversation{Instructions: filler(4000)}
	both := recall.Conversation{Instructions: filler(4000), ToolSchemas: filler(9000)}

	if measured := recall.Measure(cfg, bands, instructions).Identity; measured != 4000 {
		t.Fatalf("identity = %d tokens for a 4000 byte instruction block at a byte a token, want 4000", measured)
	}
	measured := recall.Measure(cfg, bands, both).Identity
	if measured != 13000 {
		t.Fatalf("identity = %d tokens for the same instructions plus a 9000 byte tool schema block, want 13000: the schemas are on every request and the provider bills them", measured)
	}
}

func TestWhatTheProviderReadsFromItsCacheAndWhatItChargesFresh(t *testing.T) {
	_, requests := recordedRequests(t)
	compared, reset := 0, 0
	for _, request := range requests {
		billed := request.Reported.Total()
		if request.CachedBefore == 0 || request.CachedBefore > billed {
			reset++
			continue
		}
		split := recall.Billed(request.CachedBefore, billed)
		compared++
		if split != request.Reported {
			t.Fatalf("%s step %d: from a %d token cached prefix and a %d token request we predict %d read and %d fresh, and the provider reported %d read and %d fresh",
				request.Session, request.Step, request.CachedBefore, billed,
				split.CacheRead, split.Fresh, request.Reported.CacheRead, request.Reported.Fresh)
		}
	}
	if compared < len(requests)/2 {
		t.Fatalf("only %d of %d recorded requests could be compared against the cache split", compared, len(requests))
	}
	t.Logf("the split holds on %d recorded requests; %d are skipped because the request was smaller than the cached prefix, which is a new turn rather than a step", compared, reset)
}
