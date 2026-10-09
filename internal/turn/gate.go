package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/internal/judge/ledger"
	"tofu/internal/konst"
	"tofu/internal/session"
	"tofu/internal/subagent"
)

type GateRequest struct {
	TurnID string
	Task   string
	Tool   string
	Args   json.RawMessage
	Call   string
}

type GateDecision struct {
	ID         string
	Verdict    ledger.Verdict
	Answers    []ledger.Answer
	Reason     *ledger.Reason
	PersonOnly bool
	HookAsk    string
	Failure    string
}

type Gate interface {
	Decide(ctx context.Context, request GateRequest) (GateDecision, error)
}

type GateMode int

const (
	GateShadow GateMode = iota
	GateEnforce
)

func (m GateMode) String() string {
	switch m {
	case GateShadow:
		return "shadow"
	case GateEnforce:
		return "enforce"
	}
	panic("turn: unknown gate mode")
}

type PersonAnswer int

const (
	PersonDenied PersonAnswer = iota
	PersonAllowedOnce
	PersonAlwaysHere
	PersonNotAsked
	PersonNotAskedOwnScratch
)

func (a PersonAnswer) allows() bool {
	switch a {
	case PersonDenied:
		return false
	case PersonAllowedOnce, PersonAlwaysHere, PersonNotAsked, PersonNotAskedOwnScratch:
		return true
	}
	panic("turn: unknown person answer")
}

const OutcomeKindGateAnswer = "gate-answer"

func (a PersonAnswer) Outcome() ledger.Outcome {
	switch a {
	case PersonDenied:
		return ledger.Outcome{Kind: OutcomeKindGateAnswer, Detail: "deny"}
	case PersonAllowedOnce, PersonAlwaysHere:
		return ledger.Outcome{Kind: OutcomeKindGateAnswer, Detail: "allow"}
	case PersonNotAsked, PersonNotAskedOwnScratch:
		panic("turn: a call that ran unasked has no answer of the person's to record")
	}
	panic("turn: unknown person answer")
}

type Person func(ctx context.Context, request GateRequest, decision GateDecision) (PersonAnswer, error)

func (p Person) RunsWhatJevAsks() Person {
	return p.Asking(func() bool { return false })
}

type autoRefusesAFailedGate struct{}

func (autoRefusesAFailedGate) Error() string {
	return "in auto a call the gate could not judge is refused rather than asked"
}

func (p Person) Asking(settingAsks func() bool) Person {
	if p == nil {
		return nil
	}
	return func(ctx context.Context, request GateRequest, decision GateDecision) (PersonAnswer, error) {
		asks, decided := QuestionsBlockFrom(ctx)
		if !decided {
			asks = settingAsks()
		}
		switch {
		case asks || decision.PersonOnly:
		case decision.Failure != "":
			return PersonDenied, autoRefusesAFailedGate{}
		case decision.ID != "":
			return PersonNotAsked, nil
		}
		return p(ctx, request, decision)
	}
}

const (
	allowedInAutoMode   = "gatePrompt auto"
	allowedByThePerson  = "the person"
	allowedByOwnScratch = "the sub-agent's own scratch"
)

const (
	refusedHead = "the tool gate refused this call under an enforced policy: "
	refusedTail = ". nothing ran and nothing changed. find another way to do the task, or say why this call is needed and stop."
)

func gateRefusal(ctx context.Context, person Person, request GateRequest, decision GateDecision, gateErr string) string {
	why := refusedWhy(ctx, person, request, decision, gateErr)
	if why == "" {
		return ""
	}
	return refusedHead + why + refusedTail
}

func refusedWhy(ctx context.Context, person Person, request GateRequest, decision GateDecision, gateErr string) string {
	switch {
	case gateErr != "":
		decision = GateDecision{Verdict: ledger.VerdictAsk, Failure: gateErr}
	case decision.Verdict == ledger.VerdictUnset, decision.Verdict == ledger.VerdictAllow:
		return ""
	case decision.Verdict == ledger.VerdictDeny:
		return "the verdict is deny" + standing(decision.Reason)
	case decision.Verdict != ledger.VerdictAsk:
		panic("turn: unknown verdict " + string(decision.Verdict))
	}
	asked := "the verdict is ask" + standing(decision.Reason)
	if decision.Failure != "" {
		asked = "the gate could not answer, so the call is an ask: " + decision.Failure
	}
	answer, refused := personRefusal(ctx, person, request, decision, asked)
	if refused == "" && decision.Reason != nil {
		decision.Reason.AllowedBy = answerer(ctx)
		switch answer {
		case PersonNotAsked:
			decision.Reason.AllowedBy = allowedInAutoMode
		case PersonNotAskedOwnScratch:
			decision.Reason.AllowedBy = allowedByOwnScratch
		}
	}
	return refused
}

func personRefusal(ctx context.Context, person Person, request GateRequest, decision GateDecision, asked string) (PersonAnswer, string) {
	if person == nil {
		return PersonDenied, asked + ", and no person was available to answer"
	}
	who := answerer(ctx)
	answer, err := person(ctx, request, decision)
	switch {
	case err != nil:
		return answer, asked + ", and " + who + " could not be asked: " + err.Error()
	case answer.allows():
		return answer, ""
	}
	return answer, asked + ", and " + who + " did not allow it"
}

func personOnlyRefusal(ctx context.Context, config Config, request GateRequest) string {
	if config.Gate != nil && config.GateMode == GateEnforce || !PersonOnly(config.Project, request) {
		return ""
	}
	_, refused := personRefusal(ctx, config.Person, request, GateDecision{Verdict: ledger.VerdictAsk, PersonOnly: true},
		"this call did not run: only the person allows a call that changes tofu's settings or a harness file")
	return refused
}

func AsTheLead(ctx context.Context) context.Context {
	return context.WithValue(ctx, subAgentKey{}, "")
}

func answerer(ctx context.Context) string {
	if SubAgentAsking(ctx) != "" {
		return "the orchestrator"
	}
	return allowedByThePerson
}

func (t *SpawnTool) orchestratorAnswers(held *heldSubAgent, site spawnSite) Person {
	return func(ctx context.Context, request GateRequest, decision GateDecision) (PersonAnswer, error) {
		id := held.agent.ID
		if decision.PersonOnly || decision.HookAsk == "" && decision.Verdict != ledger.VerdictAsk {
			return PersonDenied, errors.New("only the person answers this " + request.Tool + " question, and a sub-agent never asks the person")
		}
		if keepsToScratch(held.boundary, request) {
			site.notice(id, id+"'s "+request.Tool+" call keeps to its own scratch folder, so it runs without asking the orchestrator")
			return PersonNotAskedOwnScratch, nil
		}
		because, place, answers := "the gate's verdict is ask"+standing(decision.Reason), CallPlace(request), ""
		switch {
		case decision.HookAsk != "":
			because, place = "a PreToolUse hook asks first: "+decision.HookAsk, ""
		case decision.Failure != "":
			because, place = "the gate could not answer: "+decision.Failure, ""
		}
		if t.Inbox.stood(held, place) {
			site.notice(id, id+"'s "+request.Tool+" call is "+place+", which the orchestrator allowed here for the rest of its run, so it runs without asking again")
			return PersonAllowedOnce, nil
		}
		if place != "" {
			answers = ", or allow_here to allow every call of this kind (" + place + ") from " + id + " until its run ends"
		}
		wait := konst.SubAgentGateAnswerMillis * time.Millisecond
		shown := cutOnRuneBoundary(string(request.Args), konst.GateAskArgsBytes, "\n...(%s of this call cut here: lookup with call "+request.Call+" returns it whole)...\n")
		asked := fmt.Sprintf("sub-agent %s asks to run %s %s, because %s. it waits up to %s of the time you can answer: call message with to %s and answer allow or deny%s. "+
			"the message call alone answers it, and the turn ends after it with nothing to write. with no answer the call is refused.",
			id, request.Tool, shown, because, wait, id, answers)
		t.roster.Reached(id, subagent.WaitingAnswer, "asks to run "+request.Tool)
		defer t.roster.Reached(id, subagent.Working, "")
		site.notice(id, asked)
		started := t.clock()
		allowed, err := t.Inbox.awaitLead(ctx, held, t.Inbox.ask(held, asked), asked, wait)
		if err != nil {
			unanswered := fmt.Errorf("the orchestrator did not answer after %s: %w", t.clock().Sub(started).Round(time.Millisecond), err)
			site.notice(id, id+"'s "+request.Tool+" call is refused: "+unanswered.Error())
			return PersonDenied, unanswered
		}
		said, verdict := "deny", PersonDenied
		if allowed {
			said, verdict = "allow", PersonAllowedOnce
		}
		site.notice(id, "the orchestrator answered "+said+" to "+id+"'s "+request.Tool+" call after "+t.clock().Sub(started).Round(time.Millisecond).String())
		return verdict, nil
	}
}

func keepsToScratch(boundary *subagent.Boundary, request GateRequest) bool {
	var args struct{ Path, Command string }
	if json.Unmarshal(request.Args, &args) != nil {
		return false
	}
	switch request.Tool {
	case "write", "edit":
		return boundary.Scratched(args.Path)
	case bashToolName:
		return boundary.KeepsToScratch(args.Command)
	}
	return false
}

func (s spawnSite) notice(agent, text string) {
	if s.log != nil {
		_, _ = s.log.Append(session.Event{Turn: s.turn, Agent: agent, Kind: session.EventNotice}, session.NoticeBody{Text: text})
	}
}

func standing(reason *ledger.Reason) string {
	if reason == nil || reason.Question == "" {
		return ""
	}
	where := " is over "
	switch {
	case reason.DeadBand:
		where = " is within the dead band of "
	case reason.Value <= reason.Threshold:
		where = " is under "
	}
	return ", " + reason.Question + " " + number(reason.Value) + where + reason.Comparison + " " + number(reason.Threshold)
}

func number(value float64) string {
	return strconv.FormatFloat(value, 'f', 2, 64)
}

const SettingsToolName = "settings"

const callPlaceWords = 2

func CallPlace(request GateRequest) string {
	var args struct{ Path, Command string }
	if json.Unmarshal(request.Args, &args) != nil {
		return request.Tool + " " + string(request.Args)
	}
	switch {
	case args.Path != "":
		return request.Tool + " " + args.Path
	case args.Command != "":
		words, named := make([]string, 0, callPlaceWords), 0
		for _, word := range strings.Fields(args.Command) {
			flag := strings.HasPrefix(word, "-")
			if !flag {
				named++
			}
			if flag || named <= callPlaceWords {
				words = append(words, word)
			}
		}
		return request.Tool + " " + strings.Join(words, " ")
	}
	return request.Tool + " " + string(request.Args)
}

func PersonOnly(project string, request GateRequest) bool {
	var args struct{ Path, Command string }
	if json.Unmarshal(request.Args, &args) != nil {
		return false
	}
	switch request.Tool {
	case SettingsToolName:
		return true
	case "write", "edit":
		return reachesHarnessFile(project, args.Path)
	case bashToolName:
		return shellChangesTheHarness(project, args.Command)
	}
	return false
}

func harnessFiles() []string {
	return []string{
		".tofu/settings.json", ".boji/settings.json",
		".tofu/hooks.json", ".boji/hooks.json",
		".tofu/hooks/trusted.json", ".boji/hooks/trusted.json",
		".claude/settings.json", ".claude/settings.local.json",
		".codex/hooks.json", ".codex/config.toml",
	}
}

func readOnlyPrograms() []string {
	return []string{"cat", "head", "tail", "less", "more", "grep", "egrep", "fgrep", "rg", "ls", "dir", "stat", "wc", "diff",
		"cd", "pushd", "popd", "echo", "printf", "type", "file", "jq", "sha256sum", "md5sum", "realpath", "readlink", "test",
		"get-content", "gc", "select-string", "sls"}
}

func shellChangesTheHarness(project, command string) bool {
	readsOnly := true
	for _, step := range subagent.ShellSteps(command) {
		if slices.ContainsFunc(step.Changes, func(changed string) bool { return reachesHarnessFile(project, changed) }) {
			return true
		}
		if step.Program == "tofu" && (slices.Contains(step.Args, "settings") && slices.Contains(step.Args, "set") ||
			slices.Contains(step.Args, "hooks") && slices.Contains(step.Args, "trust")) {
			return true
		}
		readsOnly = readsOnly && len(step.Changes) == 0 && slices.Contains(readOnlyPrograms(), step.Program)
	}
	lower := strings.ToLower(command)
	return !readsOnly && slices.ContainsFunc(harnessFiles(), func(file string) bool {
		dir, _, _ := strings.Cut(file, "/")
		return strings.Contains(lower, dir) && strings.Contains(lower, path.Base(file))
	})
}

func reachesHarnessFile(project, written string) bool {
	full := written
	home, _ := os.UserHomeDir()
	for _, spelled := range []string{"~", "$HOME", "${HOME}", "$USERPROFILE", "${USERPROFILE}", "%USERPROFILE%", "$env:USERPROFILE"} {
		if rest, spelt := strings.CutPrefix(strings.ToLower(written), strings.ToLower(spelled)); spelt && (rest == "" || rest[0] == '/' || rest[0] == '\\') {
			full = home + written[len(spelled):]
			break
		}
	}
	if !filepath.IsAbs(full) {
		full = filepath.Join(project, full)
	}
	names := []string{full}
	if real, err := filepath.EvalSymlinks(full); err == nil {
		names = append(names, real)
	}
	if dir, err := filepath.EvalSymlinks(filepath.Dir(full)); err == nil {
		names = append(names, filepath.Join(dir, filepath.Base(full)))
	}
	if target, err := os.Readlink(full); err == nil {
		names = append(names, target)
	}
	return slices.ContainsFunc(names, namesHarnessFile)
}

func namesHarnessFile(name string) bool {
	segments := strings.Split(strings.ToLower(path.Clean(strings.ReplaceAll(name, `\`, "/"))), "/")
	for i, segment := range segments {
		segment, _, _ = strings.Cut(segment, ":")
		segments[i] = strings.TrimRight(segment, ". ")
	}
	named := "/" + strings.Join(segments, "/")
	for _, file := range harnessFiles() {
		parts := strings.Split(file, "/")
		for k := range parts {
			if strings.HasSuffix(named, "/"+strings.Join(parts[:k+1], "/")) {
				return true
			}
		}
	}
	return false
}
