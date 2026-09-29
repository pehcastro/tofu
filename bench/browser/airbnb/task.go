package airbnb

import (
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
	SearchQuery       CheckKind = "search_query"
	SearchParams      CheckKind = "search_params"
	RoomsOpened       CheckKind = "rooms_opened"
	FinalIsNthListing CheckKind = "final_is_nth_listing"
	ReportListings    CheckKind = "report_listings"
	ReportFields      CheckKind = "report_fields"
)

type Field string

var fieldPatterns = map[Field]*regexp.Regexp{
	"price":    regexp.MustCompile(`(R\$|US\$|\$|€|£)\s?\d`),
	"rating":   regexp.MustCompile(`(?i)(nota|avalia\S*|rating|★)\s*[1-5][.,]\d{1,2}|[1-5][.,]\d{1,2}\s*(★|avalia|rating|stars|estrelas)`),
	"bedrooms": regexp.MustCompile(`(?i)\d+\s*(quartos?|bedrooms?)`),
	"pool":     regexp.MustCompile(`(?i)piscina|pool`),
}

var roomID = regexp.MustCompile(`/rooms/(\d+)`)

type Check struct {
	Kind   CheckKind         `json:"kind"`
	From   string            `json:"from"`
	Value  string            `json:"value"`
	Params map[string]string `json:"params"`
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
		case SearchedFirst, SearchQuery, RoomsOpened, ReportListings:
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
				if fieldPatterns[field] == nil {
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
		searched := slices.IndexFunc(run.Visits, func(visited string) bool {
			parsed, err := url.Parse(visited)
			return err == nil && strings.Contains(parsed.Host, check.From) && parsed.Path == "/search" && parsed.Query().Get("q") != ""
		})
		reached := slices.IndexFunc(run.Visits, func(visited string) bool {
			parsed, err := url.Parse(visited)
			return err == nil && strings.Contains(parsed.Host, check.Value)
		})
		return searched >= 0 && searched < reached
	case SearchQuery:
		named := strings.ToLower(check.Value)
		return search != nil && (strings.Contains(strings.ToLower(search.Path), named) || strings.Contains(strings.ToLower(search.Query().Get("query")), named))
	case SearchParams:
		for key, value := range check.Params {
			if search == nil || !slices.Contains(search.Query()[key], value) {
				return false
			}
		}
		return true
	case RoomsOpened:
		return len(openedRooms(run)) >= check.Count
	case FinalIsNthListing:
		var visitOrder []string
		for _, visited := range run.Visits {
			if match := roomID.FindStringSubmatch(visited); match != nil && !slices.Contains(visitOrder, match[1]) {
				visitOrder = append(visitOrder, match[1])
			}
		}
		final := roomID.FindStringSubmatch(snapshotURL(run.Snapshot))
		return final != nil && len(visitOrder) >= check.Count && visitOrder[check.Count-1] == final[1]
	case ReportListings:
		return len(namedListings(run)) >= check.Count
	case ReportFields:
		named := namedListings(run)
		for _, block := range named {
			for _, field := range check.Fields {
				if !fieldPatterns[field].MatchString(block) {
					return false
				}
			}
		}
		return len(named) > 0
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

func namedListings(run Run) map[string]string {
	opened := openedRooms(run)
	named := map[string]string{}
	for line := range strings.Lines(run.Report) {
		for _, match := range roomID.FindAllStringSubmatch(line, -1) {
			if opened[match[1]] {
				named[match[1]] += line
			}
		}
	}
	return named
}
