package host

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"tofu/internal/judge/ledger"
	"tofu/internal/llm/quota"
	"tofu/internal/session"
	"tofu/internal/sys"
)

func TestBoardQueriesAnswerTypedReportsFromTheProjectsOwnRecords(t *testing.T) {
	now := time.Now()
	var project string
	verbs := map[string]string{
		"rules list":     `{"origin":"the binary","rules":[{"id":"comments","kind":"structural","origin":"shipped","mode":"shadow"},{"id":"mine","kind":"human","origin":"project","override":{"rule_id":"mine","layer":"project","change":"added","text":"say less","stale":false,"file":"x"}}]}`,
		"context g":      `{"session":"g"}`,
		"login --status": `{"subscriptions":[{"role":"llm","source":"claude-sub","accounts":[{"id":2,"account":"a","state":"standby","login":"ok"},{"id":1,"account":"b","state":"in_use","login":"ok"}]}],"keys":[]}`,
	}
	verb := func(args []string) (VerbResult, error) {
		line := strings.Join(args, " ")
		if strings.HasPrefix(line, "session info ") {
			return VerbResult{OK: true, Data: json.RawMessage(`{"id":"` + args[2] + `","handle":"h","at":"2026-10-08T10:00:00Z","turns":1,"sub_agents":0,"steps":1,"carried_messages":0,"reads":0}`)}, nil
		}
		data, known := verbs[line]
		return VerbResult{OK: known, Data: json.RawMessage(data)}, nil
	}
	var written ledger.Row
	c, _, dir := serving(t, nil, ServeConfig{Verb: verb, Ledger: func(LedgerParams) (LedgerReport, error) {
		return LedgerReport{Rows: []LedgerRow{{Row: written}}}, nil
	}})
	project = dir
	store, err := session.OpenIn(project)
	if err != nil {
		t.Fatal(err)
	}
	record := func(header session.Header, events ...session.Event) {
		t.Helper()
		log, err := store.Open(header)
		if err != nil {
			t.Fatal(err)
		}
		for _, event := range events {
			if _, err := log.Append(event, nil); err != nil {
				t.Fatal(err)
			}
		}
		if err := log.Close(); err != nil {
			t.Fatal(err)
		}
	}
	started := now.Add(-30 * time.Minute)
	firedOn := 6
	if started.Day() != now.Day() {
		firedOn = 5
	}
	record(session.Header{ID: "f", At: started, ForkedInto: "g", ForkTokensBefore: 900, ForkTokensAfter: 300},
		session.Event{At: started, Turn: "t1", Kind: session.EventTurnStart, Body: json.RawMessage(`{"task":"t","wire":"anthropic","spend":"subscription","account":1}`)},
		session.Event{At: started, Turn: "t1", Kind: session.EventPrompt, Body: json.RawMessage(`{"system":"[code_rules, from the rule comments]\nno comments"}`)},
		session.Event{At: started.Add(time.Minute), Turn: "t1", ID: "r1", Kind: session.EventRequest, Attempt: 1, Body: json.RawMessage(`{"model":"haiku","prompt_tokens":100,"completion_tokens":7}`)})
	record(session.Header{ID: "g", At: started.Add(2 * time.Minute), CarriedFrom: &session.Carried{Session: "f"}},
		session.Event{At: started.Add(3 * time.Minute), Turn: "t1-f2", Kind: session.EventMessage, Body: json.RawMessage(`{"role":"user","content":"carry on"}`)})
	state, err := sys.ProjectStateDirAt(project)
	if err != nil {
		t.Fatal(err)
	}
	command := "git status " + strings.Repeat("x", 5000)
	body, _ := json.Marshal(map[string]any{"tool": "bash", "input": map[string]any{"command": command}})
	written, err = ledger.NewWriter(filepath.Join(state, "log")).Append(ledger.Row{Point: "tool_gate", At: now.Add(-time.Minute), State: body, TurnID: "t1", Verdict: ledger.VerdictAsk,
		Reason: &ledger.Reason{Comparison: "risk_ask_at", Threshold: 1.5, Mode: ledger.ModeShadow}})
	if err != nil || written.StateElision == nil {
		t.Fatalf("writing a row whose state is held beside the ledger: %v, elided %v", err, written.StateElision)
	}
	readings, err := sys.QuotaDir()
	if err != nil {
		t.Fatal(err)
	}
	for _, reading := range []quota.Reading{
		{Provider: "claude-sub", Account: 1, At: now.Add(-time.Hour), Windows: []quota.ReadingWindow{{ID: "5h", Used: 0.1, ResetsAt: now.Add(4 * time.Hour)}}},
		{Provider: "claude-sub", Account: 1, At: now, Windows: []quota.ReadingWindow{{ID: "5h", Used: 0.3, ResetsAt: now.Add(4 * time.Hour)}}},
	} {
		if err := quota.AppendReading(readings, reading); err != nil {
			t.Fatal(err)
		}
	}
	c.ask("0", "initialize", `{"client":"boards"}`)
	c.answer("0", &InitializeResult{})

	c.ask("1", "query.session", `{"session":"g"}`)
	var detail SessionDetail
	c.answer("1", &detail)
	if len(detail.Generations) != 2 || detail.Generations[0].ForkedInto != "g" || detail.Generations[0].TokensBefore != 900 || !detail.Generations[1].Here {
		t.Errorf("query.session answered %+v, want the two generations with the fork and the one asked", detail.Generations)
	}

	c.ask("2", "query.usage.history", `{"range":"day"}`)
	var history UsageHistoryReport
	c.answer("2", &history)
	if history.Total.TokensIn != 100 || history.Total.Requests != 2 || len(history.Buckets) != 24 {
		t.Errorf("query.usage.history answered total %+v over %d buckets, want the one request and the one classifier row over 24 hours", history.Total, len(history.Buckets))
	}
	c.ask("3", "query.usage.history", `{"range":"year"}`)
	if refused := c.until(func(line wireLine) bool { return string(line.ID) == `"3"` }, "the answer to 3"); refused.Error == nil || !strings.Contains(refused.Error.Message, "day, week or month") {
		t.Errorf("an unknown range answered %s, want a refusal naming the ranges", refused.Result)
	}

	c.ask("4", "query.limits", `{}`)
	var limits LimitsReport
	c.answer("4", &limits)
	if len(limits.Burns) != 1 || limits.Burns[0].PerHour == nil || *limits.Burns[0].PerHour < 0.19 || *limits.Burns[0].PerHour > 0.21 {
		t.Errorf("query.limits burns %+v, want 0.2 an hour on account 1", limits.Burns)
	}
	if len(limits.Order) != 1 || !slices.Equal(limits.Order[0].Accounts, []int64{2, 1}) || len(limits.Spend) != 1 || limits.Spend[0].Account != 1 {
		t.Errorf("query.limits order %+v spend %+v, want the role's accounts in order and account 1's spend", limits.Order, limits.Spend)
	}

	c.ask("5", "query.context", `{"session":"g"}`)
	var context ContextReport
	c.answer("5", &context)
	if len(context.Items) == 0 || context.Items[len(context.Items)-1].Fate == "" {
		t.Errorf("query.context items %+v, want the session's items with a fate", context.Items)
	}

	c.ask("6", "query.skills", `{}`)
	var skills SkillsReport
	c.answer("6", &skills)
	if !slices.ContainsFunc(skills.Skills, func(one SkillListing) bool {
		return one.Name == "flake-triage" && one.Domain == "qa" && one.Origin == "library"
	}) {
		t.Errorf("query.skills answered %+v, want the library's flake-triage", skills.Skills)
	}

	c.ask("7", "query.rules", `{}`)
	var rules RuleListReport
	c.answer("7", &rules)
	if len(rules.Rules) != 2 || !strings.Contains(rules.Rules[0].Text, "comment") || rules.Rules[0].Trigger != "language go" || rules.Rules[0].Fires != 1 || rules.Rules[0].FiresWeek[firedOn] != 1 || rules.Rules[1].Text != "say less" {
		t.Errorf("query.rules answered %+v, want the shipped rule's text, trigger and fire, and the override's text", rules.Rules)
	}

	c.ask("8", "query.ledger.summary", `{}`)
	var summary LedgerSummary
	c.answer("8", &summary)
	if len(summary.Points) != 1 || summary.Points[0].Count != 1 || summary.Points[0].WouldAsk != 1 {
		t.Errorf("query.ledger.summary answered %+v, want tool_gate counted once as a would-ask", summary.Points)
	}
	c.ask("9", "query.ledger", `{"last":1}`)
	var rows LedgerReport
	c.answer("9", &rows)
	if len(rows.Rows) != 1 || rows.Rows[0].Subject == nil || rows.Rows[0].Subject.Tool != "bash" || rows.Rows[0].Subject.Command != command {
		t.Errorf("query.ledger row subject %+v, want bash and the command read from the elided state", rows.Rows[0].Subject)
	}

	schema, err := Schema()
	if err != nil {
		t.Fatal(err)
	}
	var declared struct {
		Requests map[string]struct {
			Result map[string]string `json:"result"`
		} `json:"x-requests"`
	}
	if err := json.Unmarshal(schema, &declared); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"query.session", "query.usage.history", "query.limits", "query.context", "query.skills", "query.rules", "query.ledger.summary", "query.ledger"} {
		if result := declared.Requests[method].Result["$ref"]; result == "" || strings.HasSuffix(result, "VerbResult") {
			t.Errorf("%s answers %q in the schema, want a typed result", method, result)
		}
	}
}
