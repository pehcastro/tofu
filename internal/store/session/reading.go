package session

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
)

func Kinds() []EventKind {
	return []EventKind{EventTurnStart, EventPrompt, EventRequest, EventMessage, EventToolCall, EventToolResult, EventSpawn,
		EventAgentEnd, EventCompaction, EventTurnEnd, EventAttachment, EventOutcome, EventStep, EventRead, EventNotice, EventListChange, EventReport, EventHook, EventMemoryRequest, EventSent,
		EventEstimate, EventEstimateSettled, EventWall}
}

type Step struct {
	Event string
	StepBody
	Raw json.RawMessage
}

type Message struct {
	Event string
	MessageBody
	Raw json.RawMessage
}

type Reading struct {
	Session     string
	Steps       []Step
	Messages    []Message
	Reads       []Read
	Attachments []Attachment
	Prompt      *PromptBody
	Outcome     json.RawMessage
	Unknown     []EventKind
}

func ReadEvents(events []Event) (Reading, error) {
	var reading Reading
	for i, event := range events {
		unreadable := func(err error) (Reading, error) {
			return Reading{}, fmt.Errorf("event %d is a %s that does not parse: %w", i+1, event.Kind, err)
		}
		switch event.Kind {
		case EventStep:
			step := Step{Event: event.ID, Raw: event.Body}
			if err := json.Unmarshal(event.Body, &step.StepBody); err != nil {
				return unreadable(err)
			}
			reading.Steps = append(reading.Steps, step)
		case EventMessage:
			message := Message{Event: event.ID, Raw: event.Body}
			if err := json.Unmarshal(event.Body, &message.MessageBody); err != nil {
				return unreadable(err)
			}
			reading.Messages = append(reading.Messages, message)
		case EventRead:
			var read Read
			if err := json.Unmarshal(event.Body, &read); err != nil {
				return unreadable(err)
			}
			reading.Reads = append(reading.Reads, read)
		case EventAttachment:
			var attachment Attachment
			if err := json.Unmarshal(event.Body, &attachment); err != nil {
				return unreadable(err)
			}
			reading.Attachments = append(reading.Attachments, attachment)
		case EventPrompt:
			var prompt PromptBody
			if err := json.Unmarshal(event.Body, &prompt); err != nil {
				return unreadable(err)
			}
			reading.Prompt = &prompt
		case EventOutcome:
			reading.Outcome = event.Body
		case EventNotice, EventListChange, EventReport, EventHook, EventMemoryRequest, EventSent, EventEstimate, EventEstimateSettled, EventWall:
		default:
			if !slices.Contains(reading.Unknown, event.Kind) {
				reading.Unknown = append(reading.Unknown, event.Kind)
			}
		}
	}
	return reading, nil
}

func (s *Store) LeadUserMessages(id string) ([]Message, error) {
	var messages []Message
	var unread error
	kept := func(event Event) bool {
		if event.Kind != EventMessage || event.Agent != "" {
			return true
		}
		message := Message{Event: event.ID, Raw: event.Body}
		if err := json.Unmarshal(event.Body, &message.MessageBody); err != nil {
			unread = fmt.Errorf("session %s: the message %s does not parse: %w", id, event.ID, err)
			return false
		}
		if message.Role == RoleUser {
			messages = append(messages, message)
		}
		return true
	}
	err := s.scanEvents(id, kept, func(line []byte, number int) (bool, error) {
		if !bytes.Contains(line, []byte(`"kind":"`+EventMessage+`"`)) || !bytes.Contains(line, []byte(`{"role":"`+RoleUser+`"`)) {
			return true, nil
		}
		event, err := parseLine[Event](line, id, number)
		return err == nil && kept(event), err
	})
	return messages, cmp.Or(unread, err)
}

func (s *Store) Reading(id string) (Reading, error) {
	events, err := s.Body(id)
	if err != nil {
		return Reading{}, err
	}
	reading, err := ReadEvents(events)
	if err != nil {
		return Reading{}, fmt.Errorf("session %s: %w", id, err)
	}
	reading.Session = id
	return reading, nil
}
