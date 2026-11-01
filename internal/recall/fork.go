package recall

import (
	"encoding/json"
	"path"
	"path/filepath"
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
	Call     string `json:"call,omitempty"`
	Handle   string `json:"handle"`
	Signpost string `json:"signpost,omitempty"`
}

type Carry struct {
	Text            string          `json:"text"`
	State           string          `json:"state,omitempty"`
	Facts           []string        `json:"facts,omitempty"`
	Said            []string        `json:"said,omitempty"`
	Standing        []string        `json:"standing,omitempty"`
	StandingDropped int             `json:"standing_dropped,omitempty"`
	Results         []CarriedResult `json:"results"`
	Files           []string        `json:"files,omitempty"`
	Unseen          []string        `json:"unseen,omitempty"`
}

const SaidCarryHeading = "what the person said in this line of sessions, oldest first, each message word for word as one quoted line. it stands until the person says otherwise:\n"

const (
	standingCarryHeading = "the standing orders the person gave in this line of sessions, oldest first, each sentence word for word as one quoted line. " +
		"they are kept apart from the messages above and outlive them, and each stands until the person says otherwise:\n"
	standingMark    = "order: "
	standingDropped = " older standing orders were dropped to keep this list under its cap"
)

func Crossed(cfg Config, bands Bands, c Conversation) bool {
	return Measure(cfg, bands, c).Total() > bands.Target()
}

const carryPreamble = "this session continues one that reached its context budget and ended. " +
	"the session that ended is on disk whole and nothing in it was rewritten. " +
	"each line below names the call that holds what it read whole, and lookup returns that call and its result again.\n" +
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

type StandingOrders func(sentences []string) []bool

func WordsAlone(sentences []string) []bool {
	kept := make([]bool, len(sentences))
	for i, sentence := range sentences {
		kept[i] = StandingByWords(sentence)
	}
	return kept
}

func HandleCarry(store *Store, cfg Config, c Conversation) (Carry, error) {
	return buildCarry(store, cfg, c, konst.FactSignpostBytes, WordsAlone, 0)
}

func DistilledCarry(store *Store, cfg Config, c Conversation) (Carry, error) {
	return JudgedCarry(store, cfg, c, WordsAlone)
}

func JudgedCarry(store *Store, cfg Config, c Conversation, standing StandingOrders) (Carry, error) {
	return buildCarry(store, cfg, c, konst.CarrySignpostBytes, standing, 0)
}

func CarryWithFiles(store *Store, cfg Config, c Conversation, standing StandingOrders, fileTokens int) (Carry, error) {
	return buildCarry(store, cfg, c, konst.CarrySignpostBytes, standing, fileTokens)
}

func buildCarry(store *Store, cfg Config, c Conversation, signpostBytes int, standing StandingOrders, fileTokens int) (Carry, error) {
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
	said, orders, dropped := personsWords(c, standing)
	if len(said)+len(orders) > 0 {
		text.WriteString(SaidCarryHeading)
		for _, words := range said {
			text.WriteString(saidMark + strconv.Quote(words) + "\n")
		}
	}
	if len(orders) > 0 {
		text.WriteString(standingCarryHeading)
		for _, order := range orders {
			text.WriteString(standingMark + strconv.Quote(order) + "\n")
		}
		if dropped > 0 {
			text.WriteString(strconv.Itoa(dropped) + standingDropped + "\n")
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
		text.WriteString(carryLastWord + cutWords(said, konst.CarryLastWordBytes))
	} else {
		text.WriteString(carryHeldTail)
	}
	files, unseen := holdFiles(cfg, c, fileTokens, text)
	return Carry{Text: text.String(), Facts: facts, Said: said, Standing: orders, StandingDropped: dropped, Results: kept, Files: files, Unseen: unseen}, nil
}

const (
	carryFiles      = "files read whole and not written or edited since, newest first, each exactly as its last read returned it, so it need not be read again:\n"
	carryFileMark   = "file "
	carryFileCall   = " bytes, read by call "
	carryFileEnd    = ":\n"
	carryUnseen     = "the last step read these whole and they came back over this session's budget, so they are not carried and were never seen: "
	carryUnseenRead = ". read the lines needed with start_line and end_line rather than a whole file, or it will not fit again.\n"
	readTool        = "read"
	errorResult     = "error: "
)

func (c Carry) WithoutFiles() Carry {
	before, after, found := strings.Cut(c.Text, carryFiles)
	if !found {
		return c
	}
	c.Text, c.Files = before, nil
	if _, unseen, named := strings.Cut(after, carryUnseen); named {
		c.Text += carryUnseen + unseen
	}
	return c
}

type wholeFile struct {
	path, call, text string
	unseen           bool
}

func (f wholeFile) block() string {
	return carryFileMark + f.path + ", " + strconv.Itoa(len(f.text)) + carryFileCall + f.call + carryFileEnd + f.text + "\n"
}

func holdFiles(cfg Config, c Conversation, fileTokens int, text *strings.Builder) (held, unseen []string) {
	if fileTokens <= 0 {
		return nil, nil
	}
	room, section := fileTokens-cfg.Tokens(carryFiles), ""
	for _, file := range wholeFiles(c) {
		block := file.block()
		switch {
		case cfg.Tokens(block) <= room:
			room -= cfg.Tokens(block)
			held = append(held, file.path)
			section += block
		case file.unseen:
			unseen = append(unseen, file.path+" ("+strconv.Itoa(len(file.text))+" bytes)")
		}
	}
	if section != "" {
		section = carryFiles + section
	}
	if len(unseen) > 0 {
		section += carryUnseen + strings.Join(unseen, ", ") + carryUnseenRead
	}
	if section != "" && !strings.HasSuffix(text.String(), "\n") {
		section = "\n" + section
	}
	text.WriteString(section)
	return held, unseen
}

func wholeFiles(c Conversation) []wholeFile {
	var files []wholeFile
	settled := map[string]bool{}
	settles := func(name string) bool {
		key := path.Clean(filepath.ToSlash(name))
		if name == "" || settled[key] {
			return false
		}
		settled[key] = true
		return true
	}
	last := 0
	if len(c.Entries) > 0 {
		last = c.Entries[len(c.Entries)-1].Step
	}
	for i, entry := range slices.Backward(c.Entries) {
		held := i >= c.heldFrom()
		_, raw, _ := strings.Cut(entry.SupersedeKey, " ")
		var args struct {
			Path      string `json:"path"`
			StartLine int    `json:"start_line"`
			EndLine   int    `json:"end_line"`
			Edits     []struct {
				Path string `json:"path"`
			} `json:"edits"`
		}
		_ = json.Unmarshal([]byte(raw), &args)
		failed, ranged := strings.HasPrefix(entry.Text, errorResult), args.StartLine > 0 || args.EndLine > 0
		switch {
		case entry.Tool == "":
			files, _, _ := carriedFiles(entry.Text)
			for _, file := range files {
				if settles(file.path) && !held {
					files = append(files, file)
				}
			}
		case entry.Tool == readTool && ranged && !failed:
		case entry.Tool == readTool && !failed && entry.Images == 0 && !AlreadyDropped(entry.Text):
			if settles(args.Path) && !held {
				files = append(files, wholeFile{path: args.Path, call: entry.Call, text: entry.Text, unseen: entry.Step == last})
			}
		default:
			settles(args.Path)
			for _, edit := range args.Edits {
				settles(edit.Path)
			}
		}
	}
	return files
}

func carriedFiles(text string) (files []wholeFile, rest string, found bool) {
	_, rest, found = strings.Cut(text, carryFiles)
	for found {
		head, body, ended := strings.Cut(rest, carryFileEnd)
		named, marked := strings.CutPrefix(head, carryFileMark)
		before, call, called := strings.Cut(named, carryFileCall)
		at := strings.LastIndex(before, ", ")
		if !ended || !marked || !called || at < 0 {
			break
		}
		size, err := strconv.Atoi(before[at+2:])
		if err != nil || size >= len(body) {
			break
		}
		files = append(files, wholeFile{path: before[:at], call: call, text: body[:size]})
		rest = body[size+1:]
	}
	return files, rest, found
}

type CarriedFile struct {
	Path, Call string
	Bytes      int
}

type CarriedFiles struct {
	Held       []CarriedFile
	Unseen     string
	Unreadable bool
}

func ReadCarried(text string) CarriedFiles {
	files, rest, found := carriedFiles(text)
	carried := CarriedFiles{Unreadable: found && rest != "" && !strings.HasPrefix(rest, carryUnseen)}
	for _, file := range files {
		carried.Held = append(carried.Held, CarriedFile{Path: file.path, Call: file.call, Bytes: len(file.text)})
	}
	if _, unseen, named := strings.Cut(text, carryUnseen); named {
		carried.Unseen, _, _ = strings.Cut(unseen, carryUnseenRead)
	}
	return carried
}

const saidMark = "said: "

func personsWords(c Conversation, standing StandingOrders) (said, orders []string, dropped int) {
	var all, given []string
	var asked []int
	heard := func(words string) string {
		if words = beforeCronFire(words); words != "" {
			all = append(all, words)
		}
		return words
	}
	judge := func(words string) {
		for _, sentence := range sentencesIn(words) {
			asked, given = append(asked, len(given)), append(given, sentence)
		}
	}
	for _, entry := range c.Entries {
		_, section, carried := strings.Cut(entry.Text, SaidCarryHeading)
		section, _, _ = strings.Cut(section, carryFiles)
		switch {
		case entry.Tool != "":
		case entry.Said != "":
			judge(heard(entry.Said))
		case carried:
			ordersKept := strings.Contains(section, standingCarryHeading)
			for _, line := range strings.Split(section, "\n") {
				if count, found := strings.CutSuffix(line, standingDropped); found {
					carriedDropped, _ := strconv.Atoi(count)
					dropped = max(dropped, carriedDropped)
				}
				if quoted, marked := strings.CutPrefix(line, saidMark); marked {
					if words, err := strconv.Unquote(quoted); err == nil {
						if words = heard(words); !ordersKept {
							judge(words)
						}
					}
				}
				if quoted, marked := strings.CutPrefix(line, standingMark); marked {
					if order, err := strconv.Unquote(quoted); err == nil {
						given = append(given, order)
					}
				}
			}
		}
	}
	sentences := make([]string, len(asked))
	for i, at := range asked {
		sentences[i] = given[at]
	}
	for i, kept := range standing(sentences) {
		if !kept {
			given[asked[i]] = ""
		}
	}
	given = slices.DeleteFunc(given, func(order string) bool { return order == "" })
	said, _ = newestWithin(all, saidMark, konst.CarrySaidBytes, konst.CarrySaidMessageBytes)
	orders, over := newestWithin(given, standingMark, konst.CarryStandingBytes, konst.CarryStandingOrderBytes)
	return said, orders, dropped + over
}

func beforeCronFire(said string) string {
	marked := strings.Index(said, konst.CronPromptMark)
	if marked < 0 {
		return said
	}
	return strings.TrimSpace(said[:strings.LastIndex(said[:marked], "\n")+1])
}

func sentencesIn(said string) []string {
	var sentences []string
	for _, line := range strings.Split(said, "\n") {
		start := 0
		for i := 0; i < len(line); i++ {
			end := i + 1
			if end < len(line) && (!strings.ContainsRune(".!?", rune(line[i])) || line[end] != ' ') {
				continue
			}
			if sentence := strings.TrimSpace(line[start:end]); sentence != "" {
				sentences = append(sentences, sentence)
			}
			start = end
		}
	}
	return sentences
}

func StandingByWords(sentence string) bool {
	words := strings.FieldsFunc(strings.ToLower(strings.ReplaceAll(sentence, "’", "'")), func(letter rune) bool {
		return !unicode.IsLetter(letter) && letter != '\''
	})
	spaced := " " + strings.Join(words, " ") + " "
	return slices.ContainsFunc(strings.Split(konst.CarryStandingPhrases, "|"), func(phrase string) bool {
		return strings.Contains(spaced, " "+phrase+" ")
	})
}

func newestWithin(items []string, mark string, room, each int) (kept []string, over int) {
	seen := map[string]bool{}
	for _, item := range slices.Backward(items) {
		cut := cutWords(item, each)
		if seen[cut] {
			continue
		}
		seen[cut] = true
		if room -= len(mark + strconv.Quote(cut) + "\n"); room < 0 {
			over++
			continue
		}
		kept = append(kept, cut)
	}
	slices.Reverse(kept)
	return kept, over
}

const saidCutMark = " ..."

func cutWords(words string, limit int) string {
	if len(words) <= limit {
		return words
	}
	end := limit - len(saidCutMark)
	for !utf8.RuneStart(words[end]) {
		end--
	}
	return words[:end] + saidCutMark
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

func lastWord(c Conversation) (string, int) {
	for i := len(c.Entries) - 1; i >= 0; i-- {
		entry := c.Entries[i]
		if entry.Tool != "" {
			continue
		}
		if strings.TrimSpace(entry.Text) != "" {
			return entry.Text, i
		}
		var called []string
		for _, call := range strings.Split(strings.TrimSpace(entry.Calls), "\n") {
			if name, _, _ := strings.Cut(call, " "); name != "" {
				called = append(called, name)
			}
		}
		if len(called) > 0 {
			return "it called " + strings.Join(called, ", ") + "\n", i
		}
	}
	return "", -1
}
