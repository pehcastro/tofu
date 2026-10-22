package turn

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/recall"
)

const anthropicLookbackBlocks = 20

type cachedRequest struct {
	state               bool
	tokens, read, write int
}

type cachingLead struct {
	preview recall.Config
	steps   func(step int) llm.Decision
	cached  map[string]bool
	sent    []cachedRequest
	took    int
}

func newCachingLead(t *testing.T, steps func(step int) llm.Decision) *cachingLead {
	t.Helper()
	preview, err := recall.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	return &cachingLead{preview: preview.OnWire("anthropic"), steps: steps, cached: map[string]bool{}}
}

func (m *cachingLead) Ask(_ context.Context, request llm.Request) (llm.Decision, error) {
	conversation := slices.DeleteFunc(slices.Clone(request.Messages), func(message llm.Message) bool { return message.Role == llm.RoleSystem })
	running, lastAssistant := sha256.New(), -1
	keys, tokens := make([]string, len(conversation)), make([]int, len(conversation))
	for i, message := range conversation {
		if message.Role == llm.RoleAssistant {
			lastAssistant = i
		}
		said, err := json.Marshal(message)
		if err != nil {
			return llm.Decision{}, err
		}
		running.Write(said)
		keys[i], tokens[i] = string(running.Sum(nil)), HistoryTokens(m.preview, conversation[:i+1])
	}
	marks := []int{len(conversation) - 1}
	if lastAssistant > 0 {
		marks = append(marks, lastAssistant-1)
	}
	read := 0
	for _, mark := range marks {
		for at := mark; at >= max(0, mark-anthropicLookbackBlocks); at-- {
			if m.cached[keys[at]] {
				read = max(read, tokens[at])
				break
			}
		}
	}
	for _, mark := range marks {
		m.cached[keys[mark]] = true
	}
	total := tokens[len(tokens)-1]
	state := conversation[len(conversation)-1].Origin.Source == llm.SourceForkState
	m.sent = append(m.sent, cachedRequest{state: state, tokens: total, read: read, write: total - read})
	if state {
		return llm.Decision{Build: "m1", Outcome: llm.OutcomeMessage, Content: "goal: rename the parser"}, nil
	}
	m.took++
	return m.steps(m.took), nil
}

func saidAndRead(step int, said, path string) llm.Decision {
	decision := toolCallDecision(llm.ToolCall{ID: "call-" + strconv.Itoa(step), Name: "read", Arguments: json.RawMessage(`{"path":"` + path + `"}`)})
	decision.Content = said
	return decision
}

func longThenShortSteps(last int) func(step int) llm.Decision {
	return func(step int) llm.Decision {
		switch {
		case step > last:
			return messageDecision()
		case step <= 4:
			return saidAndRead(step, strings.Repeat("the parser rename reaches part "+strconv.Itoa(step)+". ", 340), "big"+strconv.Itoa(step)+".go")
		}
		return saidAndRead(step, strings.Repeat("one more caller renamed. ", 20), "small.go")
	}
}

func TestNoRequestRewritesMoreThanTheWarmSuffixOfWhatTheRequestBeforeItCached(t *testing.T) {
	bigReads := map[string]int{"big1.go": 300, "big2.go": 300, "big3.go": 300, "big4.go": 300}
	forkedAt := 0
	for _, c := range []struct {
		name  string
		spent bool
	}{
		{"a continuation fork", false},
		{"a fork the spent account forces at the same step", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			model := newCachingLead(t, longThenShortSteps(90))
			config, steps := forkingConfig(t, model, bigReads, "40000")
			config.Caps.MaxSteps = 100
			if c.spent {
				config.Accounts.Next = func(context.Context, Account) (Account, bool, error) {
					return Account{ID: 2}, model.took == forkedAt, nil
				}
			}
			if _, err := Run(context.Background(), config); err != nil {
				t.Fatal(err)
			}
			states := 0
			for i, sent := range model.sent {
				if sent.state {
					states++
					if sent.write >= 1000 {
						t.Errorf("the working-state ask, request %d, read %d and wrote %d tokens, want under 1000 written", i+1, sent.read, sent.write)
					}
				}
				if i == 0 || sent.tokens < model.sent[i-1].tokens/2 {
					continue
				}
				if grew := max(0, sent.tokens-model.sent[i-1].tokens); sent.write > grew+konst.TrimWarmSuffixTokens {
					t.Errorf("request %d of %d grew %d tokens and wrote %d, past the %d token warm suffix: something before it was rewritten",
						i+1, len(model.sent), grew, sent.write, konst.TrimWarmSuffixTokens)
				}
			}
			for _, step := range *steps {
				if step.Fork != nil && forkedAt == 0 {
					forkedAt = step.Index
				}
			}
			if states == 0 || forkedAt == 0 {
				t.Fatalf("%d working-state asks and the first fork at step %d: the run never forked, so it proves nothing", states, forkedAt)
			}
		})
	}
}

func TestWithNoForkTheTrimStillGetsUnderTheTargetRewritingTheNewestResultsFirst(t *testing.T) {
	reads := map[string]int{}
	for i := 1; i <= 40; i++ {
		reads["f"+strconv.Itoa(i)+".go"] = 3000
	}
	model := newCachingLead(t, func(step int) llm.Decision {
		if step > 40 {
			return messageDecision()
		}
		return saidAndRead(step, "reading part "+strconv.Itoa(step), "f"+strconv.Itoa(step)+".go")
	})
	config, steps := forkingConfig(t, model, reads, "40000")
	config.NoFork, config.Caps.MaxSteps = true, 100
	if _, err := Run(context.Background(), config); err != nil {
		t.Fatal(err)
	}
	target, trimmed := config.Budget.Bands.Target(), 0
	for _, step := range *steps {
		if step.Compaction == nil {
			continue
		}
		trimmed++
		if step.Compaction.TokensAfter > target {
			t.Errorf("the trim at step %d left %d tokens, over the %d token target", step.Index, step.Compaction.TokensAfter, target)
		}
		if slices.ContainsFunc(step.Compaction.Drops, func(drop recall.Drop) bool { return drop.Step == 1 }) && trimmed == 1 {
			t.Errorf("the first trim, at step %d, rewrote step 1's result when newer ones would have done: %+v", step.Index, step.Compaction.Drops)
		}
	}
	if trimmed == 0 {
		t.Fatalf("a run with forks off compacted nothing across %d steps, %d requests", len(*steps), len(model.sent))
	}
	for i, sent := range model.sent {
		if sent.tokens > config.Budget.Ceiling() {
			t.Errorf("request %d went out at %d tokens, past the %d ceiling", i+1, sent.tokens, config.Budget.Ceiling())
		}
	}
}

type idlingRead struct {
	sizedRead
	clock *time.Time
	idle  time.Duration
}

func (r idlingRead) Run(ctx context.Context, raw json.RawMessage) (Result, error) {
	*r.clock = r.clock.Add(r.idle)
	return r.sizedRead.Run(ctx, raw)
}

func TestATrimRewritesOldResultsInsteadOfForkingOnlyOnceTheCacheHasGoneCold(t *testing.T) {
	const steps = 20
	reads := map[string]int{}
	for i := 1; i <= steps; i++ {
		reads["f"+strconv.Itoa(i)+".go"] = 4000
	}
	run := func(idle time.Duration) []StepRow {
		model := newCachingLead(t, func(step int) llm.Decision {
			if step > steps {
				return messageDecision()
			}
			return saidAndRead(step, strings.Repeat("the parser rename reaches part "+strconv.Itoa(step)+". ", 120), "f"+strconv.Itoa(step)+".go")
		})
		clock := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
		config, rows := forkingConfig(t, model, nil, "60000")
		config.Tools = NewRegistry(idlingRead{sizedRead: sizedRead{bytes: reads}, clock: &clock, idle: idle})
		config.Now = func() time.Time { return clock }
		if _, err := Run(context.Background(), config); err != nil {
			t.Fatal(err)
		}
		return *rows
	}
	firstRelief := func(rows []StepRow) StepRow {
		at := slices.IndexFunc(rows, func(row StepRow) bool { return row.Fork != nil || row.Compaction != nil })
		if at < 0 {
			t.Fatal("the run never crossed the target, so it proves nothing")
		}
		return rows[at]
	}
	warm := firstRelief(run(time.Second))
	if warm.Fork == nil || warm.Compaction != nil {
		t.Errorf("with the cache warm, step %d answered the crossing with fork %v and trim %+v, want the fork alone", warm.Index, warm.Fork != nil, warm.Compaction)
	}
	cold := firstRelief(run(time.Duration(konst.TrimColdAfterIdleMinutes+1) * time.Minute))
	if cold.Fork != nil || cold.Compaction == nil || !slices.ContainsFunc(cold.Compaction.Drops, func(drop recall.Drop) bool { return drop.Step == 1 }) {
		t.Errorf("after %d idle minutes, step %d answered the crossing with fork %v and trim %+v, want a trim reaching step 1 and no fork",
			konst.TrimColdAfterIdleMinutes+1, cold.Index, cold.Fork != nil, cold.Compaction)
	}
}
