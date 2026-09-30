package tasks

import (
	"cmp"
	"fmt"
	"math/rand/v2"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type city []string

var cityPairs = [][2]city{
	{{"São Paulo"}, {"Lisbon", "Lisboa"}},
	{{"São Paulo"}, {"Buenos Aires"}},
	{{"Rio de Janeiro"}, {"Santiago"}},
	{{"São Paulo"}, {"Miami"}},
	{{"São Paulo"}, {"New York", "Nova York"}},
	{{"Rio de Janeiro"}, {"Lisbon", "Lisboa"}},
	{{"Brasília"}, {"Salvador"}},
	{{"São Paulo"}, {"Recife"}},
	{{"Porto Alegre"}, {"Montevideo", "Montevidéu"}},
	{{"São Paulo"}, {"Madrid"}},
	{{"New York", "Nova York"}, {"London", "Londres"}},
	{{"San Francisco"}, {"Tokyo", "Tóquio"}},
	{{"Paris"}, {"Rome", "Roma"}},
	{{"London", "Londres"}, {"Barcelona"}},
	{{"Chicago"}, {"Mexico City", "Cidade do México"}},
}

var (
	portugueseMonths = []string{"jan", "fev", "mar", "abr", "mai", "jun", "jul", "ago", "set", "out", "nov", "dez"}
	moneyAmount      = regexp.MustCompile(`(?:R\$|US\$|\$|€|£)\s?(\d[\d.,]*)`)
	stopCount        = regexp.MustCompile(`(?i)\b(nonstop|non-stop|direct|direto|sem escalas?|sem paradas?)\b|\b(\d) (?:stops?|paradas?|escalas?)\b`)
	quotedText       = regexp.MustCompile(`"((?:[^"\\]|\\.)*)"`)
)

func (c city) shownIn(text string) bool {
	return slices.ContainsFunc(c, func(name string) bool { return strings.Contains(text, name) })
}

func dateShown(date time.Time, text string) bool {
	day, month := date.Day(), int(date.Month())
	shown := fmt.Sprintf(`(?i)\b%s[a-z]*\.? %d\b|\b%d de %s|\b0?%d/%02d\b`, date.Format("Jan"), day, day, portugueseMonths[month-1], day, month)
	return regexp.MustCompile(shown).MatchString(text)
}

func amounts(text string) []string {
	var found []string
	for _, match := range moneyAmount.FindAllStringSubmatch(text, -1) {
		found = append(found, strings.NewReplacer(".", "", ",", "").Replace(strings.TrimRight(match[1], ".,")))
	}
	return found
}

func stops(text string) []string {
	var found []string
	for _, match := range stopCount.FindAllStringSubmatch(text, -1) {
		found = append(found, cmp.Or(match[2], "0"))
	}
	return found
}

func sharesOne(report, page []string) bool {
	return slices.ContainsFunc(report, func(value string) bool { return slices.Contains(page, value) })
}

func namedIn(name, text string) bool {
	return regexp.MustCompile(`(?i)(^|[^\pL\pN])` + regexp.QuoteMeta(name) + `($|[^\pL\pN])`).MatchString(text)
}

func resultRows(page string) []string {
	var rows []string
	for line := range strings.Lines(page) {
		if strings.Contains(line, "listitem") {
			rows = append(rows, "")
		} else if len(rows) > 0 {
			rows[len(rows)-1] += line
		}
	}
	return rows
}

func airlinesIn(row string) []string {
	var parts []string
	summary := ""
	for _, found := range quotedText.FindAllStringSubmatch(row, -1) {
		parts = append(parts, strings.Split(found[1], ",")...)
		if len(found[1]) > len(summary) {
			summary = found[1]
		}
	}
	var names []string
	for _, part := range parts {
		name := strings.TrimSpace(part)
		first, _ := utf8.DecodeRuneInString(name)
		if unicode.IsUpper(first) && !strings.ContainsAny(name, "0123456789") && !stopCount.MatchString(name) && namedIn(name, summary) {
			names = append(names, name)
		}
	}
	return names
}

func reportedAirline(report, page string) bool {
	rows := resultRows(page)
	for _, price := range amounts(report) {
		if at := slices.IndexFunc(rows, func(row string) bool { return slices.Contains(amounts(row), price) }); at >= 0 {
			return slices.ContainsFunc(airlinesIn(rows[at]), func(airline string) bool { return namedIn(airline, report) })
		}
	}
	return false
}

func onGoogle(address *url.URL) bool {
	return strings.HasPrefix(address.Host, "www.google.") && (address.Path == "/" || address.Path == "/search")
}

func onFlights(address *url.URL) bool {
	return strings.HasPrefix(address.Host, "www.google.") && strings.HasPrefix(address.Path, "/travel/flights")
}

func drawFlights(seed int64, drawnOn time.Time) Task {
	draw := rand.New(rand.NewPCG(uint64(seed), 1))
	pair := cityPairs[draw.IntN(len(cityPairs))]
	depart := drawnOn.AddDate(0, 0, 21+draw.IntN(50))
	back := depart.AddDate(0, 0, 3+draw.IntN(12))
	passengers := 1 + draw.IntN(2)
	finalFlights := func(e Evidence) (Page, bool) {
		final, found := e.final()
		return final, found && onFlights(final.URL)
	}
	return Task{
		Name: "flights",
		Prompt: fmt.Sprintf("Start at google.com and reach Google Flights. Search a round trip from %s to %s, leaving %s and returning %s, for %d passenger(s). "+
			"Report the cheapest result's price, airline and number of stops, and leave the results page open. Do not book anything and do not sign in. Work in one tab.",
			pair[0][0], pair[1][0], depart.Format("January 2, 2006"), back.Format("January 2, 2006"), passengers),
		Steps: []Step{
			{"google.com came before Google Flights", func(e Evidence) bool {
				google := e.visited(onGoogle)
				return google >= 0 && google < e.visited(onFlights)
			}},
			{"Google Flights was reached", func(e Evidence) bool { return e.visited(onFlights) >= 0 }},
			{"the results page shows the origin and the destination", func(e Evidence) bool {
				final, found := finalFlights(e)
				return found && pair[0].shownIn(final.Text) && pair[1].shownIn(final.Text)
			}},
			{"the results page shows both dates", func(e Evidence) bool {
				final, found := finalFlights(e)
				return found && dateShown(depart, final.Text) && dateShown(back, final.Text)
			}},
			{"the report gives a price the results page shows", func(e Evidence) bool {
				final, found := finalFlights(e)
				return found && sharesOne(amounts(e.Run.Report), amounts(final.Text))
			}},
			{"the report names an airline the results page shows", func(e Evidence) bool {
				final, found := finalFlights(e)
				return found && reportedAirline(e.Run.Report, final.Text)
			}},
			{"the report gives a stop count the results page shows", func(e Evidence) bool {
				final, found := finalFlights(e)
				return found && sharesOne(stops(e.Run.Report), stops(final.Text))
			}},
			{"the task stayed in one tab", oneTab},
		},
	}
}
