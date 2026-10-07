package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"tofu/internal/judge/ledger"
	"tofu/internal/session"
	"tofu/internal/subagent"
)

type GateRequest struct {
	TurnID string
	Task   string
	Tool   string
	Args   json.RawMessage
}

type GateDecision struct {
	ID         string
	Verdict    ledger.Verdict
	Answers    []ledger.Answer
	Reason     *ledger.Reason
	PersonOnly bool
	HookAsk    string
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
)

func (a PersonAnswer) allows() bool {
	switch a {
	case PersonDenied:
		return false
	case PersonAllowedOnce, PersonAlwaysHere:
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
	}
	panic("turn: unknown person answer")
}

type Person func(ctx context.Context, request GateRequest, decision GateDecision) (PersonAnswer, error)

func (p Person) RunsWhatJevAsks() Person {
	if p == nil {
		return nil
	}
	return func(ctx context.Context, request GateRequest, decision GateDecision) (PersonAnswer, error) {
		if decision.ID != "" && !decision.PersonOnly {
			return PersonAllowedOnce, nil
		}
		return p(ctx, request, decision)
	}
}

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
	if gateErr != "" {
		return "the gate could not answer, and a check that cannot run refuses: " + gateErr
	}
	switch decision.Verdict {
	case ledger.VerdictUnset, ledger.VerdictAllow:
		return ""
	case ledger.VerdictDeny:
		return "the verdict is deny" + standing(decision.Reason)
	case ledger.VerdictAsk:
		if person == nil {
			return "the verdict is ask" + standing(decision.Reason) + ", and no person was available to answer"
		}
		who := answerer(ctx)
		answer, err := person(ctx, request, decision)
		switch {
		case err != nil:
			return "the verdict is ask and " + who + " could not be asked: " + err.Error()
		case answer.allows():
			return ""
		}
		return "the verdict is ask" + standing(decision.Reason) + ", and " + who + " did not allow it"
	}
	panic("turn: unknown verdict " + string(decision.Verdict))
}

func answerer(ctx context.Context) string {
	if SubAgentAsking(ctx) != "" {
		return "the orchestrator"
	}
	return "the person"
}

const orchestratorAnswerWait = 5 * time.Minute

func (t *SpawnTool) orchestratorAnswers(held *heldSubAgent, site spawnSite) Person {
	return func(ctx context.Context, request GateRequest, decision GateDecision) (PersonAnswer, error) {
		id := held.agent.ID
		if decision.PersonOnly || decision.HookAsk == "" && decision.Verdict != ledger.VerdictAsk {
			return PersonDenied, errors.New("only the person answers this " + request.Tool + " question, and a sub-agent never asks the person")
		}
		because := "the gate's verdict is ask" + standing(decision.Reason)
		if decision.HookAsk != "" {
			because = "a PreToolUse hook asks first: " + decision.HookAsk
		}
		asked := fmt.Sprintf("sub-agent %s asks to run %s %s, because %s. it waits up to %s for you: call message with to %s and answer allow or deny. with no answer the call is refused.",
			id, request.Tool, request.Args, because, orchestratorAnswerWait, id)
		t.roster.Reached(id, subagent.WaitingAnswer, "asks to run "+request.Tool)
		defer t.roster.Reached(id, subagent.Working, "")
		site.notice(id, asked)
		started, answer := t.clock(), t.Inbox.ask(held, asked)
		within, cancel := context.WithTimeout(ctx, orchestratorAnswerWait)
		defer cancel()
		var allowed bool
		select {
		case allowed = <-answer:
		case <-within.Done():
			if t.Inbox.withdraw(held, answer, asked) {
				unanswered := fmt.Errorf("the orchestrator did not answer within %s: %w", t.clock().Sub(started).Round(time.Millisecond), context.Cause(within))
				site.notice(id, id+"'s "+request.Tool+" call is refused: "+unanswered.Error())
				return PersonDenied, unanswered
			}
			allowed = <-answer
		}
		said, verdict := "deny", PersonDenied
		if allowed {
			said, verdict = "allow", PersonAllowedOnce
		}
		site.notice(id, "the orchestrator answered "+said+" to "+id+"'s "+request.Tool+" call after "+t.clock().Sub(started).Round(time.Millisecond).String())
		return verdict, nil
	}
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
