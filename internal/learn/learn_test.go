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

const day = 24 * 60

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
		if len(s.lines) > 0 {
			header.At = start().Add(time.Duration(s.lines[0].minute) * time.Minute)
		}
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

func scanWith(t *testing.T, store *session.Store, handle string, known Known) Run {
	t.Helper()
	headers, err := Chain(store, handle)
	if err != nil {
		t.Fatal(err)
	}
	run, err := Scan([]Source{{Store: store, Headers: headers}}, known, start())
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func scanOf(t *testing.T, store *session.Store, handle string, known ...memory.Entry) Run {
	t.Helper()
	return scanWith(t, store, handle, Known{Memory: known})
}

func TestTheFamilyNameAloneReadsEveryGenerationAndAGenerationsNameReadsUpToIt(t *testing.T) {
	store := writeChain(t,
		recorded{id: "sample-family", lines: []line{lead(1, "ok"), said(2, typed, "first message")}},
		recorded{id: "sample-middle", parent: "sample-family", lines: []line{lead(3, "ok"), said(4, typed, "second message")}},
		recorded{id: "sample-last", parent: "sample-middle", lines: []line{lead(5, "ok"), said(6, typed, "third message")}},
	)
	for handle, want := range map[string]int{"sample-family": 3, "sample-middle": 2, "sample-last": 3} {
		run := scanOf(t, store, handle)
		if len(run.Read.Sessions) != want || run.Read.Typed != want {
			t.Errorf("%s read %d sessions and %d messages, want %d of each", handle, len(run.Read.Sessions), run.Read.Typed, want)
		}
	}
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

func personal(run Run) (Finding, bool) {
	for _, f := range run.Findings {
		if f.Class == ClassPersonal {
			return f, true
		}
	}
	return Finding{}, false
}

func TestARecurrenceIsCheckedInTheRequestTheModelAnsweredAndAMissingOneIsATofuDefect(t *testing.T) {
	first := "deploy the preview yourself when it compiles, then let me check the fixes"
	second := "why is the preview not deploying? deploying it when it compiles is your job lol"
	for _, c := range []struct {
		name    string
		carry   string
		present bool
	}{
		{"the words were lost at the fork", "fact: bash cargo build", false},
		{"the words were carried and ignored", "the person said:\ndeploy the preview  yourself when it compiles,\nthen let me check the fixes", true},
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
					request(16),
				}},
			)
			run := scanOf(t, store, "s2")
			found, ok := personal(run)
			if !ok {
				t.Fatalf("no personal rule from a correction said in two sessions: %+v", run.Findings)
			}
			if len(found.Checks) != 1 || found.Checks[0].Request != "s2-request-12" || found.Checks[0].Present != c.present {
				t.Fatalf("the checks are %+v, want one, in s2-request-12, the request before the second message, present %v", found.Checks, c.present)
			}
			if found.Repeats != 1 || found.Calls != 2 || run.Summary.Repeats != 1 || run.Summary.Calls != 2 {
				t.Errorf("repeats %d and calls %d, summary %+v, want the one repeat and the two lead calls that answered it", found.Repeats, found.Calls, run.Summary)
			}
			defects := 0
			for _, f := range run.Findings {
				if f.Class == ClassDefect && f.Target() == TargetUpstream {
					defects++
				}
			}
			if defects != map[bool]int{false: 1, true: 0}[c.present] {
				t.Errorf("%d tofu defects, want one only when the words were missing from the request", defects)
			}
		})
	}
}

func TestALongMessageCarriedCutIsStillFoundInTheRequest(t *testing.T) {
	first := "deploy the preview yourself when it compiles. " + strings.Repeat("the header row needs the old spacing back and the sidebar keeps its width. ", 8)
	store := writeChain(t,
		recorded{id: "s1", lines: []line{lead(1, "Deploy it."), said(2, typed, first)}},
		recorded{id: "s2", parent: "s1", lines: []line{
			said(10, "fork carry", "the person said:\n"+first[:300]+" ..."),
			lead(11, "Open it when you can."),
			request(12),
			said(14, typed, "why is the preview not deploying again? deploying it is your job"),
		}},
	)
	run := scanWith(t, store, "s2", Known{})
	reply := `{"findings": [{"messages": [0, 1], "title": "The preview was not deployed when it compiled", "rule": "", "class": "personal", "reason": "a habit"}]}`
	if err := run.Group(reply, Known{}, "sample-sub/sample-model"); err != nil {
		t.Fatal(err)
	}
	for _, f := range run.All() {
		if f.Class == ClassDefect || f.Present() != len(f.Checks) {
			t.Errorf("finding %s has %d of %d checks present, want the carried start of the message found", f.Title, f.Present(), len(f.Checks))
		}
	}
}

func TestOneSessionIsOnlyWatchedAndAProjectRemarkIsNeverProposed(t *testing.T) {
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
	if len(run.Findings) != 0 {
		t.Errorf("findings %+v from a correction in one session and a remark about the project, want none", run.Findings)
	}
	if len(run.Watching) != 1 || len(run.Project) != 1 || run.Project[0].Class != ClassProject {
		t.Errorf("watching %d and project %+v, want the one-session correction watched and the hover remark about the project", len(run.Watching), run.Project)
	}
}

func TestLocalGroupingNeedsThreeSharedContentWordsAndFillerIsNotOne(t *testing.T) {
	store := writeChain(t,
		recorded{id: "s1", lines: []line{lead(1, "Done."), said(2, typed, "wtf, the picture from my last message is gone again lol")}},
		recorded{id: "s2", parent: "s1", lines: []line{lead(3, "Done."), said(4, typed, "wtf is this picture? the last tooltip slides again lol")}},
	)
	run := scanOf(t, store, "s2")
	if len(run.Findings) != 0 || len(run.Held) != 0 {
		t.Errorf("findings %+v from two messages that share only filler and two content words, want none", run.Findings)
	}
	if run.Mode != ModeLocal {
		t.Errorf("mode %q, want the local mode said", run.Mode)
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
	if len(run.Findings) != ProposalCap || len(run.Held) == 0 {
		t.Errorf("%d findings and %d held back, want the cap of %d and the rest held", len(run.Findings), len(run.Held), ProposalCap)
	}
	if len(run.Summary.Do) == 0 || !strings.Contains(run.Summary.Do[0], "tofu learn apply "+fmt.Sprint(run.Findings[0].ID)) {
		t.Errorf("the summary says to do %q, want the first finding's command first", run.Summary.Do)
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
	for i, f := range run.Findings {
		if f.Target() == TargetUpstream {
			at = i
		}
	}
	if at < 0 {
		t.Fatalf("no upstream finding: %+v", run.Findings)
	}
	key := []byte("a key only this install holds")
	draft := DraftOf(run.Findings[at], Build{Version: "0.5.5", Commit: "c5322ca8", Platform: "windows/amd64"}, start())
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
	for _, f := range append(run.Findings, run.Held...) {
		if f.Target() == TargetRule {
			if f.Retire != "m4" || f.Scope != memory.Global {
				t.Errorf("the rule finding retires %q in %s, want m4 in the global scope", f.Retire, f.Scope)
			}
			return
		}
	}
	t.Errorf("no rule proposed for an entry needed again in two later sessions: %+v", run.Findings)
}

func TestOnlyACorrectionOfTheAskCountsAsATurnThatEndedAskingAndARuleIsNotAQuote(t *testing.T) {
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
	for _, f := range run.All() {
		if f.Mechanism == leadTurnEnd {
			for _, q := range f.Quotes {
				turnEnd = append(turnEnd, q.Text)
			}
			if f.Class != ClassLibrary {
				t.Errorf("the turn-end finding is classed %s, want a library rule", f.Class)
			}
		}
	}
	if strings.Join(turnEnd, "|") != "why waiting? lol|why you stopped? nothing to wait on lol." {
		t.Errorf("the turn-end finding quotes %q, want only the two corrections of a turn that stopped to ask", turnEnd)
	}
	found, ok := personal(run)
	if !ok || !strings.Contains(found.Said, "deploying") {
		t.Fatalf("no personal rule for the deploy correction: %+v", run.Findings)
	}
	if strings.Contains(found.Rule, "lol") || strings.Contains(found.Rule, found.Said) || !strings.Contains(strings.ToLower(found.Rule), "deploy the preview itself when it compiles") {
		t.Errorf("the rule is %q, want the instruction itself, not the quote", found.Rule)
	}
}

const sampleChangelog = `# Changelog

## Unreleased

## 0.2.0 - 2026-10-08

### Fixed

- **Pictures typed while sub-agents run reach the model.** Before, they were dropped.

- **Your messages survive every fork word for word**, so a correction made before a fork still holds after it.

## 0.1.0 - 2026-10-01

### Added

- **A first release.**
`

func modelChain(t *testing.T) *session.Store {
	return writeChain(t,
		recorded{id: "s1", lines: []line{
			lead(1, "The build is done, open it when you can."),
			said(2, typed, "run the build yourself and open it for me when it is finished"),
			lead(3, "Here is the screenshot."),
			said(4, typed, "the picture i pasted while the agents ran never arrived, wtf [Image #3]"),
		}},
		recorded{id: "s2", parent: "s1", lines: []line{
			lead(day+1, "Finished. You can run it now."),
			said(day+2, typed, "why is it not running? running finished builds is your job lol"),
			lead(day+3, "I see no picture."),
			said(day+4, typed, "my picture is missing again, typed it while agents worked"),
			said(day+5, typed, "the sidebar border is too loud"),
		}},
		recorded{id: "s3", parent: "s2", lines: []line{
			lead(2*day+1, "Done."),
			said(2*day+2, typed, "the sidebar border is still too loud"),
		}},
	)
}

func knownForModel() Known {
	return Known{Releases: Releases(sampleChangelog), Settings: map[string]string{"verifySubAgents": "the lead checks each sub-agent's work"}}
}

func TestTheModelsGroupsAreCheckedFieldByFieldBeforeTheyAreBelieved(t *testing.T) {
	run := scanWith(t, modelChain(t), "s3", knownForModel())
	if !strings.Contains(run.Prompt(knownForModel()), "0.2.0") {
		t.Fatalf("the prompt offers no release note")
	}
	reply := "Here you go:\n" + `{"findings": [
	 {"messages": [0, 2], "title": "The lead hands back a finished build instead of running it", "rule": "run the build yourself and open it for me when it is finished", "class": "personal", "reason": "a habit of the lead"},
	 {"messages": [1, 3, 99, 1], "title": "Pictures typed while sub-agents ran never reached the model", "rule": "", "class": "defect", "reason": "tofu dropped the attachment", "fixed_in": "9.9.9"},
	 {"messages": [2], "title": "Already used", "rule": "Run finished builds.", "class": "personal", "reason": "repeat"},
	 {"messages": [4, 5], "title": "Sidebar border", "rule": "", "class": "bug", "reason": "not a class"},
	 {"messages": [4, 5], "title": "The sidebar border is too strong", "rule": "", "class": "setting", "reason": "a setting", "setting": "sidebarLoudness", "value": "low"},
	 {"messages": [4, 5], "title": "The sidebar border is too strong", "rule": "", "class": "project", "reason": "about the desk app"}
	]}` + "\nthat is all"
	if err := run.Group(reply, knownForModel(), "sample-sub/sample-model"); err != nil {
		t.Fatal(err)
	}
	if run.Mode != ModeModel || run.Model != "sample-sub/sample-model" {
		t.Errorf("mode %q by %q, want the model mode and the slug that grouped", run.Mode, run.Model)
	}
	var habit, defect Finding
	for _, f := range run.All() {
		switch f.Title {
		case "The lead hands back a finished build instead of running it":
			habit = f
		case "Pictures typed while sub-agents ran never reached the model":
			defect = f
		case "Already used", "Sidebar border":
			t.Errorf("a group that should have been dropped was kept: %+v", f)
		}
	}
	if habit.Times != 2 || habit.Sessions != 2 || habit.Class != ClassPersonal {
		t.Errorf("the habit is %+v, want two messages in two sessions, a personal rule", habit)
	}
	if habit.Rule != "" {
		t.Errorf("the rule %q copies the person's message, want it refused", habit.Rule)
	}
	if defect.Times != 2 || defect.Target() != TargetUpstream || defect.FixedIn != "" {
		t.Errorf("the defect is %+v, want the two real messages, an upstream draft and no version that was never offered", defect)
	}
	if len(run.Project) != 1 || run.Project[0].Class != ClassProject {
		t.Errorf("project findings %+v, want the sidebar remark there and nowhere else", run.Project)
	}
	if run.Sent.Dropped < 5 {
		t.Errorf("%d fields dropped (%q), want the out-of-range number, the repeat, the used group, the unknown class, the unknown setting and the copied rule", run.Sent.Dropped, run.Sent.Refused)
	}
}

func TestARuleIsOnePlainInstructionThatNamesNoOneAndCopiesNoQuote(t *testing.T) {
	quotes := []Quote{{Text: "why is it not running? running finished builds is your job lol"}}
	for rule, want := range map[string]bool{
		"Run a finished build and open it without being asked.": true,
		"The lead runs what it built and opens it itself.":      true,
		"Run it, wtf.":                                            false,
		"Look at [Image #21] first.":                              false,
		"Running finished builds is your job.":                    false,
		"Sure! Here it is:\nThe lead runs what it built.":         false,
		"The person wants the lead to run what it built.":         false,
		"Tell the user what changed.":                             false,
		"Marta wants the lead to run what it built.":              false,
		"The lead " + strings.Repeat("keeps working, ", 20) + ".": false,
	} {
		if fault := ruleFault(rule, quotes); (fault == "") != want {
			t.Errorf("ruleFault(%q) is %q, want accepted %v", rule, fault, want)
		}
	}
}

func TestAFindingAFixReleasedLaterCoversIsFixedAndOneSeenAfterTheFixIsNot(t *testing.T) {
	releases := Releases(sampleChangelog)
	if len(releases) != 2 || releases[0].Version != "0.2.0" || releases[0].Day != "2026-10-08" || len(releases[0].Notes) != 2 {
		t.Fatalf("releases %+v, want 0.2.0 then 0.1.0 with the notes of each", releases)
	}
	reply := `{"findings": [{"messages": [1, 3], "title": "Pictures typed while sub-agents ran never reached the model", "rule": "", "class": "defect", "reason": "tofu dropped it", "fixed_in": "0.2.0"}]}`
	for _, c := range []struct {
		name    string
		recurs  int
		fixed   bool
		sameDay bool
	}{
		{"last seen two days before the fix", 0, true, false},
		{"last seen the day of the fix", 2*day + 3, true, true},
		{"seen again after the fix", 3*day + 3, false, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			store := modelChain(t)
			if c.recurs > 0 {
				store = writeChain(t,
					recorded{id: "s1", lines: []line{lead(1, "Here."), said(2, typed, "run the build yourself"), lead(3, "Here."), said(4, typed, "the picture i pasted while agents ran never arrived")}},
					recorded{id: "s2", parent: "s1", lines: []line{lead(c.recurs-2, "Done."), said(c.recurs-1, typed, "why is it not running"), lead(c.recurs, "I see no picture."), said(c.recurs+1, typed, "my picture is missing again")}},
				)
			}
			run := scanWith(t, store, "s2", knownForModel())
			if err := run.Group(reply, knownForModel(), "sample-sub/sample-model"); err != nil {
				t.Fatal(err)
			}
			fixed := len(run.Fixed) == 1 && run.Fixed[0].FixedIn == "0.2.0"
			if fixed != c.fixed || (fixed && run.Fixed[0].FixedSameDay != c.sameDay) {
				t.Errorf("fixed %+v, want fixed %v the same day %v", run.Fixed, c.fixed, c.sameDay)
			}
			if c.fixed == (len(run.Findings) > 0) {
				t.Errorf("findings %+v, want a fixed finding never proposed and an unfixed one proposed", run.Findings)
			}
		})
	}
}

func TestLostWordsAreMarkedFixedByTheReleaseThatCarriesThemThroughAFork(t *testing.T) {
	first := "deploy the preview yourself when it compiles, then let me check the fixes"
	second := "why is the preview not deploying? deploying it when it compiles is your job lol"
	store := writeChain(t,
		recorded{id: "s1", lines: []line{lead(1, "Deploy it yourself."), said(2, typed, first)}},
		recorded{id: "s2", parent: "s1", lines: []line{lead(11, "Open it."), request(12), said(14, typed, second)}},
	)
	run := scanWith(t, store, "s2", Known{Releases: Releases(sampleChangelog)})
	for _, f := range run.Fixed {
		if f.Mechanism == forkCarry && f.FixedIn == "0.2.0" {
			return
		}
	}
	t.Errorf("fixed %+v and findings %+v, want the fork carry fixed in 0.2.0", run.Fixed, run.Findings)
}

func TestADefectDraftFromTheModelIsABugAndCarriesNoQuote(t *testing.T) {
	run := scanWith(t, modelChain(t), "s3", knownForModel())
	reply := `{"findings": [{"messages": [1, 3], "title": "Pictures typed while sub-agents ran never reached the model", "rule": "", "class": "defect", "reason": "tofu dropped the attachment"}]}`
	if err := run.Group(reply, Known{}, "sample-sub/sample-model"); err != nil {
		t.Fatal(err)
	}
	if len(run.Findings) != 1 {
		t.Fatalf("findings %+v, want the one defect", run.Findings)
	}
	text := DraftOf(run.Findings[0], Build{Version: "0.1.0"}, start()).Encode([]byte("k"))
	for _, leak := range []string{"wtf", "never arrived", "missing again", "s1", "s2"} {
		if strings.Contains(text, leak) {
			t.Errorf("the draft carries %q:\n%s", leak, text)
		}
	}
	if !strings.Contains(text, "kind: bug") || !strings.Contains(text, "Pictures typed while sub-agents ran never reached the model") {
		t.Errorf("the draft is not a bug report with the finding's title:\n%s", text)
	}
}
