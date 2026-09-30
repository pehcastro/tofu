package airbnb

import (
	"cmp"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

type CheckKind string

const (
	SearchedFirst     CheckKind = "searched_first"
	ListingsIn        CheckKind = "listings_in"
	SearchParams      CheckKind = "search_params"
	RoomsOpened       CheckKind = "rooms_opened"
	FinalIsNthListing CheckKind = "final_is_nth_listing"
	ReportListings    CheckKind = "report_listings"
	ReportFields      CheckKind = "report_fields"
)

type Field string

const noRating = `|(?i)\b[0-2] (avaliaç\S*|coment\S*|reviews?)\b|no score|no reviews?|sem avaliaç|novo anúncio|\bnovo\b`

var linePatterns = map[Field]*regexp.Regexp{
	"price":    regexp.MustCompile(`(R\$|US\$|\$|€|£)\s?\d`),
	"rating":   regexp.MustCompile(`(?i)(nota|avalia\S*|rating|★)\s*[1-5][.,]\d{1,2}|[1-5][.,]\d{1,2}\s*((de|out of) 5\s*)?(★|avalia|rating|stars|estrelas)` + noRating),
	"bedrooms": regexp.MustCompile(`(?i)\d+\s*(quartos?|bedrooms?)`),
	"pool":     regexp.MustCompile(`(?i)piscina|pool`),
	"wifi":     regexp.MustCompile(`(?i)wi-?fi|internet`),
	"pets":     regexp.MustCompile(`(?i)pets?\b|animais|animal`),
	"kitchen":  regexp.MustCompile(`(?i)kitchen|cozinha`),
}

var yesOrNo = regexp.MustCompile(`(?i)^\W*(yes|no|sim|não|nao)\b`)

var cellPatterns = map[Field]*regexp.Regexp{
	"price":    linePatterns["price"],
	"rating":   regexp.MustCompile(`^\W*[1-5][.,]\d{1,2}` + noRating),
	"bedrooms": regexp.MustCompile(`\d`),
	"pool":     yesOrNo,
	"wifi":     yesOrNo,
	"pets":     yesOrNo,
	"kitchen":  yesOrNo,
}

var columnOf = map[Field]*regexp.Regexp{
	"price":    regexp.MustCompile(`(?i)price|preço|total`),
	"rating":   regexp.MustCompile(`(?i)rating|avalia|nota`),
	"bedrooms": regexp.MustCompile(`(?i)bedroom|quarto`),
	"pool":     linePatterns["pool"],
	"wifi":     linePatterns["wifi"],
	"pets":     linePatterns["pets"],
	"kitchen":  linePatterns["kitchen"],
}

var (
	roomID    = regexp.MustCompile(`/rooms/(\d+)`)
	tableRule = regexp.MustCompile(`^\s*\|[\s:|-]+\|\s*$`)
)

type Listing struct {
	ID    string
	Line  string
	Page  string
	Cells map[Field]string
}

func (l Listing) Answers(field Field) bool {
	if cell, inTable := l.Cells[field]; inTable {
		return cellPatterns[field].MatchString(strings.TrimSpace(cell))
	}
	return linePatterns[field].MatchString(l.Line)
}

func tableCells(line string) []string {
	trimmed := strings.TrimSpace(strings.ReplaceAll(line, `\|`, "/"))
	if len(trimmed) < 2 || !strings.HasPrefix(trimmed, "|") || !strings.HasSuffix(trimmed, "|") {
		return nil
	}
	return strings.Split(trimmed[1:len(trimmed)-1], "|")
}

type Check struct {
	Kind   CheckKind         `json:"kind"`
	From   string            `json:"from"`
	Value  string            `json:"value"`
	Params map[string]string `json:"params"`
	AnyOf  map[string]string `json:"any_of"`
	Count  int               `json:"count"`
	Fields []Field           `json:"fields"`
}

type Step struct {
	Step  int    `json:"step"`
	Says  string `json:"says"`
	Check Check  `json:"check"`
}

type Task struct {
	Prompt string `json:"prompt"`
	Steps  []Step `json:"steps"`
}

//go:embed task.json
var taskJSON []byte

func Load() (Task, error) {
	var task Task
	if err := json.Unmarshal(taskJSON, &task); err != nil {
		return task, fmt.Errorf("task.json: %w", err)
	}
	for at, step := range task.Steps {
		if step.Step != at+1 {
			return task, fmt.Errorf("task.json: step %d sits at position %d", step.Step, at+1)
		}
		switch step.Check.Kind {
		case SearchedFirst, ListingsIn, RoomsOpened, ReportListings:
		case FinalIsNthListing:
			if step.Check.Count < 1 {
				return task, fmt.Errorf("task.json: step %d names no listing by its place in the visit order", step.Step)
			}
		case SearchParams:
			if len(step.Check.Params) == 0 {
				return task, fmt.Errorf("task.json: step %d checks no params", step.Step)
			}
		case ReportFields:
			for _, field := range step.Check.Fields {
				if linePatterns[field] == nil {
					return task, fmt.Errorf("task.json: step %d names unknown field %q", step.Step, field)
				}
			}
		default:
			return task, fmt.Errorf("task.json: step %d has unknown check %q", step.Step, step.Check.Kind)
		}
	}
	return task, nil
}

func passes(check Check, run Run) bool {
	search := lastSearch(run.Visits)
	switch check.Kind {
	case SearchedFirst:
		reached := slices.IndexFunc(run.Pages, func(page Page) bool {
			parsed, err := url.Parse(page.URL)
			return err == nil && strings.Contains(parsed.Host, check.Value)
		})
		if reached < 1 || !slices.Contains(strings.Fields(run.Pages[reached].Reached), "click") {
			return false
		}
		before, err := url.Parse(run.Pages[reached-1].URL)
		return err == nil && strings.Contains(before.Host, check.From) && before.Path == "/search" && before.Query().Get("q") != ""
	case ListingsIn:
		reported := listings(run)
		for _, listing := range reported {
			if !strings.Contains(strings.ToLower(listing.Page), strings.ToLower(check.Value)) {
				return false
			}
		}
		return len(reported) > 0
	case SearchParams:
		carries := func(key, value string) bool { return search != nil && slices.Contains(search.Query()[key], value) }
		for key, value := range check.Params {
			if !carries(key, value) {
				return false
			}
		}
		for key, value := range check.AnyOf {
			if carries(key, value) {
				return true
			}
		}
		return len(check.AnyOf) == 0
	case RoomsOpened:
		return len(openedRooms(run)) >= check.Count
	case FinalIsNthListing:
		reported := listings(run)
		final := roomID.FindStringSubmatch(snapshotURL(run.Snapshot))
		return final != nil && len(reported) >= check.Count && reported[check.Count-1].ID == final[1]
	case ReportListings:
		return len(listings(run)) >= check.Count
	case ReportFields:
		reported := listings(run)
		for _, listing := range reported {
			for _, field := range check.Fields {
				if !listing.Answers(field) {
					return false
				}
			}
		}
		return len(reported) > 0
	}
	panic(fmt.Sprintf("unknown check %q", check.Kind))
}

func allURLs(run Run) []string {
	urls := slices.Clone(run.Visits)
	for _, tab := range run.Tabs {
		urls = append(urls, tab.URL)
	}
	return urls
}

func lastSearch(visits []string) *url.URL {
	for _, visited := range slices.Backward(visits) {
		if parsed, err := url.Parse(visited); err == nil && strings.HasPrefix(parsed.Path, "/s/") {
			return parsed
		}
	}
	return nil
}

func openedRooms(run Run) map[string]bool {
	opened := map[string]bool{}
	for _, visited := range allURLs(run) {
		if match := roomID.FindStringSubmatch(visited); match != nil {
			opened[match[1]] = true
		}
	}
	return opened
}

func snapshotURL(snapshot string) string {
	if header := snapshotHeader.FindStringSubmatch(snapshot); header != nil {
		return header[2]
	}
	return ""
}

func openedInOrder(run Run) ([]string, map[string]string, map[string]string) {
	var order []string
	titles, texts := map[string]string{}, map[string]string{}
	for _, page := range run.Pages {
		if match := roomID.FindStringSubmatch(page.URL); match != nil {
			titles[match[1]] = cmp.Or(titles[match[1]], page.Title)
			texts[match[1]] += page.Text
		}
	}
	for _, visited := range allURLs(run) {
		if match := roomID.FindStringSubmatch(visited); match != nil && !slices.Contains(order, match[1]) {
			order = append(order, match[1])
		}
	}
	return order, titles, texts
}

func reportEntries(report string, opened map[string]bool) []Listing {
	var rows, lines []Listing
	var header []string
	fields := 0
	text := slices.Collect(strings.Lines(report))
	for at, line := range text {
		cells := tableCells(line)
		if cells != nil && at+1 < len(text) && tableRule.MatchString(text[at+1]) {
			header, fields = cells, 0
			for _, name := range header {
				for _, pattern := range columnOf {
					if pattern.MatchString(name) {
						fields++
					}
				}
			}
			continue
		}
		match := roomID.FindStringSubmatch(line)
		if cells != nil && len(cells) == len(header) && fields >= 2 && !tableRule.MatchString(line) {
			row := Listing{Line: line, Cells: map[Field]string{}}
			if match != nil && opened[match[1]] {
				row.ID = match[1]
			}
			for column, name := range header {
				for field, pattern := range columnOf {
					if pattern.MatchString(name) {
						row.Cells[field] = cells[column]
					}
				}
			}
			rows = append(rows, row)
			continue
		}
		if match != nil && opened[match[1]] && !slices.ContainsFunc(lines, func(listing Listing) bool { return listing.ID == match[1] }) {
			lines = append(lines, Listing{ID: match[1], Line: line})
		}
	}
	if len(rows) > 0 {
		return rows
	}
	return lines
}

func listings(run Run) []Listing {
	order, titles, texts := openedInOrder(run)
	entries := reportEntries(run.Report, openedRooms(run))
	bound := map[string]bool{}
	for _, entry := range entries {
		bound[entry.ID] = entry.ID != ""
	}
	free := func() []string {
		return slices.DeleteFunc(slices.Clone(order), func(id string) bool { return bound[id] })
	}
	for at := range entries {
		for _, id := range free() {
			name, _, _ := strings.Cut(titles[id], " - ")
			if entries[at].ID == "" && name != "" && strings.Contains(strings.ToLower(entries[at].Line), strings.ToLower(name)) {
				entries[at].ID, bound[id] = id, true
			}
		}
	}
	for at := range entries {
		if remaining := free(); entries[at].ID == "" && len(remaining) > 0 {
			entries[at].ID, bound[remaining[0]] = remaining[0], true
		}
	}
	var reported []Listing
	for _, entry := range entries {
		if entry.ID != "" {
			entry.Page = texts[entry.ID]
			reported = append(reported, entry)
		}
	}
	return reported
}
