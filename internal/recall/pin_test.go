package recall_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"tofu/internal/recall"
	"tofu/internal/session"
)

const (
	pinnedCorpusPath   = "testdata/estimate-corpus.jsonl"
	pinnedCorpusFloor  = 500
	liveSessionsDir    = "../../.tofu/sessions"
	pinnedCorpusWriter = "TOFU_PIN_ESTIMATE_CORPUS"
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

const wireCountingCacheReadsInsidePromptTokens = "codex"

func (s recordedStep) reported(wire string) recall.Bill {
	fresh := s.PromptTokens + s.CacheWriteTokens
	if wire == wireCountingCacheReadsInsidePromptTokens {
		fresh -= s.CacheReadTokens
	}
	return recall.Bill{CacheRead: s.CacheReadTokens, Fresh: fresh}
}

type recordedToolCall struct {
	Arguments json.RawMessage `json:"arguments"`
}

type recordedMessage struct {
	Role      string             `json:"role"`
	Content   string             `json:"content"`
	ToolCalls []recordedToolCall `json:"tool_calls"`
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

func lastAssistant(messages []recordedMessage) int {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" {
			return i
		}
	}
	return -1
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
			reported := step.reported(header.Wire)
			if step.Occupancy != nil && reported.Total() > 0 {
				requests = append(requests, recordedRequest{
					Session:      header.ID,
					Day:          header.At.Format(time.DateOnly),
					Step:         step.Index,
					Wire:         header.Wire,
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

type pinnedRow struct {
	Session          string `json:"session"`
	Day              string `json:"day"`
	Wire             string `json:"wire"`
	Step             int    `json:"step"`
	Rebuilt          string `json:"rebuilt"`
	InstructionBytes int    `json:"instruction_bytes"`
	EntryBytes       []int  `json:"entry_bytes"`
	BilledCacheRead  int    `json:"billed_cache_read"`
	BilledFresh      int    `json:"billed_fresh"`
	CachedBefore     int    `json:"cached_before"`
	RecordedEstimate int    `json:"recorded_estimate"`
}

func (p pinnedRow) request() recordedRequest {
	entries := make([]recall.Entry, 0, len(p.EntryBytes))
	for _, bytes := range p.EntryBytes {
		entries = append(entries, recall.Entry{Text: filler(bytes)})
	}
	return recordedRequest{
		Session:      p.Session,
		Day:          p.Day,
		Step:         p.Step,
		Wire:         p.Wire,
		Rebuilt:      p.Rebuilt,
		Conversation: recall.Conversation{Instructions: filler(p.InstructionBytes), Entries: entries},
		Reported:     recall.Bill{CacheRead: p.BilledCacheRead, Fresh: p.BilledFresh},
		CachedBefore: p.CachedBefore,
		Recorded:     p.RecordedEstimate,
	}
}

func pinnedOf(request recordedRequest) pinnedRow {
	entryBytes := make([]int, 0, len(request.Conversation.Entries))
	for _, entry := range request.Conversation.Entries {
		entryBytes = append(entryBytes, len(entry.Text))
	}
	return pinnedRow{
		Session:          request.Session,
		Day:              request.Day,
		Wire:             request.Wire,
		Step:             request.Step,
		Rebuilt:          request.Rebuilt,
		InstructionBytes: len(request.Conversation.Instructions),
		EntryBytes:       entryBytes,
		BilledCacheRead:  request.Reported.CacheRead,
		BilledFresh:      request.Reported.Fresh,
		CachedBefore:     request.CachedBefore,
		RecordedEstimate: request.Recorded,
	}
}

func recordedRequests(t *testing.T) (recall.Config, []recordedRequest) {
	t.Helper()
	cfg := shippedConfig(t)
	pinned, err := os.ReadFile(pinnedCorpusPath)
	if err != nil {
		t.Fatalf("the pinned corpus at %s does not open, and the bound is measured on nothing without it: %v", pinnedCorpusPath, err)
	}
	var requests []recordedRequest
	for number, line := range strings.Split(string(pinned), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var row pinnedRow
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatalf("%s line %d is not a pinned row: %v", pinnedCorpusPath, number+1, err)
		}
		requests = append(requests, row.request())
	}
	if len(requests) < pinnedCorpusFloor {
		t.Fatalf("the pinned corpus holds %d requests and the bound was measured on at least %d: a corpus this small is not the one the bound means", len(requests), pinnedCorpusFloor)
	}
	return cfg, requests
}

func liveRequests(t *testing.T) ([]recordedRequest, int) {
	t.Helper()
	cfg := shippedConfig(t)
	store := session.NewStore(filepath.FromSlash(liveSessionsDir))
	listing, err := store.Listing()
	if err != nil {
		t.Skipf("the live sessions are not readable from here, so the pin cannot be checked against them: %v", err)
	}
	var requests []recordedRequest
	unreadable := 0
	for _, header := range listing.Sessions {
		events, err := store.Body(header.ID)
		if err != nil {
			unreadable++
			continue
		}
		requests = append(requests, sessionRequests(t, cfg, header, events)...)
	}
	return requests, unreadable
}

func TestThePinnedCorpusStillMatchesTheLiveSessionsItWasTakenFrom(t *testing.T) {
	live, unreadable := liveRequests(t)
	if path := os.Getenv(pinnedCorpusWriter); path != "" {
		writePin(t, path, live)
	}
	_, pinned := recordedRequests(t)

	liveByKey, seenLive := map[string]pinnedRow{}, map[string]int{}
	for _, request := range live {
		seenLive[request.Session]++
		liveByKey[request.Session+"#"+strconv.Itoa(seenLive[request.Session])] = pinnedOf(request)
	}
	gone, seen, held := 0, map[string]int{}, map[string]bool{}
	for _, request := range pinned {
		want := pinnedOf(request)
		seen[want.Session]++
		held[want.Session] = true
		got, ok := liveByKey[want.Session+"#"+strconv.Itoa(seen[want.Session])]
		if !ok {
			gone++
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s step %d reads differently from the live store than the pin records: live %+v, pinned %+v", want.Session, want.Step, got, want)
		}
	}
	fresh := 0
	for _, request := range live {
		if !held[request.Session] {
			fresh++
		}
	}
	t.Logf("%d live requests against %d pinned; %d pinned rows are no longer in the live store; %d live requests come from sessions recorded since the pin and are not measured by the bound; %d live sessions unreadable",
		len(live), len(pinned), gone, fresh, unreadable)
}

func writePin(t *testing.T, path string, requests []recordedRequest) {
	t.Helper()
	var out strings.Builder
	for _, request := range requests {
		line, err := json.Marshal(pinnedOf(request))
		if err != nil {
			t.Fatalf("a rebuilt request does not marshal: %v", err)
		}
		out.Write(line)
		out.WriteString("\n")
	}
	if err := os.WriteFile(path, []byte(out.String()), 0o644); err != nil {
		t.Fatalf("writing the pin to %s: %v", path, err)
	}
	t.Logf("wrote %d rows, %d bytes, to %s", len(requests), out.Len(), path)
}
