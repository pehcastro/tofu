package session

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode"
)

const (
	originMemoryView  = "memory view"
	originEpisodeView = "episode view"
)

func namesIn(events []Event) map[string]bool {
	named := map[string]bool{}
	notInAName := func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune("-_.", r)
	}
	note := func(text string) {
		for _, word := range strings.FieldsFunc(text, notInAName) {
			named[word], named[strings.TrimRight(word, ".")] = true, true
		}
	}
	for _, event := range events {
		note(event.Agent + " " + event.Call)
		if event.Agent != "" {
			continue
		}
		switch event.Kind {
		case EventMessage:
			var message MessageBody
			if json.Unmarshal(event.Body, &message) == nil && message.Origin != originMemoryView && message.Origin != originEpisodeView {
				note(message.Content)
			}
		case EventToolCall:
			decoder := json.NewDecoder(bytes.NewReader(event.Body))
			for token, err := decoder.Token(); err == nil; token, err = decoder.Token() {
				if text, isText := token.(string); isText {
					note(text)
				}
			}
		}
	}
	delete(named, "")
	return named
}
