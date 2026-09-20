package recall

import (
	"fmt"
	"strings"
	"unicode"

	"tofu/internal/konst"
)

type CarriedResult struct {
	Tool     string `json:"tool"`
	Key      string `json:"key"`
	Bytes    int    `json:"bytes"`
	Handle   string `json:"handle"`
	Signpost string `json:"signpost,omitempty"`
}

type Carry struct {
	Text    string          `json:"text"`
	Results []CarriedResult `json:"results"`
}

type carryDetail int

const (
	carryNames carryDetail = iota
	carrySignposts
)

func Crossed(cfg Config, bands Bands, c Conversation) bool {
	return Measure(cfg, bands, c).Total() > bands.Target()
}

const carryPreamble = "this session continues one that reached its context budget and ended. " +
	"the session that ended is on disk whole and nothing in it was rewritten. " +
	"every result it read is held whole in an artifact, and artifact_fetch reads any range of one by its handle.\n" +
	"what it did, in order:\n"

const carryLastWord = "the last thing it said or did:\n"

func HandleCarry(store *Store, cfg Config, c Conversation) (Carry, error) {
	return buildCarry(store, cfg, c, carryNames)
}

func DistilledCarry(store *Store, cfg Config, c Conversation) (Carry, error) {
	return buildCarry(store, cfg, c, carrySignposts)
}

func buildCarry(store *Store, cfg Config, c Conversation, detail carryDetail) (Carry, error) {
	var carry Carry
	text := &strings.Builder{}
	text.WriteString(carryPreamble)
	for _, entry := range c.Entries {
		if entry.Tool == "" {
			continue
		}
		stored := len(entry.Text) >= cfg.CompactFloorBytes
		if !stored && detail == carryNames {
			continue
		}
		handle := entry.Handle
		if handle == "" && stored {
			put, err := store.put([]byte(entry.Text))
			if err != nil {
				return Carry{}, fmt.Errorf("recall: the fork could not store the %s result from step %d: %w", entry.Tool, entry.Step, err)
			}
			handle = put
		}
		result := CarriedResult{Tool: entry.Tool, Key: entry.SupersedeKey, Bytes: len(entry.Text), Handle: handle}
		fmt.Fprintf(text, "  step %d %s: %d bytes", entry.Step, oneLine(entry.SupersedeKey), result.Bytes)
		if handle != "" {
			fmt.Fprintf(text, ", artifact %s", handle)
		}
		if detail == carrySignposts {
			result.Signpost = oneLine(entry.Text)
			fmt.Fprintf(text, "\n    it came back: %s", result.Signpost)
		}
		text.WriteString("\n")
		carry.Results = append(carry.Results, result)
	}
	text.WriteString(carryLastWord)
	text.WriteString(lastWord(c))
	carry.Text = text.String()
	return carry, nil
}

func oneLine(text string) string {
	var line strings.Builder
	gap := false
	for _, letter := range text {
		if line.Len() >= konst.CarrySignpostBytes {
			return line.String() + " ..."
		}
		if unicode.IsSpace(letter) {
			gap = line.Len() > 0
			continue
		}
		if gap {
			line.WriteByte(' ')
			gap = false
		}
		line.WriteRune(letter)
	}
	return line.String()
}

func lastWord(c Conversation) string {
	for i := len(c.Entries) - 1; i >= 0; i-- {
		if c.Entries[i].Tool == "" && strings.TrimSpace(c.Entries[i].Text) != "" {
			return c.Entries[i].Text
		}
	}
	return ""
}
