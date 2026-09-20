package recall

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/internal/konst"
	rc "tofu/internal/recall"
	"tofu/internal/sys"
)

type SessionCall struct {
	Tool          string          `json:"tool"`
	Args          json.RawMessage `json:"args,omitempty"`
	Command       string          `json:"command,omitempty"`
	RenderedBytes int             `json:"rendered_bytes"`
}

type SessionStep struct {
	Index           int           `json:"index"`
	AssistantText   string        `json:"assistant_text,omitempty"`
	PromptTokens    int           `json:"prompt_tokens"`
	CacheReadTokens int           `json:"cache_read_tokens"`
	ToolCalls       []SessionCall `json:"tool_calls,omitempty"`
}

type Session struct {
	ID    string        `json:"id"`
	Task  string        `json:"task"`
	Steps []SessionStep `json:"steps,omitempty"`
}

func ReadSession(path string) (Session, error) {
	data, err := sys.ReadFile(path)
	if err != nil {
		return Session{}, err
	}
	var session Session
	if err := json.Unmarshal(data, &session); err != nil {
		return Session{}, fmt.Errorf("recall: %s is not a recorded turn: %w", path, err)
	}
	if session.Task == "" || len(session.Steps) == 0 {
		return Session{}, fmt.Errorf("recall: %s carries no task and no steps", path)
	}
	return session, nil
}

type ReplayStep struct {
	Index           int
	InputTokens     int
	FactsTokens     int
	RecordedTokens  int
	CacheReadTokens int
	FreshTokens     int
	Drops           []rc.Drop
}

type ForkRow struct {
	Step          int
	Carry         rc.Carry
	TokensBefore  int
	TokensAfter   int
	CarryTokens   int
	BlockedMicros int64
	SourcesKnown  int
	SourcesNamed  int
}

type Refetch struct {
	Step   int
	Source string
	Bytes  int
	Named  bool
}

type callArgs struct {
	Path    string `json:"path"`
	Handle  string `json:"handle"`
	Pattern string `json:"pattern"`
	Command string `json:"command"`
}

func callSource(call SessionCall) string {
	var args callArgs
	if err := json.Unmarshal(call.Args, &args); err != nil {
		return call.Tool
	}
	return cmp.Or(args.Path, args.Handle, args.Pattern, args.Command, call.Tool)
}

type CarryBuilder func(*rc.Store, rc.Config, rc.Conversation) (rc.Carry, error)

type Arm struct {
	Name     string
	Rewrite  bool
	Carry    CarryBuilder
	Signpost int
}

type ReplayResult struct {
	Steps     []ReplayStep
	Drops     []rc.Drop
	Forks     []ForkRow
	Refetches []Refetch
	Peak      rc.Occupancy
	Final     rc.Occupancy
	Carried   rc.Conversation
}

func (r ReplayResult) RefetchedBytes() int {
	total := 0
	for _, refetch := range r.Refetches {
		total += refetch.Bytes
	}
	return total
}

func (r ReplayResult) BlindRefetches() int {
	blind := 0
	for _, refetch := range r.Refetches {
		if !refetch.Named {
			blind++
		}
	}
	return blind
}

func (r ReplayResult) InputTokens() int {
	total := 0
	for _, step := range r.Steps {
		total += step.InputTokens
	}
	return total
}

func (r ReplayResult) CacheReadTokens() int {
	total := 0
	for _, step := range r.Steps {
		total += step.CacheReadTokens
	}
	return total
}

func (r ReplayResult) FreshTokens() int {
	total := 0
	for _, step := range r.Steps {
		total += step.FreshTokens
	}
	return total
}

func cachedPrefixTokens(cfg rc.Config, previous, current rc.Conversation) int {
	if previous.Instructions != current.Instructions {
		return 0
	}
	tokens := cfg.Tokens(current.Instructions)
	for i, entry := range current.Entries {
		if i >= len(previous.Entries) || previous.Entries[i].Text != entry.Text {
			break
		}
		tokens += cfg.Tokens(entry.Text)
	}
	return tokens
}

func ReplaySession(store *rc.Store, cfg rc.Config, bands rc.Bands, session Session, arm Arm) (ReplayResult, error) {
	if len(session.Steps) == 0 {
		return ReplayResult{}, errors.New("recall: a session with no steps has nothing to replay")
	}
	signpostBytes := cmp.Or(arm.Signpost, konst.FactSignpostBytes)
	unrecordedPrefixBytes := session.Steps[0].CacheReadTokens * cfg.BytesPerThousandTokens / 1000
	instructions := session.Task + strings.Repeat(".", unrecordedPrefixBytes)
	conversation := rc.Conversation{Instructions: instructions}

	var result ReplayResult
	var sent rc.Conversation
	seen := make(map[string]bool)
	carried := make(map[string]bool)
	var newSessionText string
	for _, step := range session.Steps {
		occupancy := rc.Measure(cfg, bands, conversation)
		if occupancy.Total() > result.Peak.Total() {
			result.Peak = occupancy
		}
		cached := cachedPrefixTokens(cfg, sent, conversation)
		result.Steps = append(result.Steps, ReplayStep{
			Index:           step.Index,
			InputTokens:     occupancy.Total(),
			FactsTokens:     occupancy.Facts,
			RecordedTokens:  step.PromptTokens + step.CacheReadTokens,
			CacheReadTokens: cached,
			FreshTokens:     occupancy.Total() - cached,
		})
		sent = rc.Conversation{Instructions: conversation.Instructions, Entries: slices.Clone(conversation.Entries)}
		assistant := step.AssistantText
		for _, call := range step.ToolCalls {
			assistant += "\n" + call.Tool + " " + string(call.Args)
		}
		if assistant != "" {
			conversation.Entries = append(conversation.Entries, rc.Entry{Step: step.Index, Text: assistant})
		}
		for _, call := range step.ToolCalls {
			source := callSource(call)
			if carried[source] {
				result.Refetches = append(result.Refetches, Refetch{
					Step:   step.Index,
					Source: source,
					Bytes:  call.RenderedBytes,
					Named:  strings.Contains(newSessionText, source),
				})
			}
			seen[source] = true
			conversation.Entries = append(conversation.Entries, rc.Entry{
				Step:         step.Index,
				Tool:         call.Tool,
				SupersedeKey: call.Tool + " " + string(call.Args),
				Text:         recordedBody(call),
			})
		}
		sheet, _, err := rc.Distil(store, conversation, signpostBytes)
		if err != nil {
			return ReplayResult{}, err
		}
		conversation.Facts = sheet
		if arm.Carry != nil && rc.Crossed(cfg, bands, conversation) {
			started := time.Now()
			carry, err := arm.Carry(store, cfg, conversation)
			if err != nil {
				return ReplayResult{}, err
			}
			before := rc.Measure(cfg, bands, conversation).Total()
			conversation = rc.Conversation{
				Instructions: instructions,
				Facts:        sheet,
				Entries: []rc.Entry{
					{Step: step.Index, Text: session.Task},
					{Step: step.Index, Text: aboveTheSheet(carry.Text, sheet)},
				},
			}
			carried = maps.Clone(seen)
			newSessionText = strings.Join(append(slices.Clone(sheet), carry.Text), "\n")
			named := 0
			for source := range carried {
				if strings.Contains(newSessionText, source) {
					named++
				}
			}
			result.Forks = append(result.Forks, ForkRow{
				Step:          step.Index,
				Carry:         carry,
				TokensBefore:  before,
				TokensAfter:   rc.Measure(cfg, bands, conversation).Total(),
				CarryTokens:   cfg.Tokens(carry.Text),
				BlockedMicros: time.Since(started).Microseconds(),
				SourcesKnown:  len(carried),
				SourcesNamed:  named,
			})
			continue
		}
		if !arm.Rewrite {
			continue
		}
		compacted, drops, err := rc.Compact(store, cfg, bands, conversation)
		if err != nil {
			return ReplayResult{}, err
		}
		conversation = compacted
		result.Drops = append(result.Drops, drops...)
		result.Steps[len(result.Steps)-1].Drops = drops
	}
	result.Final, result.Carried = rc.Measure(cfg, bands, conversation), conversation
	return result, nil
}

const CacheReadPricePerHundred = 10

func (r ReplayResult) BilledUnits() int {
	return r.FreshTokens() + r.CacheReadTokens()*CacheReadPricePerHundred/100
}

type Measured struct {
	Arm    string
	Result ReplayResult
}

func ArmsTable(target int, measured []Measured) string {
	var report strings.Builder
	fmt.Fprintf(&report, "target %d tokens, %d steps\n", target, len(measured[0].Result.Steps))
	fmt.Fprintf(&report, "%-18s %15s %11s %12s %8s %14s %6s\n",
		"arm", "context carried", "last step", "cache read", "fresh", "billed at "+strconv.Itoa(CacheReadPricePerHundred)+"%", "events")
	for _, m := range measured {
		last := m.Result.Steps[len(m.Result.Steps)-1].InputTokens
		events := len(m.Result.Drops) + len(m.Result.Forks)
		fmt.Fprintf(&report, "%-18s %15d %11d %12d %8d %14d %6d\n",
			m.Arm, m.Result.InputTokens(), last, m.Result.CacheReadTokens(), m.Result.FreshTokens(), m.Result.BilledUnits(), events)
	}
	return report.String()
}

func Extend(session Session, steps int) Session {
	longer := Session{ID: session.ID + "-x" + strconv.Itoa(steps), Task: session.Task}
	for len(longer.Steps) < steps {
		step := session.Steps[len(longer.Steps)%len(session.Steps)]
		step.Index = len(longer.Steps) + 1
		longer.Steps = append(longer.Steps, step)
	}
	return longer
}

func aboveTheSheet(carry string, sheet []string) string {
	inBand := make(map[string]bool, len(sheet))
	for _, line := range sheet {
		inBand[line] = true
	}
	var left []string
	for _, line := range strings.Split(carry, "\n") {
		if !inBand[strings.TrimSpace(line)] {
			left = append(left, line)
		}
	}
	return strings.Join(left, "\n")
}

func recordedBody(call SessionCall) string {
	stand := call.Tool + " result of " + call.Command + ", length recorded, body not\n"
	if len(stand) >= call.RenderedBytes {
		return stand[:call.RenderedBytes]
	}
	return stand + strings.Repeat(".", call.RenderedBytes-len(stand))
}
