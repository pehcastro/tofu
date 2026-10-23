package host

import (
	"math"
	"slices"

	"tofu/internal/llm"
	"tofu/internal/toolcard"
)

type ToolCard struct {
	Family CardFamily `json:"family"`
	Doing  string     `json:"doing"`
	Verb   string     `json:"verb"`
	Object string     `json:"object,omitempty"`
	Meta   string     `json:"meta,omitempty"`
	Tone   CardTone   `json:"tone"`
}

type CardFamily string

func (CardFamily) enum() []string {
	return []string{string(toolcard.Explored), string(toolcard.Edited), string(toolcard.Tests), string(toolcard.Git), string(toolcard.Board),
		string(toolcard.Memory), string(toolcard.Agents), string(toolcard.Asked), string(toolcard.Web), string(toolcard.Ran)}
}

type CardTone string

func (CardTone) enum() []string {
	return []string{string(toolcard.Ok), string(toolcard.Warn), string(toolcard.Fail)}
}

type CardInk string

func (CardInk) enum() []string {
	return []string{string(toolcard.Plain), string(toolcard.Muted), string(toolcard.Ref), string(toolcard.Added), string(toolcard.Removed), string(toolcard.Failed)}
}

type CardPiece struct {
	Text string  `json:"text"`
	Ink  CardInk `json:"ink"`
}

type CardLine struct {
	Family CardFamily  `json:"family"`
	Detail []CardPiece `json:"detail"`
}

type GroupSummary struct {
	Calls  int         `json:"calls"`
	Failed int         `json:"failed"`
	Head   []CardPiece `json:"head"`
	Open   []CardLine  `json:"open"`
}

type cardRow struct {
	id    string
	cards []toolcard.Card
}

type placedCard struct {
	row *cardRow
	at  int
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
		row = &cardRow{id: id}
		r.open[asker] = row
	}
	r.placed[id] = placedCard{row: row, at: len(row.cards)}
	row.cards = append(row.cards, card)
	return wireCard(card), row.id
}

func (r *cardRows) settle(id, content string, failed bool, exit *int) (*ToolCard, string, *GroupSummary) {
	placed, known := r.placed[id]
	if !known {
		return nil, "", nil
	}
	delete(r.placed, id)
	code := 0
	if exit != nil {
		code = *exit
	}
	row := placed.row
	row.cards[placed.at] = row.cards[placed.at].Settled(content, failed, code)
	summary := toolcard.Group(row.cards, toolcard.Room{Highlights: math.MaxInt, Items: math.MaxInt, HeadColumns: math.MaxInt, DetailColumns: math.MaxInt})
	wired := &GroupSummary{Calls: summary.Calls, Failed: summary.Failed, Head: wirePhrase(summary.Head)}
	for _, line := range summary.Open {
		wired.Open = append(wired.Open, CardLine{Family: CardFamily(line.Family), Detail: wirePhrase(line.Detail)})
	}
	return wireCard(row.cards[placed.at]), row.id, wired
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
	return &ToolCard{Family: CardFamily(card.Family), Doing: card.Doing, Verb: card.Verb, Object: card.Object, Meta: card.Meta, Tone: CardTone(card.Tone)}
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
