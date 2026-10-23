package toolcard

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Ink string

const (
	Plain   Ink = "plain"
	Muted   Ink = "muted"
	Ref     Ink = "ref"
	Added   Ink = "added"
	Removed Ink = "removed"
	Failed  Ink = "failed"
)

func Inks() []Ink { return []Ink{Plain, Muted, Ref, Added, Removed, Failed} }

type Piece struct {
	Text string
	Ink  Ink
}

type Phrase []Piece

type Room struct {
	Highlights    int
	Items         int
	HeadColumns   int
	DetailColumns int
}

type Line struct {
	Family Family
	Detail Phrase
}

type Summary struct {
	Calls  int
	Failed int
	Head   Phrase
	Open   []Line
}

const (
	highlightGap = "   "
	itemGap      = ", "
)

func Group(cards []Card, room Room) Summary {
	summary := Summary{Calls: len(cards)}
	byFamily := map[Family][]Card{}
	for _, card := range cards {
		byFamily[card.Family] = append(byFamily[card.Family], card)
		if card.Tone == Fail {
			summary.Failed++
		}
	}
	summary.Head = say(Piece{Text: counted(len(cards), "call", "calls"), Ink: Muted})
	shown := 0
	for _, highlight := range highlights(cards, summary.Failed) {
		next := append(append(slices.Clip(summary.Head), Piece{Text: highlightGap, Ink: Plain}), highlight...)
		if shown < room.Highlights && next.width() <= room.HeadColumns {
			summary.Head, shown = next, shown+1
		}
	}
	for _, family := range Families() {
		if held := byFamily[family]; len(held) > 0 {
			items, collapsed := detail(family, held)
			summary.Open = append(summary.Open, Line{Family: family, Detail: fitted(items, collapsed, room)})
		}
	}
	return summary
}

func text(said string) Piece { return Piece{Text: said, Ink: Plain} }

func failure() Piece { return Piece{Text: "failed", Ink: Failed} }

func say(pieces ...Piece) Phrase {
	var said Phrase
	for _, piece := range pieces {
		if piece.Text == "" {
			continue
		}
		if len(said) > 0 {
			piece.Text = " " + piece.Text
		}
		said = append(said, piece)
	}
	return said
}

func (p Phrase) width() int {
	columns := 0
	for _, piece := range p {
		columns += utf8.RuneCountInString(piece.Text)
	}
	return columns
}

func joined(items []Phrase) Phrase {
	var said Phrase
	for at, item := range items {
		if at > 0 {
			said = append(said, text(itemGap))
		}
		said = append(said, item...)
	}
	return said
}

func fitted(items, collapsed []Phrase, room Room) Phrase {
	if line := joined(items); len(items) <= room.Items && line.width() <= room.DetailColumns {
		return line
	}
	for kept := len(collapsed); kept > 0; kept-- {
		line := joined(collapsed[:kept])
		if kept < len(collapsed) {
			line = append(line, text(itemGap+"+"+strconv.Itoa(len(collapsed)-kept)+" more"))
		}
		if kept == 1 || line.width() <= room.DetailColumns {
			return line
		}
	}
	return nil
}

func counts(added, removed int) []Piece {
	var said []Piece
	if added > 0 {
		said = append(said, Piece{Text: "+" + strconv.Itoa(added), Ink: Added})
	}
	if removed > 0 {
		said = append(said, Piece{Text: "−" + strconv.Itoa(removed), Ink: Removed})
	}
	return said
}

func highlights(cards []Card, failed int) []Phrase {
	var said []Phrase
	if failed > 0 {
		said = append(said, say(text(strconv.Itoa(failed)), failure()))
	}
	var moved []string
	var lastTest Card
	pushes, saved, started := false, false, 0
	for _, card := range cards {
		ok := card.Tone != Fail
		switch {
		case !card.settled:
		case card.Family == Tests:
			lastTest = card
		case card.kind == kindCommit:
			pushes = pushes || card.pushNote == pushedNote
		case card.kind == kindPush:
			pushes = pushes || ok
		case card.kind == kindBoardMove && ok && !slices.Contains(moved, card.Object):
			moved = append(moved, card.Object)
		case card.kind == kindRemember:
			saved = saved || card.Verb == "saved"
		case card.kind == kindSpawn && ok:
			started++
		}
	}
	if committed := committedShas(cards); committed != nil {
		if pushes {
			committed = append(committed, text(", pushed"))
		}
		said = append(said, committed)
	}
	if edits := merged(cards); len(edits) > 0 {
		said = append(said, say(text(counted(len(edits), "edit", "edits"))))
	}
	if lastTest.Family == Tests && lastTest.Tone != Fail {
		said = append(said, say(text("tests pass")))
	}
	switch len(moved) {
	case 0:
	case 1:
		said = append(said, say(text("moved"), Piece{Text: moved[0], Ink: Ref}))
	default:
		said = append(said, say(text(strconv.Itoa(len(moved))+" tickets moved")))
	}
	if saved {
		said = append(said, say(text("memory saved")))
	}
	if started > 0 {
		said = append(said, say(text(counted(started, "agent", "agents")+" started")))
	}
	return said
}

func committedShas(cards []Card) Phrase {
	said := say(text("committed"))
	for _, card := range cards {
		if card.kind != kindCommit || card.Object == "" {
			continue
		}
		if len(said) > 1 {
			said = append(said, text(","))
		}
		said = append(said, Piece{Text: " " + card.Object, Ink: Ref})
	}
	if len(said) == 1 {
		return nil
	}
	return said
}

func merged(cards []Card) []Change {
	var changes []Change
	at := map[string]int{}
	for _, card := range cards {
		if card.Family != Edited || card.Tone == Fail {
			continue
		}
		for _, change := range card.Changes {
			index, seen := at[change.Path]
			if !seen {
				index, changes = len(changes), append(changes, Change{Path: change.Path})
				at[change.Path] = index
			}
			changes[index].Added += change.Added
			changes[index].Removed += change.Removed
		}
	}
	return changes
}

func detail(family Family, cards []Card) ([]Phrase, []Phrase) {
	switch family {
	case Explored:
		said := explored(cards)
		return said, said
	case Edited:
		var items, short []Phrase
		for _, change := range merged(cards) {
			delta := counts(change.Added, change.Removed)
			items = append(items, say(append([]Piece{text(change.Path)}, delta...)...))
			short = append(short, say(append([]Piece{text(base(change.Path))}, delta...)...))
		}
		return items, short
	case Tests:
		return tested(cards)
	case Git:
		return gitDetail(cards)
	case Board:
		return boarded(cards)
	case Memory:
		said := tally(cards, func(card Card) string { return card.Verb })
		return said, said
	case Agents, Web:
		return distinct(cards), tally(cards, func(card Card) string { return card.Verb })
	case Asked:
		var items []Phrase
		for _, card := range cards {
			question := card.Object
			if question == "" {
				question = "a question"
			}
			items = append(items, say(text(question+","), text(card.Meta)))
		}
		return items, append([]Phrase{say(text(counted(len(cards), "question", "questions")))}, tally(cards, func(card Card) string { return card.Meta })...)
	case Ran:
		return ran(cards)
	}
	panic("toolcard: unknown family " + string(family))
}

func distinct(cards []Card) []Phrase {
	var said []Phrase
	seen := map[string]bool{}
	for _, card := range cards {
		if key := card.Verb + " " + card.Object; !seen[key] {
			seen[key] = true
			said = append(said, say(text(card.Verb), text(card.Object)))
		}
	}
	return said
}

func tally(cards []Card, key func(Card) string) []Phrase {
	var order []string
	counted := map[string]int{}
	for _, card := range cards {
		if name := key(card); name != "" {
			if counted[name]++; counted[name] == 1 {
				order = append(order, name)
			}
		}
	}
	said := make([]Phrase, len(order))
	for at, name := range order {
		said[at] = say(text(name), text(strconv.Itoa(counted[name])))
	}
	return said
}

func explored(cards []Card) []Phrase {
	files, searches, listings := map[string]bool{}, 0, 0
	var looks []string
	for _, card := range cards {
		switch card.kind {
		case kindRead:
			files[card.Object] = true
		case kindSearch:
			searches++
		case kindList:
			listings++
		default:
			if look := strings.TrimPrefix(card.Object, "git "); !slices.Contains(looks, look) {
				looks = append(looks, look)
			}
		}
	}
	var said []Phrase
	if len(files) > 0 {
		said = append(said, say(text("read "+counted(len(files), "file", "files"))))
	}
	if searches > 0 {
		said = append(said, say(text(counted(searches, "search", "searches"))))
	}
	if listings > 0 {
		said = append(said, say(text(counted(listings, "listing", "listings"))))
	}
	if len(looks) > 0 {
		said = append(said, say(text("git "+strings.Join(looks, itemGap))))
	}
	return said
}

func tested(cards []Card) ([]Phrase, []Phrase) {
	var keys []string
	runs := map[string][]Card{}
	for _, card := range cards {
		key := strings.TrimSpace(card.Verb + " " + card.Object)
		if len(runs[key]) == 0 {
			keys = append(keys, key)
		}
		runs[key] = append(runs[key], card)
	}
	var items []Phrase
	var names []string
	for _, key := range keys {
		items = append(items, say(append([]Piece{text(key)}, outcome(runs[key])...)...))
		if last := runs[key][len(runs[key])-1]; last.Tone == Fail {
			names = append(names, cmp.Or(why(last), key))
		}
	}
	collapsed := []Phrase{say(text(counted(len(cards), "run", "runs")))}
	if len(names) == 0 {
		return items, append(collapsed, say(text("all passed")))
	}
	collapsed = append(collapsed, Phrase{text(strconv.Itoa(len(names)) + " "), failure(), text(": " + names[0])})
	for _, name := range names[1:] {
		collapsed = append(collapsed, say(text(name)))
	}
	return items, collapsed
}

func why(card Card) string {
	return strings.TrimPrefix(strings.TrimSpace(strings.TrimPrefix(card.Meta, "failed")), "at ")
}

func outcome(runs []Card) []Piece {
	failures := 0
	for _, run := range runs {
		if run.Tone == Fail {
			failures++
		}
	}
	last := runs[len(runs)-1]
	switch {
	case failures == 0:
		return []Piece{text("passed")}
	case last.Tone != Fail:
		return []Piece{text("failed " + times(failures) + ", then passed")}
	case failures == len(runs):
		return []Piece{failure(), text(why(last))}
	}
	return []Piece{text("passed, then"), failure(), text(why(last))}
}

func times(n int) string {
	if n == 1 {
		return "once"
	}
	return strconv.Itoa(n) + " times"
}

func gitDetail(cards []Card) ([]Phrase, []Phrase) {
	var items, failures []Phrase
	for _, card := range cards {
		sub := strings.TrimPrefix(card.failedVerb, "git ")
		var item Phrase
		switch {
		case card.kind == kindCommit && card.Object != "":
			item = say(Piece{Text: card.Object, Ink: Ref}, text(card.subject))
			if card.pushNote != "" {
				item = append(item, text(itemGap+card.pushNote))
			}
		case card.Tone == Fail && card.kind == kindPush && strings.HasPrefix(card.Meta, "("):
			item = say(text("push refused"), text(card.Meta))
		case card.Tone == Fail && card.kind == kindPull && strings.Contains(card.Meta, "conflict"):
			item = say(text(sub), text(strings.TrimPrefix(card.Meta, "1 ")))
		case card.Tone == Fail:
			item = say(text(cmp.Or(sub, card.Verb)), failure())
		default:
			item = say(text(card.Verb), text(card.Object), text(card.Meta))
		}
		items = append(items, item)
		if card.Tone == Fail {
			failures = append(failures, item)
		}
	}
	if committed := committedShas(cards); committed != nil {
		failures = append(failures, committed)
	}
	return items, failures
}

func boarded(cards []Card) ([]Phrase, []Phrase) {
	var ids []string
	moves, parts := map[string][]string{}, map[string][]string{}
	var reads []Card
	for _, card := range cards {
		if card.kind == kindRan {
			reads = append(reads, card)
			continue
		}
		if len(moves[card.Object]) == 0 && len(parts[card.Object]) == 0 {
			ids = append(ids, card.Object)
		}
		var part string
		switch card.kind {
		case kindBoardMove:
			if card.Tone != Fail {
				moves[card.Object] = append(moves[card.Object], card.Meta)
				continue
			}
		case kindBoardNew:
			part = "opened"
		case kindBoardLog:
			part = "logged " + card.Meta
		case kindBoardAssign:
			part = "assigned " + card.Meta
		case kindBoardAsk:
			part = "asked"
		}
		if card.Tone == Fail {
			part = card.Verb + " failed"
		}
		parts[card.Object] = append(parts[card.Object], part)
	}
	var items, collapsed []Phrase
	for _, id := range ids {
		done, last := parts[id], argument(parts[id], 0)
		if held := moves[id]; len(held) > 0 {
			done, last = append([]string{strings.Join(held, " ")}, done...), held[len(held)-1]
		}
		ticket := Piece{Text: id, Ink: Ref}
		items = append(items, say(ticket, text(strings.Join(done, itemGap))))
		collapsed = append(collapsed, say(ticket, text(last)))
	}
	looked := distinct(reads)
	return append(items, looked...), append(collapsed, looked...)
}

func ran(cards []Card) ([]Phrase, []Phrase) {
	var items, failing []Phrase
	seen := map[string]bool{}
	for _, card := range cards {
		if seen[card.Object] {
			continue
		}
		seen[card.Object] = true
		if card.Tone != Fail {
			items = append(items, say(text(card.Object)))
			continue
		}
		item := say(text(card.Object), failure(), text(card.Meta))
		items, failing = append(items, item), append(failing, item)
	}
	return items, append([]Phrase{say(text(counted(len(cards), "command", "commands")))}, failing...)
}
