package recall

import (
	"fmt"
	"strings"
)

type CarriedResult struct {
	Tool   string `json:"tool"`
	Key    string `json:"key"`
	Bytes  int    `json:"bytes"`
	Handle string `json:"handle"`
}

type Carry struct {
	Text    string          `json:"text"`
	Results []CarriedResult `json:"results"`
}

func Crossed(cfg Config, bands Bands, c Conversation) bool {
	return Measure(cfg, bands, c).Total() > bands.Target()
}

const carryPreamble = "this session continues one that reached its context budget and ended. " +
	"the session that ended is on disk whole and nothing in it was rewritten. " +
	"every result it read is held whole in an artifact, and artifact_fetch reads any range of one by its handle.\n" +
	"what it did, in order:\n"

const carryLastWord = "the last thing it said or did:\n"

func HandleCarry(store *Store, cfg Config, c Conversation) (Carry, error) {
	var carry Carry
	text := &strings.Builder{}
	text.WriteString(carryPreamble)
	for _, entry := range c.Entries {
		if entry.Tool == "" || len(entry.Text) < cfg.CompactFloorBytes {
			continue
		}
		handle := entry.Handle
		if handle == "" {
			stored, err := store.put([]byte(entry.Text))
			if err != nil {
				return Carry{}, fmt.Errorf("recall: the fork could not store the %s result from step %d: %w", entry.Tool, entry.Step, err)
			}
			handle = stored
		}
		result := CarriedResult{Tool: entry.Tool, Key: entry.SupersedeKey, Bytes: len(entry.Text), Handle: handle}
		carry.Results = append(carry.Results, result)
		fmt.Fprintf(text, "  step %d %s: %d bytes, artifact %s\n", entry.Step, entry.SupersedeKey, result.Bytes, handle)
	}
	text.WriteString(carryLastWord)
	text.WriteString(lastWord(c))
	carry.Text = text.String()
	return carry, nil
}

func lastWord(c Conversation) string {
	for i := len(c.Entries) - 1; i >= 0; i-- {
		if c.Entries[i].Tool == "" && strings.TrimSpace(c.Entries[i].Text) != "" {
			return c.Entries[i].Text
		}
	}
	return ""
}
