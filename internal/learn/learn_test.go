package learn

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"tofu/internal/memory"
	"tofu/internal/session"
)

const wrapper = "<env>\nworking directory: F:\\work\nplatform: windows/amd64\n</env>\n\ninstructions from this project's CLAUDE.md, which outrank anything above them that disagrees:\n# Rules\n\nNever mark work done on an agent's word.\n\n"

const ruleNote = "[code_rules, from the rule e2e_first]\nprove a feature end to end, not in pieces.\n\n"

func start() time.Time { return time.Date(2026, 10, 6, 18, 0, 0, 0, time.UTC) }

type line struct {
	minute  int
	origin  string
	text    string
	agent   string
	untimed bool
}

type recorded struct {
	id     string
	parent string
	lines  []line
}

func writeChain(t *testing.T, chain ...recorded) *session.Store {
	t.Helper()
	store := session.NewStore(t.TempDir())
	for _, s := range chain {
		name := s.id
		header := session.Header{ID: s.id, Name: &name, At: start()}
		if s.parent != "" {
			header.CarriedFrom = &session.Carried{Session: s.parent}
		}
		log, err := store.Open(header)
		if err != nil {
			t.Fatal(err)
		}
		var hashes []string
		for _, l := range s.lines {
			at := start().Add(time.Duration(l.minute) * time.Minute)
			body := session.MessageBody{Role: session.RoleUser, Content: l.text, Origin: l.origin}
			if l.origin == "" {
				body = session.MessageBody{Role: session.RoleAssistant, Content: l.text}
			}
			if !l.untimed && l.origin != "" {
				body.TakenAt = &at
			}
			if l.origin == "request" {
				exchange := session.Exchange{Request: fmt.Sprintf("%s-request-%d", s.id, l.minute), At: at, Messages: append([]string(nil), hashes...)}
				if err := log.Exchanged(exchange); err != nil {
					t.Fatal(err)
				}
				continue
			}
			if _, err := log.Append(session.Event{Kind: session.EventMessage, At: at, Agent: l.agent}, body); err != nil {
				t.Fatal(err)
			}
			hash, err := log.Keep(body)
			if err != nil {
				t.Fatal(err)
			}
			hashes = append(hashes, hash)
		}
		if err := log.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return store
}

const (
	typed  = "typed by the person"
	steer  = "steer"
	task   = "task"
	report = "sub-agent report"
)

func lead(minute int, text string) line { return line{minute: minute, text: text} }

func request(minute int) line { return line{minute: minute, origin: "request"} }

func said(minute int, origin, text string) line {
	return line{minute: minute, origin: origin, text: text}
}

func scanOf(t *testing.T, store *session.Store, handle string, known ...memory.Entry) Run {
	t.Helper()
	headers, err := Chain(store, handle)
	if err != nil {
		t.Fatal(err)
	}
	run, err := Scan([]Source{{Store: store, Headers: headers}}, Known{Memory: known}, start())
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func TestTheWordsAreThePersonsOnlyOncePerMessageAndNeverAScheduleOrAReport(t *testing.T) {
	cron := wrapper + "Run the loop top to bottom. The owner is away."
	first := "can't i just launch one script to check it all?"
	second := "why is nothing launching? its your job when builds end to handle this lol"
	store := writeChain(t,
		recorded{id: "s1", lines: []line{
			lead(1, "Here are the commands for you to run."),
			said(2, typed, wrapper+ruleNote+first),
			said(3, report, wrapper+"rust-dev-1 ran as rust-dev\n\nsub-agent rust-dev-1 is finished"),
			said(4, task, cron),
			said(5, task, cron),
			{minute: 6, origin: task, text: "do the sub-agent's own task", agent: "rust-dev-1"},
		}},
		recorded{id: "s2", parent: "s1", lines: []line{
			said(2, typed, wrapper+ruleNote+first),
			said(7, "fork task", wrapper+"rust-dev-1 ran as rust-dev"),
			lead(8, "The build is done. You can open it now."),
			{minute: 9, origin: typed, text: wrapper + "[code_rules, from the rule review_diff]\nread the diff before you report.\nthen say what changed.\n\n" + second, untimed: true},
		}},
	)

	run := scanOf(t, store, "s2")

	var got []string
	for _, s := range run.Said {
		got = append(got, s.Text)
	}
	if strings.Join(got, "|") != first+"|"+second {
		t.Fatalf("the person's words read as %q, want exactly the two typed messages without the environment, the rule note, the cron prompt, the report, the fork copy or the sub-agent's task", got)
	}
	if run.Said[0].Session != "s1" || run.Said[1].Session != "s2" {
		t.Errorf("the messages are placed in %s and %s, want the session each was first typed in", run.Said[0].Session, run.Said[1].Session)
	}
	if run.Read.Repeated != 2 {
		t.Errorf("%d repeated prompts counted, want the two cron fires set aside and counted", run.Read.Repeated)
	}
	if !strings.Contains(run.Said[1].Before, "You can open it now") {
		t.Errorf("the lead's turn before the second message is %q", run.Said[1].Before)
	}
}

func TestARecurrenceIsCheckedInTheRequestTheModelAnsweredAndBucketedByWhatItHeld(t *testing.T) {
	first := "deploy the preview yourself when it compiles, then let me check the fixes"
	second := "why is the preview not deploying? deploying it when it compiles is your job lol"
	for _, c := range []struct {
		name    string
		carry   string
		present bool
		bucket  Bucket
	}{
		{"the words were lost at the fork", "fact: bash cargo build", false, BucketUpstream},
		{"the words were carried and ignored", "the person said:\ndeploy the preview  yourself when it compiles,\nthen let me check the fixes", true, BucketApply},
	} {
		t.Run(c.name, func(t *testing.T) {
			store := writeChain(t,
				recorded{id: "s1", lines: []line{lead(1, "Run these commands yourself."), said(2, typed, first), lead(3, "I will.")}},
				recorded{id: "s2", parent: "s1", lines: []line{
					said(10, "fork carry", c.carry),
					lead(11, "The build finished. Open it when you can."),
					request(12),
					lead(13, "Done, over to you."),
					said(14, typed, second),
					request(15),
				}},
			)
			run := scanOf(t, store, "s2")
			if len(run.Proposals) == 0 {
				t.Fatalf("no proposal from a correction said in two sessions: %+v", run)
			}
			theme := run.Proposals[0].Themes[0]
			if len(theme.Checks) != 1 || theme.Checks[0].Request != "s2-request-12" || theme.Checks[0].Present != c.present {
				t.Fatalf("the checks are %+v, want one, in s2-request-12, the request before the second message, present %v", theme.Checks, c.present)
			}
			buckets := map[Bucket]bool{}
			for _, p := range run.Proposals {
				buckets[p.Bucket] = true
			}
			if !buckets[c.bucket] || !buckets[BucketApply] {
				t.Errorf("the buckets are %v, want %s, and a memory entry to apply either way", buckets, c.bucket)
			}
			if c.present && buckets[BucketUpstream] {
				t.Errorf("an upstream draft for words that were in the request")
			}
		})
	}
}

func TestOneSessionIsOnlyWatchedAndAProjectDefectIsOnlySeen(t *testing.T) {
	store := writeChain(t,
		recorded{id: "s1", lines: []line{
			lead(1, "Opened it."),
			said(2, typed, "why did you stop again? keep working on the queue lol"),
			lead(3, "Stopped again."),
			said(4, typed, "why did you stop? keep working on the queue, you should not wait"),
			said(5, typed, "the hover on the tab close button flashes"),
		}},
		recorded{id: "s2", parent: "s1", lines: []line{lead(6, "Done."), said(7, typed, "the tab close button hover still flashes")}},
	)
	run := scanOf(t, store, "s2")
	if len(run.Proposals) != 0 {
		t.Errorf("proposals %+v from a correction in one session and a defect in the product, want none", run.Proposals)
	}
	if len(run.Watching) != 1 || len(run.Seen) != 1 {
		t.Errorf("watching %d and seen %d, want the one-session correction watched and the hover defect seen", len(run.Watching), len(run.Seen))
	}
}

func TestAtMostTheCapIsProposedAndTheRestIsHeldBack(t *testing.T) {
	topics := []string{"commit message format", "ticket board update", "binary install path", "changelog entry wording", "golden file update", "sub-agent brief length", "cargo jobs count"}
	var first, second []line
	for i, topic := range topics {
		first = append(first, lead(i*3, "ok"), said(i*3+1, typed, "you should fix the "+topic+" yourself, again"))
		second = append(second, lead(100+i*3, "ok"), said(100+i*3+1, typed, "why is the "+topic+" still wrong? you should fix it yourself"))
	}
	store := writeChain(t, recorded{id: "s1", lines: first}, recorded{id: "s2", parent: "s1", lines: second})
	run := scanOf(t, store, "s2")
	if len(run.Proposals) != ProposalCap || len(run.Held) == 0 {
		t.Errorf("%d proposals and %d held back, want the cap of %d and the rest held", len(run.Proposals), len(run.Held), ProposalCap)
	}
}

func TestAnUpstreamDraftSaysWhatHappenedWithoutHisWordsAndCarriesAMarkThatBreaksOnAnEdit(t *testing.T) {
	first := "deploy the preview yourself when it compiles, then let me check the fixes"
	second := "why is the preview not deploying? deploying it when it compiles is your job lol"
	store := writeChain(t,
		recorded{id: "sample-one", lines: []line{lead(1, "Deploy it yourself."), said(2, typed, first)}},
		recorded{id: "sample-two", parent: "sample-one", lines: []line{lead(11, "Open it."), request(12), said(14, typed, second)}},
	)
	run := scanOf(t, store, "sample-two")
	at := -1
	for i, p := range run.Proposals {
		if p.Bucket == BucketUpstream {
			at = i
		}
	}
	if at < 0 {
		t.Fatalf("no upstream proposal: %+v", run.Proposals)
	}
	key := []byte("a key only this install holds")
	draft := DraftOf(run.Proposals[at], Build{Version: "0.5.5", Commit: "c5322ca8", Platform: "windows/amd64"}, start())
	text := draft.Encode(key)
	for _, leak := range []string{first, second, "sample-one", "sample-two", "compiles is your job"} {
		if strings.Contains(text, leak) {
			t.Errorf("the draft carries %q:\n%s", leak, text)
		}
	}
	for _, field := range []string{"format: tofu-learn-upstream", "format_version: 1", "kind: failure", "mark: "} {
		if !strings.Contains(text, field) {
			t.Errorf("the draft has no %q:\n%s", field, text)
		}
	}
	if _, ok := ReadDraft([]byte(text), key); !ok {
		t.Errorf("the mark does not verify on the draft as written")
	}
	if _, ok := ReadDraft([]byte(strings.Replace(text, "sessions: 2", "sessions: 9", 1)), key); ok {
		t.Errorf("the mark still verifies after the session count was edited")
	}
}

func TestAnEntryNeededAgainInLaterSessionsIsProposedAsARuleThatRetiresIt(t *testing.T) {
	first := "you should decide the order of the tickets yourself"
	second := "why ask me? you should decide the order of tickets yourself, again"
	store := writeChain(t,
		recorded{id: "s1", lines: []line{lead(1, "Which ticket first?"), said(2, typed, first)}},
		recorded{id: "s2", parent: "s1", lines: []line{lead(5, "Your call on the order."), request(6), said(7, typed, second)}},
		recorded{id: "s3", parent: "s2", lines: []line{lead(9, "Which ticket should go first?"), request(10), said(11, typed, "again, decide the order of the tickets yourself")}},
	)
	entry := memory.Entry{ID: "m4", Scope: memory.Global, Kind: memory.KindPerson, Text: "The lead decides the order of the tickets itself.", Said: first, At: start()}
	run := scanOf(t, store, "s3", entry)
	for _, p := range append(run.Proposals, run.Held...) {
		if p.Target == TargetRule {
			if p.Retire != "m4" || p.Scope != memory.Global {
				t.Errorf("the rule proposal retires %q in %s, want m4 in the global scope", p.Retire, p.Scope)
			}
			return
		}
	}
	t.Errorf("no rule proposed for an entry needed again in two later sessions: %+v", run.Proposals)
}

func TestOnlyACorrectionOfTheAskCountsAsATurnThatEndedAskingAndAStatementIsAnInstruction(t *testing.T) {
	store := writeChain(t,
		recorded{id: "s1", lines: []line{
			lead(1, "Decision for you: should the hover fade over 100 ms?"),
			said(2, typed, "the sketches are static because they were drafts lol, but i want gentle motion"),
			lead(3, "Your call: keep the light theme?"),
			said(4, typed, "as i said, the badges are loud"),
			lead(5, "Decision for you: tell me when I can take the machine."),
			said(6, typed, "why waiting? lol"),
		}},
		recorded{id: "s2", parent: "s1", lines: []line{
			lead(7, "Tell me if you'd rather I hold off."),
			said(8, typed, "why you stopped? nothing to wait on lol."),
			lead(9, "Build finished, open it when you can."),
			said(10, typed, "ok, please deploy the preview yourself when it compiles, then let me check"),
		}},
		recorded{id: "s3", parent: "s2", lines: []line{
			lead(11, "Nobody has opened it yet."),
			said(12, typed, "why is the preview not deploying? deploying it when it compiles is your job lol"),
		}},
	)
	run := scanOf(t, store, "s3")
	var turnEnd []string
	for _, theme := range run.Corrections {
		if theme.Mechanism == leadTurnEnd {
			for _, q := range theme.Quotes {
				turnEnd = append(turnEnd, q.Text)
			}
		}
	}
	if strings.Join(turnEnd, "|") != "why waiting? lol|why you stopped? nothing to wait on lol." {
		t.Errorf("the turn-end theme quotes %q, want only the two corrections of a turn that stopped to ask", turnEnd)
	}
	for _, p := range run.Proposals {
		if p.Target == TargetMemory && strings.Contains(p.Said, "deploying") {
			if strings.Contains(p.Text, "lol") || strings.Contains(p.Text, p.Said) || !strings.Contains(p.Text, "deploy the preview itself when it compiles") {
				t.Errorf("the statement is %q, want the instruction itself, not the quote", p.Text)
			}
			return
		}
	}
	t.Errorf("no memory proposal for the run theme: %+v", run.Proposals)
}

func TestAStatementFromTheModelIsOneInstructionOrNothing(t *testing.T) {
	for reply, want := range map[string]bool{
		"The lead runs what it built and opens it itself.":        true,
		"\"The lead runs what it built.\"":                        true,
		"Sure! Here it is:\nThe lead runs what it built.":         false,
		"The person wants the lead to run what it built.":         false,
		"Marta wants the lead to run what it built.":              false,
		"The lead " + strings.Repeat("keeps working, ", 20) + ".": false,
		"Run what was built and open it, without asking first.":   true,
	} {
		if _, ok := Statement(reply); ok != want {
			t.Errorf("Statement(%q) accepted %v, want %v", reply, ok, want)
		}
	}
}
