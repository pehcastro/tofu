package turn

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/recall"
	"tofu/internal/safe"
	"tofu/internal/session"
	"tofu/internal/sys"
)

const AdvisorAgent = "advisor"

func (t *SpawnTool) Advise(ctx context.Context, plan string) {
	site, _ := ctx.Value(spawnSiteKey{}).(spawnSite)
	if site.log == nil {
		return
	}
	words := personWords(site.conversation)
	waiting := "advisor " + site.call
	t.Inbox.mu.Lock()
	t.Inbox.watched[waiting] = true
	t.Inbox.mu.Unlock()
	reading, cancel := outlivingTheWave(ctx)
	safe.Go(sys.Crashes(t.Project), waiting, func() {
		defer func() {
			cancel()
			t.Inbox.mu.Lock()
			delete(t.Inbox.watched, waiting)
			t.Inbox.mu.Unlock()
			t.Inbox.signal()
		}()
		read := t.advised(reading, site.log, site.turn, words, plan)
		said := fmt.Sprintf("the advisor read the plan against %d of the person's messages: %d missing, %d extra", read.Words, len(read.Missing), len(read.Extra))
		if read.Error != "" {
			said = "the advisor did not read the plan: " + read.Error
		}
		notice := session.AdvisorNotice{NoticeBody: session.NoticeBody{Text: said, From: RecordedOrigin(llm.Origin{Source: llm.SourceNotice})}, Advisor: &read}
		if _, err := site.log.Append(session.Event{Turn: site.turn, Agent: site.agent, Kind: session.EventNotice}, notice); err != nil && t.base.Notify != nil {
			t.base.Notify("the advisor's read of the plan was not recorded: " + err.Error())
		}
	})
}

func personWords(conversation []llm.Message) []string {
	var words []string
	for _, message := range conversation {
		if message.Role != llm.RoleUser || message.Origin.Source.Kind() != llm.OriginPerson {
			continue
		}
		if said, typed := TaskIn(message.Content); typed && strings.TrimSpace(said) != "" {
			words = append(words, strings.TrimSpace(said))
		}
	}
	return words
}

func (t *SpawnTool) advised(ctx context.Context, log *session.Log, turnID string, words []string, plan string) (read session.AdvisorRead) {
	read = session.AdvisorRead{Agent: AdvisorAgent, Words: len(words), Plan: plan}
	started := time.Now()
	defer func() { read.DurationMS = time.Since(started).Milliseconds() }()
	failed := func(err error) session.AdvisorRead {
		read.Error = err.Error()
		return read
	}
	if len(words) == 0 {
		return failed(errors.New("the conversation holds none of the person's words, so there is nothing to read the plan against"))
	}
	definition, err := t.SubAgents.Named(AdvisorAgent)
	if err != nil {
		return failed(err)
	}
	opened, err := t.open(definition, "")
	if err != nil {
		return failed(err)
	}
	if opened.Close != nil {
		defer opened.Close()
	}
	read.Model = opened.Slug
	model, accounts := t.base.Model, t.base.Accounts
	if opened.Accounts.Pick != nil {
		accounts = opened.Accounts
	}
	if accounts.Pick != nil {
		account, err := accounts.Pick(ctx)
		if err != nil {
			return failed(err)
		}
		if account.Model != nil {
			model = account.Model
		}
	}
	if model == nil {
		return failed(errors.New("no model opened for the advisor"))
	}
	brief := "the person's words, oldest first:\n\n" + strings.Join(words, "\n\n") + "\n\nthe plan the lead set:\n\n" + plan
	within, stop := context.WithTimeout(ctx, konst.SubAgentAskMillis*time.Millisecond)
	defer stop()
	request := llm.Request{Turn: turnID, Messages: []llm.Message{
		{Role: llm.RoleSystem, Content: definition.Instructions},
		{Role: llm.RoleUser, Content: brief, Origin: llm.Origin{Source: llm.SourceBrief, Name: t.orchestratorID}},
	}}
	tapped, tap := llm.Tapped(within)
	asked := time.Now()
	decision, err := model.Ask(tapped, request)
	read.Model, read.CostUSD = cmp.Or(decision.Build, read.Model), decision.Usage.Cost
	recorded, id, why := &record{log: log, scope: turnID, turn: turnID, agent: AdvisorAgent, said: map[string]string{}, lastSent: &messagesKept{}}, session.NewEventID(), "reads the plan the lead set"
	preview, previewErr := recall.LoadConfig()
	recorded.note(previewErr)
	recorded.sent(id, why, request, preview.OnWire(t.base.Wire))
	recorded.exchange(id, why, t.base.Wire, request, asked, tap.Attempts(), decision, err)
	if err == nil {
		recorded.step(session.EventRequest, stepFrom(id, 0, requestTiming{attempt: len(tap.Attempts()), durationMS: time.Since(asked).Milliseconds()}, decision))
	}
	if len(recorded.failed) > 0 && t.base.Notify != nil {
		t.base.Notify("the advisor's request was not wholly recorded: " + strings.Join(recorded.failed, "; "))
	}
	if err != nil {
		return failed(err)
	}
	if decision.Outcome != llm.OutcomeMessage {
		return failed(fmt.Errorf("the advisor's reply ended as %s, stop reason %q, with no answer", decision.Outcome, decision.Stop))
	}
	named := false
	for line := range strings.SplitSeq(decision.Content, "\n") {
		label, finding, found := strings.Cut(strings.TrimSpace(line), ":")
		label, finding = strings.ToLower(strings.Trim(label, "-* ")), strings.TrimSpace(finding)
		if !found || label != "missing" && label != "extra" {
			continue
		}
		named = true
		switch {
		case finding == "" || strings.EqualFold(strings.TrimRight(finding, "."), "none"):
		case label == "missing":
			read.Missing = append(read.Missing, finding)
		default:
			read.Extra = append(read.Extra, finding)
		}
	}
	if !named {
		return failed(fmt.Errorf("the advisor's reply has no Missing or Extra line: %q", strings.TrimSpace(decision.Content)))
	}
	return read
}
