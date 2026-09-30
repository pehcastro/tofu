package turn

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"

	"tofu/internal/llm"
	"tofu/internal/recall"
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

func browsePages(t *testing.T, resultCap int, pages []shownPage) (*stubModel, string) {
	t.Helper()
	var results []string
	model := &stubModel{}
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
	return model, dir
}

func toolResults(request llm.Request) []string {
	var results []string
	for _, message := range request.Messages {
		if message.Role == llm.RoleTool {
			results = append(results, message.Content)
		}
	}
	return results
}

func TestFiveObservesOfFivePagesLeaveTheLastWholeAndFourShortEntries(t *testing.T) {
	var pages []shownPage
	for i := 1; i <= 5; i++ {
		n := strconv.Itoa(i)
		pages = append(pages, shownPage{tool: "browser_observe", note: "listing " + n + " costs R$ 4" + n + "0 a night; next open listing " + strconv.Itoa(i+1),
			url: "https://stays.test/rooms/" + n, title: "Stay number " + n, rows: 300})
	}
	pages[2].url = "https://stays.test/rooms/3?adults=2&checkin=2026-10-09&checkout=2026-10-15&currency=BRL"
	pages[2].note = strings.Repeat("R$ 4.667 total for listing 3, ", 7)[:200]
	model, dir := browsePages(t, 32768, pages)

	last := model.requests[len(model.requests)-1]
	results := toolResults(last)
	if len(results) != 5 {
		t.Fatalf("the last request holds %d tool results, want 5", len(results))
	}
	if whole := pages[4].result(); len(results[4]) != len(whole) || !strings.Contains(results[4], "Stay number 5") {
		t.Errorf("the last page is %d bytes in the prompt, want it whole at %d", len(results[4]), len(whole))
	}
	for i, shrunk := range results[:4] {
		t.Logf("entry %d, %d bytes:\n%s", i+1, len(shrunk), shrunk)
		if len(shrunk) > 400 || !strings.Contains(shrunk, "tab 1 "+pages[i].url+" ") || !strings.Contains(shrunk, pages[i].title) || !strings.Contains(shrunk, pages[i].note) {
			t.Errorf("entry %d is %d bytes, want at most 400 holding its url, title and note", i+1, len(shrunk))
		}
		_, named, _ := strings.Cut(shrunk, "\nartifact ")
		handle, _, _ := strings.Cut(named, " ")
		held, err := recall.NewStore(dir).Fetch(handle)
		if err != nil || !strings.Contains(string(held), pages[i].title) || len(held) < 300*40 {
			t.Errorf("entry %d names artifact %s, which does not hold its page whole: %d bytes, %v", i+1, handle, len(held), err)
		}
	}
	for step, request := range model.requests[2:] {
		for i, shrunk := range toolResults(request)[:step+1] {
			if shrunk != results[i] {
				t.Errorf("entry %d read differently in request %d than in the last, so the cached prefix broke:\n%s", i+1, step+3, shrunk)
			}
		}
	}
}

func TestAnActThatListsOnlyChangesShrinksToThePageItActedOnAndErrorsStayWhole(t *testing.T) {
	pages := []shownPage{
		{tool: "browser_observe", note: "on the search page", url: "https://stays.test/s/Atibaia", title: "Atibaia stays", rows: 300},
		{tool: "browser_act", note: "opened the filters", lead: "1. click e5: the page changed\nran 1 of 1\n\n", rows: 200},
		{tool: "browser_observe", note: "the tab closed on me"},
		{tool: "browser_observe", note: "filters set, reading the results", url: "https://stays.test/s/Atibaia?price_max=500&" + strings.Repeat("amenities%5B%5D=7&", 30), title: "Atibaia under 500", rows: 20},
		{tool: "browser_act", note: "open the first result", lead: "1. click e9: the url changed\nran 1 of 1\n\n", url: "https://stays.test/rooms/1", title: "Stay one", rows: 400},
	}
	model, dir := browsePages(t, 16384, pages)
	results := toolResults(model.requests[len(model.requests)-1])
	for i, shrunk := range results {
		t.Logf("entry %d, %d bytes:\n%s", i+1, len(shrunk), shrunk)
	}
	if act := results[1]; len(act) > 400 || !strings.Contains(act, "1. click e5: the page changed") || !strings.Contains(act, "https://stays.test/s/Atibaia") || !strings.Contains(act, "Atibaia stays") || !strings.Contains(act, "opened the filters") {
		t.Errorf("the act that listed only changes shrank without its action, the page it acted on, or its note:\n%s", act)
	}
	if !strings.HasPrefix(results[2], "error: ") || !strings.Contains(results[2], "the tab closed") {
		t.Errorf("the failed observe did not stay whole:\n%s", results[2])
	}
	if long := results[3]; len(long) > 400+len(pages[3].url) || !strings.Contains(long, "tab 1 "+pages[3].url+" ") || !strings.Contains(long, "filters set, reading the results") {
		t.Errorf("a page with a %d byte url did not shrink around its whole url and its note:\n%s", len(pages[3].url), long)
	}
	if !strings.HasPrefix(results[4], "artifact ") || !strings.Contains(results[4], " holds this result whole") {
		t.Fatalf("the newest page, over the result cap, is not as it was rendered:\n%s", results[4])
	}
	rendered := strings.Fields(results[4])[1]
	forked := append(slices.Clone(model.requests[len(model.requests)-1].Messages), llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "call-9", Name: "browser_observe", Arguments: json.RawMessage(`{"tab":1,"note":"back on the results"}`)}}},
		llm.Message{Role: llm.RoleTool, ToolCallID: "call-9", Content: pages[3].result()})
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
	if !carried || !strings.Contains(forked[len(forked)-3].Content, "artifact "+rendered+" ") {
		t.Errorf("the rendered page shrank or forked without the handle it was rendered under, %s:\n%s\n%+v", rendered, forked[len(forked)-3].Content, fork.Carry.Results)
	}
}
