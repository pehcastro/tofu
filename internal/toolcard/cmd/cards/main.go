package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"tofu/internal/toolcard"
)

//go:embed fixture.json
var fixture []byte

const (
	highlightsShown = 4
	itemsShown      = 3
	rowColumns      = 90
	labelColumns    = 15
)

type call struct {
	Name    string          `json:"name"`
	Group   string          `json:"group"`
	Args    json.RawMessage `json:"args"`
	Content string          `json:"content"`
	Failed  bool            `json:"failed"`
	Exit    int             `json:"exit"`
}

func main() {
	var calls []call
	if err := json.Unmarshal(fixture, &calls); err != nil {
		fmt.Fprintln(os.Stderr, "fixture:", err)
		os.Exit(1)
	}
	var order []string
	grouped := map[string][]toolcard.Card{}
	for _, c := range calls {
		if len(grouped[c.Group]) == 0 {
			order = append(order, c.Group)
			fmt.Printf("\n%s\n", c.Group)
		}
		started := toolcard.Of(c.Name, c.Args)
		card := started.Settled(c.Content, c.Failed, c.Exit)
		grouped[c.Group] = append(grouped[c.Group], card)
		placed := "row"
		if !card.Fold {
			placed = "alone"
		}
		fmt.Printf("%2d  %-8s %-4s %-5s  %s  | running: %s\n", len(grouped[c.Group]), card.Family, card.Tone, placed,
			words(card.Verb, card.Object, card.Meta), words(started.Doing, started.Object))
	}
	room := toolcard.Room{Highlights: highlightsShown, Items: itemsShown, HeadColumns: rowColumns, DetailColumns: rowColumns - labelColumns}
	ruler := strings.Repeat("-", rowColumns)
	for _, group := range order {
		summary := toolcard.Group(grouped[group], room)
		fmt.Printf("\n%s summary, %d columns\n%s\n  %s\n", group, rowColumns, ruler, rendered(summary.Head))
		for _, line := range summary.Open {
			fmt.Printf("     %-9s %s\n", line.Family, rendered(line.Detail))
		}
	}
}

func rendered(phrase toolcard.Phrase) string {
	var said strings.Builder
	for _, piece := range phrase {
		said.WriteString(piece.Text)
	}
	return said.String()
}

func words(parts ...string) string {
	return strings.Join(strings.Fields(strings.Join(parts, " ")), " ")
}
