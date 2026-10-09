package host

import (
	"context"
	"encoding/json"

	"tofu/internal/memory"
	"tofu/internal/session"
	"tofu/internal/turn"
	"tofu/internal/turn/tools"
)

func (h *Host) AnswerMemory(id string, scope memory.Scope) bool {
	return h.memoryPicks.answer(id, scope)
}

func pickMemory(emit func(Event), book *replies[memory.Scope]) tools.MemoryPick {
	return func(ctx context.Context, offer tools.MemoryOffer) (memory.Scope, error) {
		shown, err := json.Marshal(map[string]string{"statement": offer.Statement, "scope": string(offer.Scope), "said": offer.Said})
		if err != nil {
			return "", err
		}
		id := session.NewEventID()
		reply := book.wait(id)
		defer book.forget(id)
		emit(Event{Kind: EventDecision, ID: id, Decision: &Decision{Tool: turn.RememberToolName, Verdict: Ask, Remembers: offer.Statement, MemoryScope: offer.Scope, MemoryScopes: offer.Scopes}})
		accepts := []ApprovalDecision{}
		for _, scope := range offer.Scopes {
			accepts = append(accepts, rememberedAs(scope))
		}
		emit(Event{Kind: EventAwaitPerson, ID: id, Tool: turn.RememberToolName, Args: shown, Accepts: append(accepts, RejectOnce, RejectAlways, Cancelled)})
		defer emit(Event{Kind: EventResumed, ID: id})
		select {
		case picked := <-reply:
			return picked, nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}
