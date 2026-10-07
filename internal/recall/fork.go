package recall

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

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
	Said    []string        `json:"said,omitempty"`
	Results []CarriedResult `json:"results"`
}

const SaidCarryHeading = "what the person said in this line of sessions, oldest first, each message word for word as one quoted line. it stands until the person says otherwise:\n"

func Crossed(cfg Config, bands Bands, c Conversation) bool {
	return Measure(cfg, bands, c).Total() > bands.Target()
}

const carryPreamble = "this session continues one that reached its context budget and ended. " +
	"the session that ended is on disk whole and nothing in it was rewritten. " +
	"every result it read is held whole in an artifact, and artifact_fetch reads any range of one by its handle.\n" +
	"what this line of sessions has already looked at, oldest first:\n"

const carryLastWord = "the last thing it said or did:\n"

const (
	carryActs = "the last browser actions it took, oldest first, each with what it did to the page:\n"
	actTool   = "browser_act"
)

const carrySnapshots = "the last snapshot of each tab it worked in, as it came back, so the page need not be observed again before acting:\n"

const carryHeldTail = "its last steps follow this message word for word, as they were sent.\n"

func (c Conversation) heldFrom() int {
	return len(c.Entries) - c.HeldWhole
}

func lastSnapshots(c Conversation) []string {
	var tabs []string
	newest := make(map[string]int)
	for i, entry := range c.Entries {
		_, _, body := unwrapped(entry.Text)
		first, _, _ := strings.Cut(body, "\n")
		fields := strings.Fields(first)
		if !interactiveSnapshot(entry) || len(fields) < 2 || fields[0] != "tab" {
			continue
		}
		tab := fields[1]
		if _, seen := newest[tab]; !seen {
			tabs = append(tabs, tab)
		}
		newest[tab] = i
	}
	var snapshots []string
	for _, tab := range tabs {
		if at := newest[tab]; at < c.heldFrom() {
			lead, _, _ := unwrapped(c.Entries[at].Text)
			snapshots = append(snapshots, strings.TrimPrefix(c.Entries[at].Text, lead))
		}
	}
	return snapshots
}

func interactiveSnapshot(entry Entry) bool {
	_, args, _ := strings.Cut(entry.SupersedeKey, " ")
	var parsed struct {
		Tab         *int  `json:"tab"`
		Interactive *bool `json:"interactive"`
		Actions     []struct {
			Action string `json:"action"`
		} `json:"actions"`
	}
	if json.Unmarshal([]byte(args), &parsed) != nil || parsed.Tab == nil || parsed.Interactive != nil && !*parsed.Interactive {
		return false
	}
	for _, act := range parsed.Actions {
		if act.Action == "navigate" || act.Action == "back" || act.Action == "wait" {
			return false
		}
	}
	return true
}

func lastActs(c Conversation) []string {
	var acts []string
	for _, entry := range c.Entries[:c.heldFrom()] {
		lines := ""
		switch entry.Tool {
		case "":
			_, carried, _ := strings.Cut(entry.Text, carryActs)
			lines, _, _ = strings.Cut(carried, carryLastWord)
		case actTool:
			lines, _, _ = unwrapped(entry.Text)
		}
		for _, line := range strings.Split(lines, "\n") {
			number, act, numbered := strings.Cut(line, ". ")
			if _, err := strconv.Atoi(number); numbered && err == nil {
				acts = append(acts, act)
			}
		}
	}
	return acts[max(0, len(acts)-konst.CarryActLines):]
}

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
	said := saidSoFar(c)
	if len(said) > 0 {
		text.WriteString(SaidCarryHeading)
		for _, words := range said {
			text.WriteString(saidMark + strconv.Quote(words) + "\n")
		}
	}
	if snapshots := lastSnapshots(c); len(snapshots) > 0 {
		text.WriteString(carrySnapshots)
		for _, snapshot := range snapshots {
			text.WriteString(snapshot + "\n")
		}
	}
	if acts := lastActs(c); len(acts) > 0 {
		text.WriteString(carryActs)
		for i, act := range acts {
			text.WriteString(strconv.Itoa(i+1) + ". " + act + "\n")
		}
	}
	if said, at := lastWord(c); at < c.heldFrom() {
		text.WriteString(carryLastWord + said)
	} else {
		text.WriteString(carryHeldTail)
	}
	return Carry{Text: text.String(), Facts: facts, Said: said, Results: kept}, nil
}

const saidMark = "said: "

func saidSoFar(c Conversation) []string {
	var said []string
	for _, entry := range c.Entries {
		_, section, carried := strings.Cut(entry.Text, SaidCarryHeading)
		switch {
		case entry.Tool != "":
		case entry.Said != "":
			said = append(said, entry.Said)
		case carried:
			for _, line := range strings.Split(section, "\n") {
				quoted, marked := strings.CutPrefix(line, saidMark)
				if words, err := strconv.Unquote(quoted); marked && err == nil {
					said = append(said, words)
				}
			}
		}
	}
	var kept []string
	room := konst.CarrySaidBytes
	for _, words := range slices.Backward(said) {
		cut := words
		if len(cut) > konst.CarrySaidMessageBytes {
			end := konst.CarrySaidMessageBytes - len(saidCutMark)
			for !utf8.RuneStart(cut[end]) {
				end--
			}
			cut = cut[:end] + saidCutMark
		}
		if slices.Contains(kept, cut) {
			continue
		}
		if room -= len(cut); room < 0 {
			break
		}
		kept = append(kept, cut)
	}
	slices.Reverse(kept)
	return kept
}

const saidCutMark = " ..."

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

func lastWord(c Conversation) (string, int) {
	for i := len(c.Entries) - 1; i >= 0; i-- {
		if c.Entries[i].Tool == "" && strings.TrimSpace(c.Entries[i].Text) != "" {
			return c.Entries[i].Text, i
		}
	}
	return "", -1
}
