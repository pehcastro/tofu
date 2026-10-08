package turn

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/recall"
	"tofu/internal/session"
	"tofu/internal/web"
)

type scriptedPages struct {
	name  string
	pages *[]string
}

func (s scriptedPages) Name() string { return s.name }

func (s scriptedPages) Definition() llm.Tool {
	return llm.Tool{Name: s.name, Parameters: map[string]any{"type": "object"}}
}

func (s scriptedPages) Run(context.Context, json.RawMessage) (Result, error) {
	next := (*s.pages)[0]
	*s.pages = (*s.pages)[1:]
	if next == "" {
		return Result{}, errors.New("the tab closed")
	}
	return Result{Content: next}, nil
}

type shownPage struct {
	tool, note, url, title, lead string
	rows                         int
}

func (p shownPage) result() string {
	header := "tab 1 " + p.url + " " + strconv.Quote(p.title) + "\n"
	if p.url == "" {
		header = ""
	}
	return p.lead + web.Untrusted("Chrome tab 1", header+strings.Repeat("- link \"a stay in Atibaia, R$ 480 a night\" [ref=e9]\n", p.rows))
}

type seenModel struct {
	*stubModel
	seen [][]llm.Message
}

func (m *seenModel) Ask(ctx context.Context, request llm.Request) (llm.Decision, error) {
	m.seen = append(m.seen, slices.Clone(request.Messages))
	return m.stubModel.Ask(ctx, request)
}

func browsePages(t *testing.T, resultCap int, pages []shownPage) ([][]llm.Message, string) {
	t.Helper()
	var results []string
	model := &seenModel{stubModel: &stubModel{}}
	for i, page := range pages {
		args, err := json.Marshal(map[string]any{"tab": 1, "note": page.note})
		if err != nil {
			t.Fatal(err)
		}
		model.decisions = append(model.decisions, toolCallDecision(llm.ToolCall{ID: "call-" + strconv.Itoa(i), Name: page.tool, Arguments: args}))
		if page.rows == 0 {
			results = append(results, "")
			continue
		}
		results = append(results, page.result())
	}
	model.decisions = append(model.decisions, messageDecision())
	dir := t.TempDir()
	config := Config{Model: model, Spend: SpendSubscription, Task: "find a stay in Atibaia", ResultBytesCap: resultCap, ArtifactDir: dir, NoLastWord: true,
		Tools: NewRegistry(scriptedPages{"browser_observe", &results}, scriptedPages{"browser_act", &results})}
	if _, err := Run(context.Background(), config); err != nil {
		t.Fatal(err)
	}
	return model.seen, dir
}

func toolResults(messages []llm.Message) []string {
	var results []string
	for _, message := range messages {
		if message.Role == llm.RoleTool {
			results = append(results, message.Content)
		}
	}
	return results
}

func TestSixPagesShrinkInOneBatchKeepingTheNoteWrittenAfterEachAndTheCachedPrefix(t *testing.T) {
	var pages []shownPage
	for i := 1; i <= 6; i++ {
		n := strconv.Itoa(i)
		pages = append(pages, shownPage{tool: "browser_observe", note: "listing " + n + " costs R$ 4" + n + "0 a night; next open listing " + strconv.Itoa(i+1),
			url: "https://stays.test/rooms/" + n, title: "Stay number " + n, rows: 300})
	}
	pages[2].url = "https://stays.test/rooms/3?adults=2&checkin=2026-10-09&checkout=2026-10-15&currency=BRL"
	pages[2].note = strings.Repeat("R$ 4.667 total for listing 3, ", 7)[:200]
	pages[3].note = strings.Repeat("the Filtros panel holds Wifi, Pool, Kitchen; ", 5)[:200]
	requests, dir := browsePages(t, 32768, pages)

	var broke []int
	for r := 1; r < len(requests); r++ {
		results := toolResults(requests[r])
		var whole []int
		for i, result := range results {
			if shrunkHandle(result) == "" {
				whole = append(whole, i)
			}
		}
		if len(whole) > 4 || len(results) >= 2 && !slices.Contains(whole, len(results)-2) || !slices.Contains(whole, len(results)-1) {
			t.Errorf("request %d holds pages %v whole out of %d, want at most 4 and always the last 2", r, whole, len(results))
		}
		before := requests[r-1]
		if len(requests[r]) < len(before) || !slices.EqualFunc(before, requests[r][:len(before)], func(a, b llm.Message) bool { return a.Content == b.Content && a.ToolCallID == b.ToolCallID }) {
			broke = append(broke, r)
		}
	}
	if !slices.Equal(broke, []int{5}) {
		t.Errorf("the prefix changed before requests %v, want only before request 5, where the one batch shrinks", broke)
	}

	results := toolResults(requests[len(requests)-1])
	if len(results) != 6 {
		t.Fatalf("the last request holds %d tool results, want 6", len(results))
	}
	for i := 3; i < 6; i++ {
		if whole := pages[i].result(); len(results[i]) != len(whole) || !strings.Contains(results[i], pages[i].title) {
			t.Errorf("page %d is %d bytes in the prompt, want it whole at %d", i+1, len(results[i]), len(whole))
		}
	}
	for i, shrunk := range results[:3] {
		t.Logf("entry %d, %d bytes:\n%s", i+1, len(shrunk), shrunk)
		if len(shrunk) > shrunkPageBytes || !strings.Contains(shrunk, "tab 1 "+pages[i].url+" ") || !strings.Contains(shrunk, pages[i].title) || !strings.Contains(shrunk, pages[i].note) || !strings.Contains(shrunk, pages[i+1].note) {
			t.Errorf("entry %d is %d bytes, want at most %d holding its url, title, its own note and the note written after it", i+1, len(shrunk), shrunkPageBytes)
		}
		handle := shrunkHandle(shrunk)
		held, err := recall.NewStore(dir).Fetch(handle)
		if err != nil || !strings.Contains(string(held), pages[i].title) || len(held) < 300*40 {
			t.Errorf("entry %d names artifact %s, which does not hold its page whole: %d bytes, %v", i+1, handle, len(held), err)
		}
	}
}

func TestSmallOldPagesBehindTwoLargeOnesStayWholeBecauseTheRewriteCostsMoreThanItSaves(t *testing.T) {
	var pages []shownPage
	for i, rows := range []int{20, 20, 20, 300, 300} {
		n := strconv.Itoa(i + 1)
		pages = append(pages, shownPage{tool: "browser_observe", note: "reading listing " + n, url: "https://stays.test/rooms/" + n, title: "Stay number " + n, rows: rows})
	}
	requests, _ := browsePages(t, 32768, pages)
	for r := 1; r < len(requests); r++ {
		before := requests[r-1]
		if len(requests[r]) < len(before) || !slices.EqualFunc(before, requests[r][:len(before)], func(a, b llm.Message) bool { return a.Content == b.Content }) {
			t.Errorf("the prefix changed before request %d, want it never to change when no shrink pays for its rewrite", r)
		}
	}
	for i, result := range toolResults(requests[len(requests)-1]) {
		if len(result) != len(pages[i].result()) || !strings.Contains(result, pages[i].title) {
			t.Errorf("page %d is %d bytes in the last request, want it whole and unchanged at %d", i+1, len(result), len(pages[i].result()))
		}
	}
}

func TestAnActThatListsOnlyChangesShrinksToThePageItActedOnAndErrorsStayWhole(t *testing.T) {
	pages := []shownPage{
		{tool: "browser_observe", note: "on the search page", url: "https://stays.test/s/Atibaia", title: "Atibaia stays", rows: 300},
		{tool: "browser_act", note: "opened the filters", lead: "1. click e5: the page changed\nran 1 of 1\n\n", rows: 200},
		{tool: "browser_observe", note: "the tab closed on me"},
		{tool: "browser_observe", note: "filters set, reading the results", url: "https://stays.test/s/Atibaia?price_max=500&" + strings.Repeat("amenities%5B%5D=7&", 30), title: "Atibaia under 500", rows: 20},
		{tool: "browser_act", note: "open the first result", lead: "1. click e9: the url changed to https://stays.test/rooms/1, a page titled \"Stay one\"\nran 1 of 1\n\n", url: "https://stays.test/rooms/1", title: "Stay one", rows: 400},
		{tool: "browser_observe", note: "stay one sleeps 4 at R$ 480", url: "https://stays.test/rooms/1/photos", title: "Stay one photos", rows: 20},
	}
	requests, dir := browsePages(t, 16384, pages)
	results := toolResults(requests[len(requests)-1])
	for i, shrunk := range results {
		t.Logf("entry %d, %d bytes:\n%s", i+1, len(shrunk), shrunk)
	}
	if act := results[1]; len(act) > shrunkPageBytes || !strings.Contains(act, "1. click e5: the page changed") || !strings.Contains(act, "https://stays.test/s/Atibaia") || !strings.Contains(act, "Atibaia stays") || !strings.Contains(act, "opened the filters") || !strings.Contains(act, "the tab closed on me") {
		t.Errorf("the act that listed only changes shrank without its action, the page it acted on, its note, or the note of the failed call after it:\n%s", act)
	}
	if !strings.HasPrefix(results[2], "error: ") || !strings.Contains(results[2], "the tab closed") {
		t.Errorf("the failed observe did not stay whole:\n%s", results[2])
	}
	if long := results[3]; len(long) > shrunkPageBytes+len(pages[3].url) || !strings.Contains(long, "tab 1 "+pages[3].url+" ") || !strings.Contains(long, "filters set, reading the results") || !strings.Contains(long, "open the first result") {
		t.Errorf("a page with a %d byte url did not shrink around its whole url and both notes:\n%s", len(pages[3].url), long)
	}
	if !strings.HasPrefix(results[4], "artifact ") || !strings.Contains(results[4], " holds this result whole") {
		t.Fatalf("the page over the result cap, still one of the last 2, is not as it was rendered:\n%s", results[4])
	}
	rendered := strings.Fields(results[4])[1]
	forked := slices.Clone(requests[len(requests)-1])
	for i, page := range []shownPage{{url: "https://stays.test/rooms/2", title: "Stay two", rows: 400}, pages[3], pages[3]} {
		id := "call-" + strconv.Itoa(9+i)
		forked = append(forked, llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: id, Name: "browser_observe", Arguments: json.RawMessage(`{"tab":1,"note":"back on the results"}`)}}},
			llm.Message{Role: llm.RoleTool, ToolCallID: id, Content: page.result()})
	}
	at := slices.IndexFunc(forked, func(message llm.Message) bool { return message.ToolCallID == "call-4" })
	artifacts, err := NewArtifacts(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	budget, err := recall.BudgetFor("m1", 0)
	if err != nil {
		t.Fatal(err)
	}
	fork, _, err := forkHistory(artifacts, budget, "find a stay in Atibaia", forked, ForkContinuation, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	var carried bool
	for _, kept := range fork.Carry.Results {
		carried = carried || kept.Handle == rendered
	}
	if !carried || shrunkHandle(forked[at].Content) != rendered {
		t.Errorf("the rendered page shrank or forked without the handle it was rendered under, %s:\n%s\n%+v", rendered, forked[at].Content, fork.Carry.Results)
	}
}

func forkStep(said string, results ...string) []llm.Message {
	step := []llm.Message{{Role: llm.RoleAssistant, Content: said}}
	for i, result := range results {
		id := said + "-" + strconv.Itoa(i)
		step[0].ToolCalls = append(step[0].ToolCalls, llm.ToolCall{ID: id, Name: "read", Arguments: json.RawMessage(`{"path":"` + id + `.go"}`)})
		step = append(step, llm.Message{Role: llm.RoleTool, ToolCallID: id, Content: result})
	}
	return step
}

func checkedFork(t *testing.T, budget recall.Budget, forced ForkKind, messages []llm.Message) (*Fork, []llm.Message) {
	t.Helper()
	artifacts, err := NewArtifacts(t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	fork, begun, err := forkHistory(artifacts, budget, "fix the parser", slices.Clone(messages), forced, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	tail := begun[3:]
	if fork.TailMessages != len(tail) || !slices.EqualFunc(tail, messages[len(messages)-len(tail):], func(a, b llm.Message) bool {
		return a.Content == b.Content && a.ToolCallID == b.ToolCallID && len(a.ToolCalls) == len(b.ToolCalls)
	}) {
		t.Fatalf("the fork records a tail of %d and keeps %d messages after the carry, want the end of the old history word for word", fork.TailMessages, len(tail))
	}
	called := map[string]bool{}
	for i, message := range begun {
		for _, call := range message.ToolCalls {
			called[call.ID] = true
			if !slices.ContainsFunc(begun[i+1:], func(m llm.Message) bool { return m.ToolCallID == call.ID }) {
				t.Errorf("call %s is kept and its result is not", call.ID)
			}
		}
		if message.Role == llm.RoleTool && !called[message.ToolCallID] {
			t.Errorf("the result of %s is kept and its call is not", message.ToolCallID)
		}
		if len(message.Images) > 0 && i >= 3 {
			t.Errorf("message %d in the tail carries an image the estimate never priced", i)
		}
	}
	if budget.Crossed(artifacts.preview, historyOf(begun)) {
		t.Errorf("the forked history is %d tokens against a %d target", budget.Tokens(artifacts.preview, historyOf(begun)), budget.Bands.Target())
	}
	if len(tail) > 0 && strings.Contains(fork.Carry.Text, "the last thing it said or did:\n"+messages[len(messages)-len(tail)].Content) {
		t.Errorf("the carry repeats the last word the tail already holds:\n%s", fork.Carry.Text)
	}
	return fork, begun
}

func TestForkKeepsWholeStepsOfTheTailAndNeverSplitsACallFromItsResult(t *testing.T) {
	budget := recall.Budget{}.At(6300+konst.ImageTokens*100/konst.ContextForkPercentOfUsable, "a small budget to fork under, with room for the task's picture")
	small, large := strings.Repeat("a line of the parser\n", 10), strings.Repeat("a line of the parser\n", 300)
	head := []llm.Message{{Role: llm.RoleSystem, Content: "you are tofu"}, {Role: llm.RoleUser, Content: "fix the parser", Images: []llm.Image{{MediaType: "image/png", Data: []byte("png")}}}}
	history := func(steps ...[]llm.Message) []llm.Message {
		return slices.Concat(append([][]llm.Message{head}, steps...)...)
	}
	for _, c := range []struct {
		name     string
		forced   ForkKind
		messages []llm.Message
		tail     int
	}{
		{"a tail that would start on a tool result", ForkContinuation,
			history(forkStep("old", large), forkStep(large+"read the lexer", small), forkStep("read the grammar", small)), 2},
		{"parallel tool calls in one step", ForkContinuation,
			history(forkStep("old", large), forkStep("read three files at once", small, small, small)), 4},
		{"a tail bigger than the budget on its own", ForkContinuation,
			history(forkStep("old", small), forkStep("read the whole generated table", large, large)), 0},
		{"an image in the tail", ForkContinuation,
			history(forkStep("old", small), []llm.Message{{Role: llm.RoleUser, Content: "this one", Images: []llm.Image{{MediaType: "image/png", Data: []byte("png")}}}}, forkStep("read it", small)), 2},
		{"a forced fork with almost no history", ForkAccountSpent,
			history(forkStep("read the parser", small)), 2},
	} {
		t.Run(c.name, func(t *testing.T) {
			fork, _ := checkedFork(t, budget, c.forced, c.messages)
			if fork.TailMessages != c.tail {
				t.Errorf("kept %d messages after the carry, want %d", fork.TailMessages, c.tail)
			}
		})
	}
	t.Run("a second fork right after the first", func(t *testing.T) {
		_, begun := checkedFork(t, budget, ForkContinuation, history(forkStep("old", large), forkStep("read the lexer", small), forkStep("read the grammar", small)))
		artifacts, err := NewArtifacts(t.TempDir(), true)
		if err != nil {
			t.Fatal(err)
		}
		next := append(slices.Clone(begun), forkStep("read the tests", small)...)
		if budget.Crossed(artifacts.preview, historyOf(next)) {
			t.Fatalf("one small step after the fork crosses the %d target again, so the turn forks every step", budget.Bands.Target())
		}
		fork, again := checkedFork(t, budget, ForkContinuation, next)
		if slices.ContainsFunc(again[3:], func(m llm.Message) bool { return m.Content == begun[2].Content }) || fork.TailMessages == 0 {
			t.Errorf("the second fork kept %d messages and the first carry among them is %v", fork.TailMessages, slices.ContainsFunc(again[3:], func(m llm.Message) bool { return m.Content == begun[2].Content }))
		}
	})
}

func TestForkAtTheDefaultCeilingRecordsTheCountThatDecidedAndKeepsANewestStepOverTheTailCap(t *testing.T) {
	cfg, err := recall.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	budget := recall.Budget{}.At(konst.ContextCeilingTokens, "the default ceiling").Sending(cfg, strings.Repeat(`{"name":"read"}`, 2000))
	table := strings.Repeat("0001 func parser(lexer token) window { return budget.carry(1) }\n", 480)
	messages := slices.Concat(
		[]llm.Message{{Role: llm.RoleSystem, Content: "you are tofu"}, {Role: llm.RoleUser, Content: "read the tables"}},
		forkStep("old", strings.Repeat(table, 15)),
		forkStep("read two generated tables at once", table, table))
	newest := HistoryTokens(cfg, messages[len(messages)-3:])
	if newest <= konst.ForkTailTokens {
		t.Fatalf("the newest step is %d tokens, under the %d tail cap, so it proves nothing", newest, konst.ForkTailTokens)
	}
	fork, _ := checkedFork(t, budget, "", messages)
	if decided := budget.Tokens(cfg, historyOf(messages)); fork.TokensBefore != decided {
		t.Errorf("the fork records %d tokens before and the count that decided it was %d against a %d target", fork.TokensBefore, decided, budget.Bands.Target())
	}
	if fork.TailMessages != 3 {
		t.Errorf("kept %d messages after the carry, want the newest step of %d tokens whole, 3 messages, with %d of room under the target",
			fork.TailMessages, newest, budget.Bands.Target()-fork.TokensAfter)
	}
}

func TestCompactCarriedShrinksEveryOldResultAndTheNewSessionReadsBackShrunk(t *testing.T) {
	big := strings.Repeat("a numbered line of a large file\n", 400)
	read := func(id string) []llm.ToolCall {
		return []llm.ToolCall{{ID: id, Name: "read", Arguments: json.RawMessage(`{"path":"` + id + `.txt"}`)}}
	}
	carried := []llm.Message{
		{Role: llm.RoleUser, Content: "read a"},
		{Role: llm.RoleAssistant, Content: "reading a", ToolCalls: read("a")},
		{Role: llm.RoleTool, ToolCallID: "a", Content: big},
		{Role: llm.RoleAssistant, Content: "a is numbered lines"},
		{Role: llm.RoleUser, Content: "read b and c"},
		{Role: llm.RoleAssistant, Content: "reading b", ToolCalls: read("b")},
		{Role: llm.RoleTool, ToolCallID: "b", Content: big},
		{Role: llm.RoleAssistant, Content: "reading c", ToolCalls: read("c")},
		{Role: llm.RoleTool, ToolCallID: "c", Content: big},
	}
	artifacts := t.TempDir()
	compacted, err := CompactCarried(artifacts, "", carried)
	if err != nil {
		t.Fatal(err)
	}
	shrunk := compacted.Messages
	if compacted.Results != 2 || compacted.TokensAfter >= compacted.TokensBefore {
		t.Fatalf("compacted %+v, want the two results before the last step shrunk and fewer tokens after", compacted)
	}
	if !strings.Contains(shrunk[2].Content, "artifact ") || !strings.Contains(shrunk[6].Content, "/compact") || shrunk[8].Content != big {
		t.Fatalf("a and b should hold a handle naming /compact and the last step's c stay whole:\n%s\n%s\n%.80s", shrunk[2].Content, shrunk[6].Content, shrunk[8].Content)
	}
	if carried[2].Content != big {
		t.Fatal("the history handed in was shrunk in place, so a failed write would leave it shrunk")
	}
	if again, err := CompactCarried(artifacts, "", shrunk); err != nil || again.Results != 0 {
		t.Fatalf("a second compact shrank %d result(s) again, err %v", again.Results, err)
	}

	store := session.OpenAt(t.TempDir())
	old, err := store.Open(session.Header{ID: session.NewEventID(), Wire: "anthropic"})
	if err != nil {
		t.Fatal(err)
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	into, err := RecordCarried(store, old.ID(), compacted, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	events, err := store.Body(into)
	if err != nil {
		t.Fatal(err)
	}
	reread, err := ConversationFrom(events)
	if err != nil {
		t.Fatal(err)
	}
	if len(reread) != len(shrunk) {
		t.Fatalf("the new session reads back %d messages, want %d", len(reread), len(shrunk))
	}
	for i, message := range reread {
		if message.Role != shrunk[i].Role || message.Content != shrunk[i].Content || message.ToolCallID != shrunk[i].ToolCallID || len(message.ToolCalls) != len(shrunk[i].ToolCalls) {
			t.Errorf("message %d reads back as %+v, want %+v", i, message, shrunk[i])
		}
	}
	ended, err := store.Header(old.ID())
	if err != nil {
		t.Fatal(err)
	}
	begun, err := store.Header(into)
	if err != nil {
		t.Fatal(err)
	}
	if ended.ForkedInto != into || ended.EndReason != session.EndedByFork || ended.ForkTokensBefore != compacted.TokensBefore ||
		ended.ForkTokensAfter != compacted.TokensAfter || begun.ForkKind != string(ForkCompact) ||
		begun.CarriedFrom == nil || begun.CarriedFrom.Session != old.ID() || begun.Wire != "anthropic" {
		t.Errorf("the old session reads %+v and the new one %+v", ended, begun)
	}
}
