package host

import (
	"cmp"
	"errors"
	"strings"

	"tofu/internal/session"
	"tofu/internal/turn/tools"
)

type MentionOutcome string

const (
	MentionItem      MentionOutcome = MentionOutcome(tools.QuoteItem)
	MentionNotFound  MentionOutcome = MentionOutcome(tools.QuoteNotFound)
	MentionAmbiguous MentionOutcome = MentionOutcome(tools.QuoteAmbiguous)
)

func (MentionOutcome) enum() []string {
	return []string{string(MentionItem), string(MentionNotFound), string(MentionAmbiguous)}
}

type MentionParams struct {
	Session string `json:"session,omitempty"`
	Ref     string `json:"ref"`
}

type MentionResolved struct {
	Outcome MentionOutcome `json:"outcome"`
	Ref     string         `json:"ref"`
	Session string         `json:"session,omitempty"`
	Item    string         `json:"item,omitempty"`
	Speaker string         `json:"speaker,omitempty"`
	Line    string         `json:"line,omitempty"`
}

func (s *server) resolveMention(p MentionParams) (any, error) {
	ref := strings.TrimSpace(p.Ref)
	s.mu.Lock()
	recorded := cmp.Or(p.Session, s.items.session)
	item, asked := s.items.loggedAs(ref)
	s.mu.Unlock()
	if recorded == "" {
		return nil, &Refusal{Code: CodeRefused, Message: "no session is open and none was named, so there is nothing to resolve the mention in"}
	}
	store, err := session.OpenIn(s.Host.dir)
	if err != nil {
		return nil, err
	}
	found, err := tools.ResolveQuote(store, recorded, asked)
	if errors.Is(err, tools.ErrQuoteNoID) {
		return nil, &Refusal{Code: CodeBadParams, Message: err.Error()}
	}
	if err != nil {
		return nil, err
	}
	resolved := MentionResolved{Outcome: MentionOutcome(found.Outcome), Ref: ref, Session: found.Session}
	if found.Outcome == tools.QuoteItem {
		line, _, _ := strings.Cut(strings.TrimSpace(found.Words), "\n")
		resolved.Ref, resolved.Item, resolved.Speaker, resolved.Line = found.Ref, cmp.Or(item, found.Event), found.Speaker, line
	}
	return resolved, nil
}
