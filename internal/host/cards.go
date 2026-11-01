package host

import (
	"math"
	"slices"
	"sync"

	"tofu/internal/llm"
	"tofu/internal/toolcard"
)

type ToolCard struct {
	Family CardFamily `json:"family"`
	Doing  string     `json:"doing"`
	Verb   string     `json:"verb"`
	Object string     `json:"object,omitempty"`
	Meta   string     `json:"meta,omitempty"`
	From   string     `json:"from,omitempty"`
	To     string     `json:"to,omitempty"`
	Tone   CardTone   `json:"tone"`
}

type CardFamily string

func (CardFamily) enum() []string { return names(toolcard.Families()) }

type CardTone string

func (CardTone) enum() []string { return names(toolcard.Tones()) }

type CardInk string

func (CardInk) enum() []string { return names(toolcard.Inks()) }

func names[T ~string](values []T) []string {
	said := make([]string, len(values))
	for at, value := range values {
		said[at] = string(value)
	}
	return said
}

type CardPiece struct {
	Text string  `json:"text"`
	Ink  CardInk `json:"ink"`
}

type CardLine struct {
	Family CardFamily  `json:"family"`
	Detail []CardPiece `json:"detail"`
	masked *maskedLine
}

type maskedLine struct {
	once   sync.Once
	detail []CardPiece
}

func (l CardLine) maskedBy(mask func(string) string) CardLine {
	if l.masked == nil {
		return CardLine{Family: l.Family, Detail: maskedPieces(l.Detail, mask)}
	}
	l.masked.once.Do(func() { l.masked.detail = maskedPieces(l.Detail, mask) })
	return CardLine{Family: l.Family, Detail: l.masked.detail}
}

type GroupSummary struct {
	Calls  int         `json:"calls"`
	Failed int         `json:"failed"`
	Head   []CardPiece `json:"head"`
	Open   []CardLine  `json:"open"`
}

type cardRow struct {
	id    string
	cards toolcard.Row
	wired map[toolcard.Family]wiredLine
}

type wiredLine struct {
	from toolcard.Phrase
	line CardLine
}

type placedCard struct {
	row  *cardRow
	seat toolcard.Seat
}

type cardRows struct {
	open   map[string]*cardRow
	placed map[string]placedCard
}

func (r *cardRows) place(asker, id string, card toolcard.Card) (*ToolCard, string) {
	if r.open == nil {
		r.open, r.placed = map[string]*cardRow{}, map[string]placedCard{}
	}
	row := r.open[asker]
	if row == nil {
		row = &cardRow{id: id, cards: toolcard.Row{Room: toolcard.Room{Highlights: math.MaxInt, Items: math.MaxInt, HeadColumns: math.MaxInt, DetailColumns: math.MaxInt}}, wired: map[toolcard.Family]wiredLine{}}
		r.open[asker] = row
	}
	r.placed[id] = placedCard{row: row, seat: row.cards.Add(card)}
	return wireCard(card), row.id
}

func (r *cardRows) settle(id, content string, outcome ToolOutcome, exit *int) (*ToolCard, string, *GroupSummary) {
	placed, known := r.placed[id]
	if !known {
		return nil, "", nil
	}
	delete(r.placed, id)
	code := 0
	if exit != nil {
		code = *exit
	}
	ending := toolcard.Finished
	switch outcome {
	case ToolFailed:
		ending = toolcard.Broken
	case ToolSkipped:
		ending = toolcard.Skipped
	case ToolRan, ToolAborted:
	}
	row := placed.row
	settled := row.cards.Settle(placed.seat, content, ending, code)
	summary := row.cards.Summary()
	wired := &GroupSummary{Calls: summary.Calls, Failed: summary.Failed, Head: wirePhrase(summary.Head)}
	for _, line := range summary.Open {
		held, known := row.wired[line.Family]
		if !known || !slices.Equal(held.from, line.Detail) {
			held = wiredLine{from: line.Detail, line: CardLine{Family: CardFamily(line.Family), Detail: wirePhrase(line.Detail), masked: &maskedLine{}}}
			row.wired[line.Family] = held
		}
		wired.Open = append(wired.Open, held.line)
	}
	return wireCard(settled), row.id, wired
}

func (a *watcher) endLeadRow() {
	a.marks.Lock()
	defer a.marks.Unlock()
	delete(a.cards.open, "")
}

func endsTheRow(source llm.Source) bool {
	return source != llm.SourceEnvironment && source != llm.SourceSpawnLine
}

func wireCard(card toolcard.Card) *ToolCard {
	return &ToolCard{Family: CardFamily(card.Family), Doing: card.Doing, Verb: card.Verb, Object: card.Object, Meta: card.Meta, From: card.From, To: card.To, Tone: CardTone(card.Tone)}
}

func maskedPieces(pieces []CardPiece, mask func(string) string) []CardPiece {
	masked := slices.Clone(pieces)
	for at := range masked {
		masked[at].Text = mask(masked[at].Text)
	}
	return masked
}

func wirePhrase(phrase toolcard.Phrase) []CardPiece {
	pieces := make([]CardPiece, len(phrase))
	for at, piece := range phrase {
		pieces[at] = CardPiece{Text: piece.Text, Ink: CardInk(piece.Ink)}
	}
	return pieces
}
