package boardy

import (
	"os"
	"strings"
	"testing"
)

func sample() Question {
	return Question{
		TLDR:      "Keep answers in one file per board?",
		ELI5:      "Like one shared notepad for the whole team.\nYou write under each question and the agent reads it.",
		Options:   []string{"One file per board", "One file per ticket"},
		Recommend: 1,
		Why:       "one place to read, and the desk shows one list",
	}
}

func TestQuestionsRefuseAnIncompleteAsk(t *testing.T) {
	m := managedBoard(t, "DEMO")
	ticket := holding(t, m, "DEMO", "x", Doing)
	_, err := m.Move(ticket.ID, NeedsDecision, "", manager)
	refused(t, err, "no open question")

	broken := map[string]func(*Question){
		"no tldr":                       func(q *Question) { q.TLDR = "" },
		"a tldr over two lines":         func(q *Question) { q.TLDR = "a\nb" },
		"no eli5":                       func(q *Question) { q.ELI5 = " " },
		"an eli5 of four lines":         func(q *Question) { q.ELI5 = "a\nb\nc\nd" },
		"an eli5 line read as option":   func(q *Question) { q.ELI5 = "first\n2. then this" },
		"an eli5 line read as advice":   func(q *Question) { q.ELI5 = "Recommend 1: sneaky" },
		"one option":                    func(q *Question) { q.Options = q.Options[:1] },
		"five options":                  func(q *Question) { q.Options = []string{"a", "b", "c", "d", "e"} },
		"an empty option":               func(q *Question) { q.Options[1] = "" },
		"a recommendation out of range": func(q *Question) { q.Recommend = 3 },
		"no recommendation":             func(q *Question) { q.Recommend = 0 },
		"no why":                        func(q *Question) { q.Why = "" },
	}
	for name, breaks := range broken {
		q := sample()
		breaks(&q)
		if _, err := m.Ask(ticket.ID, q, manager); err == nil {
			t.Errorf("%s was asked", name)
		}
	}
	if _, err := m.Ask(ticket.ID, sample(), agent); err == nil {
		t.Error("an agent asked on a ticket assigned to someone else")
	}
	if _, err := m.Ask(ticket.ID, sample(), manager); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Move(ticket.ID, NeedsDecision, "", manager); err != nil {
		t.Fatalf("a complete question reaches needs-decision: %v", err)
	}
}

func TestQuestionsRoundTripAndStayOutOfAnOldTicket(t *testing.T) {
	ticket := ticketWith("x")
	ticket.ID, ticket.Type, ticket.Status, ticket.Priority = "DEMO-1", Task, Doing, DefaultPriority
	if strings.Contains(string(ticket.Markdown()), "Questions") {
		t.Fatalf("a ticket with no question writes no Questions section, so an older tofu still reads it:\n%s", ticket.Markdown())
	}
	first, second := sample(), sample()
	first.ID, second.ID = 1, 3
	second.Options = append(second.Options, "Both, one feeding the other")
	second.Recommend = 3
	ticket.Questions = []Question{first, second}
	parsed, err := ParseTicket(ticket.Markdown())
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Questions) != 2 || parsed.Questions[0].ELI5 != first.ELI5 || parsed.Questions[1].Recommend != 3 || parsed.Questions[1].Options[2] != second.Options[2] || parsed.Questions[1].ID != 3 || parsed.Questions[0].Why != first.Why {
		t.Fatalf("questions changed on a round trip:\n%+v\n%s", parsed.Questions, ticket.Markdown())
	}
	if parsed.Log != ticket.Log || parsed.Acceptance[0] != ticket.Acceptance[0] {
		t.Fatal("the Questions section ate a neighbour")
	}
}

func TestCatchupAppliesAnAnswerOnceAndKeepsAnUnreadOne(t *testing.T) {
	m := managedBoard(t, "DEMO")
	review := holding(t, m, "DEMO", "from review", Review)
	doing := holding(t, m, "DEMO", "from doing", Doing)
	for _, ticket := range []Ticket{review, doing} {
		if _, err := m.Ask(ticket.ID, sample(), manager); err != nil {
			t.Fatal(err)
		}
	}
	reworded := sample()
	reworded.TLDR = "A second thing to settle?"
	if _, err := m.Ask(doing.ID, reworded, manager); err != nil {
		t.Fatal(err)
	}
	for _, ticket := range []Ticket{review, doing} {
		if _, err := m.Move(ticket.ID, NeedsDecision, "", manager); err != nil {
			t.Fatal(err)
		}
	}

	listed, err := m.Catchup("DEMO")
	if err != nil || len(listed.Open) != 3 {
		t.Fatalf("want 3 open questions, got %d: %v", len(listed.Open), err)
	}
	raw, err := os.ReadFile(listed.File)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	text = strings.Replace(text, "answer:\n", "answer: 2\n", 1)
	text = strings.Replace(text, "answer:\n", "answer:\nneither, keep both in the desk\n", 2)
	if err := os.WriteFile(listed.File, []byte(strings.ReplaceAll(text, "\n", "\r\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	again, err := m.Catchup("DEMO")
	if err != nil || again.Open[0].Answer != "2" || again.Open[2].Answer == "" {
		t.Fatalf("listing again wiped a typed answer: %+v %v", again.Open, err)
	}
	raw, _ = os.ReadFile(again.File)
	if err := os.WriteFile(again.File, []byte(strings.Replace(string(raw), "Q2: A second thing to settle?", "Q2: A question somebody rewrote", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Answers("DEMO", agent); err == nil {
		t.Error("an agent applied the person's answers")
	}

	applied, err := m.Answers("DEMO", manager)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied.Applied) != 2 || len(applied.Stale) != 1 || len(applied.Open) != 1 {
		t.Fatalf("want 2 applied, 1 stale, 1 open, got %+v", applied)
	}
	back, _ := m.Store.Get(review.ID)
	if back.Status != Review || len(back.OpenQuestions()) != 0 || len(back.Questions) != 1 || back.Questions[0].Answer != "option 2, One file per ticket" || !strings.Contains(back.Log, "One file per ticket") {
		t.Fatalf("an answered ticket goes back to where it was with the answer kept on its question and logged, got %s %+v\n%s", back.Status, back.Questions, back.Log)
	}
	still, _ := m.Store.Get(doing.ID)
	if still.Status != NeedsDecision || len(still.OpenQuestions()) != 1 || !strings.Contains(still.Log, "keep both in the desk") {
		t.Fatalf("a ticket with a question left stays, got %s %+v\n%s", still.Status, still.Questions, still.Log)
	}

	second, err := m.Answers("DEMO", manager)
	if err != nil || len(second.Applied) != 0 || len(second.Stale) != 0 {
		t.Fatalf("a second read finds nothing, got %+v %v", second, err)
	}
}
