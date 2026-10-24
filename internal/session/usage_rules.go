package session

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"tofu/internal/konst"
)

const (
	ruleMarkOpen  = "from the rule "
	ruleMarkClose = "]"
)

type Week [daysInAWeek]int

type RuleFires struct {
	Fires int  `json:"fires"`
	Week  Week `json:"fires_week"`
}

func (s *Store) RuleFires(now time.Time) (map[string]RuleFires, []Skip, error) {
	listing, err := s.Listing()
	if err != nil {
		return nil, nil, err
	}
	fires, skipped := map[string]RuleFires{}, listing.Skipped
	fired := func(id string, at time.Time) {
		ago, counted := int(calendarDay(now, now).Sub(calendarDay(at, now)).Hours()/hoursInADay), fires[id]
		counted.Fires++
		if ago < daysInAWeek {
			counted.Week[daysInAWeek-1-ago]++
		}
		fires[id] = counted
	}
	for _, header := range listing.Sessions {
		parts, counted := map[string][]PromptPart{}, map[string]bool{}
		err := s.eachOf(header.ID, []EventKind{EventPrompt, EventMessage, EventSent}, func(event Event) {
			var sent struct {
				System  string       `json:"system"`
				Parts   []PromptPart `json:"parts"`
				Role    string       `json:"role"`
				Content string       `json:"content"`
			}
			if json.Unmarshal(event.Body, &sent) != nil {
				return
			}
			switch {
			case event.Kind == EventPrompt:
				parts[event.Agent] = sent.Parts
				if sent.Parts == nil {
					markedRules(sent.System, event.At, fired)
				}
			case event.Kind == EventMessage && sent.Role == RoleUser && parts[event.Agent] == nil:
				markedRules(sent.Content, event.At, fired)
			case event.Kind == EventSent:
				for _, part := range parts[event.Agent] {
					if key := event.Turn + "\x00" + event.Agent + "\x00" + part.Rule; part.Rule != "" && !part.Off && !counted[key] {
						counted[key] = true
						fired(part.Rule, event.At)
					}
				}
			}
		})
		if err != nil {
			skipped = append(skipped, Skip{ID: header.ID, Reason: err})
		}
	}
	return fires, skipped, nil
}

func markedRules(text string, at time.Time, fired func(string, time.Time)) {
	named := map[string]bool{}
	for _, part := range strings.Split(text, ruleMarkOpen)[1:] {
		id, closed := strings.CutSuffix(strings.SplitAfter(part, ruleMarkClose)[0], ruleMarkClose)
		if closed && !named[id] {
			named[id] = true
			fired(id, at)
		}
	}
}

func calendarDay(at, now time.Time) time.Time {
	year, month, day := at.In(now.Location()).Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

type RequestParts struct {
	Request string       `json:"request"`
	Agent   string       `json:"agent,omitempty"`
	Turn    string       `json:"turn,omitempty"`
	At      time.Time    `json:"at"`
	Prompt  []PromptPart `json:"prompt"`
	Sent    SentBody     `json:"sent"`
	Carried []SentItem   `json:"carried"`
}

var ErrSentBeforeParts = errors.New("session: nothing records what went into this request, and a request sent before tofu kept that record has none")

func (s *Store) RequestParts(id, handle string) (RequestParts, error) {
	prompts, carried := map[string]json.RawMessage{}, map[string][]SentItem{}
	var found []RequestParts
	err := s.eachOf(id, []EventKind{EventPrompt, EventSent}, func(event Event) {
		if event.Kind == EventPrompt {
			prompts[event.Agent] = event.Body
			return
		}
		var sent SentBody
		if json.Unmarshal(event.Body, &sent) != nil {
			return
		}
		kept := carried[event.Agent]
		for _, dropped := range sent.Dropped {
			if at := slices.IndexFunc(kept, func(item SentItem) bool { return item.Blob == dropped }); at >= 0 {
				kept = slices.Delete(kept, at, at+1)
			}
		}
		carried[event.Agent] = append(kept, sent.Added...)
		if DrawnAs(event.Request, handle) {
			var prompt PromptBody
			_ = json.Unmarshal(prompts[event.Agent], &prompt)
			found = append(found, RequestParts{Request: event.Request, Agent: event.Agent, Turn: event.Turn, At: event.At, Prompt: prompt.Parts, Sent: sent,
				Carried: slices.Clone(carried[event.Agent])})
		}
	})
	switch {
	case err != nil:
		return RequestParts{}, err
	case len(found) == 0:
		return RequestParts{}, fmt.Errorf("%w: %q", ErrSentBeforeParts, handle)
	case len(found) > 1:
		return RequestParts{}, fmt.Errorf("%w: %q matches %d requests", ErrEventHashAmbiguous, handle, len(found))
	}
	return found[0], nil
}

func (s *Store) eachOf(id string, kinds []EventKind, visit func(Event)) error {
	if err := namesOneSession(id); err != nil {
		return err
	}
	if events, read := s.parsed[id]; read || s.legacy(id) {
		var err error
		if !read {
			events, err = s.legacyEvents(id)
		}
		for _, event := range events {
			if slices.Contains(kinds, event.Kind) {
				visit(event)
			}
		}
		return err
	}
	file, err := os.Open(s.EventsPath(id))
	if err != nil {
		return fmt.Errorf("session: %s has no events: %w", id, err)
	}
	defer func() { _ = file.Close() }()
	reader, line := bufio.NewReaderSize(file, konst.EventScanBytes), []byte(nil)
	for number := 0; ; {
		chunk, err := reader.ReadSlice('\n')
		blank, wanted := len(bytes.TrimSpace(chunk)) == 0, slices.Contains(kinds, kindOf(chunk))
		if wanted {
			line = append(line[:0], chunk...)
		}
		for errors.Is(err, bufio.ErrBufferFull) {
			chunk, err = reader.ReadSlice('\n')
			if wanted {
				line = append(line, chunk...)
			}
		}
		switch {
		case errors.Is(err, io.EOF):
			return nil
		case err != nil:
			return err
		case blank:
			continue
		}
		number++
		if !wanted {
			continue
		}
		event, err := parseLine[Event](line, id, number)
		if err != nil {
			return err
		}
		visit(event)
	}
}

func kindOf(head []byte) EventKind {
	_, after, found := bytes.Cut(head, []byte(`"kind":"`))
	kind, _, closed := bytes.Cut(after, []byte(`"`))
	if !found || !closed {
		return ""
	}
	return EventKind(kind)
}
