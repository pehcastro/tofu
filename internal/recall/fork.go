package recall

import (
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
	Facts   []string        `json:"facts,omitempty"`
	Results []CarriedResult `json:"results"`
}

func Crossed(cfg Config, bands Bands, c Conversation) bool {
	return Measure(cfg, bands, c).Total() > bands.Target()
}

const carryPreamble = "this session continues one that reached its context budget and ended. " +
	"the session that ended is on disk whole and nothing in it was rewritten. " +
	"every result it read is held whole in an artifact, and artifact_fetch reads any range of one by its handle.\n" +
	"what this line of sessions has already looked at, oldest first:\n"

const carryLastWord = "the last thing it said or did:\n"

func HandleCarry(store *Store, cfg Config, c Conversation) (Carry, error) {
	return buildCarry(store, c, konst.FactSignpostBytes)
}

func DistilledCarry(store *Store, cfg Config, c Conversation) (Carry, error) {
	return buildCarry(store, c, konst.CarrySignpostBytes)
}

func buildCarry(store *Store, c Conversation, signpostBytes int) (Carry, error) {
	facts, kept, err := Distil(store, c, signpostBytes)
	if err != nil {
		return Carry{}, err
	}
	text := &strings.Builder{}
	text.WriteString(carryPreamble)
	for _, line := range facts {
		text.WriteString(line)
		text.WriteString("\n")
	}
	text.WriteString(carryLastWord)
	text.WriteString(lastWord(c))
	return Carry{Text: text.String(), Facts: facts, Results: kept}, nil
}

func oneLine(text string, limit int) string {
	var line strings.Builder
	gap := false
	for _, letter := range text {
		if line.Len() >= limit {
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
