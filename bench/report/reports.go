package report

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Provenance string

const (
	FromProse Provenance = "prose"
	FromRerun Provenance = "rerun"
)

func (p Provenance) Describe() string {
	if p == FromRerun {
		return "built by running the package's own code again"
	}
	return "built from the report's own text, not from a rerun, which is weaker evidence"
}

type handRead struct {
	Report string     `json:"report"`
	Figure string     `json:"figure"`
	Says   string     `json:"says"`
	From   Provenance `json:"from"`
	Sample string     `json:"sample"`
	Skips  string     `json:"skips"`
}

type Report struct {
	Package     string     `json:"package"`
	Date        string     `json:"date"`
	Source      string     `json:"source"`
	Title       string     `json:"title"`
	Figure      string     `json:"figure"`
	Says        string     `json:"says"`
	BuiltFrom   Provenance `json:"built_from"`
	BuiltFromIs string     `json:"built_from_is"`
	Sample      string     `json:"sample"`
	Skips       string     `json:"skips"`
	Conclusion  string     `json:"conclusion"`
	Unparsed    bool       `json:"conclusion_unparsed"`
	State       State      `json:"state"`
	StateSource string     `json:"state_source"`
	StateNote   string     `json:"state_note"`
	Conditions  []string   `json:"conditions"`
	Arms        *Block     `json:"arms"`
	Body        []Block    `json:"body"`
}

func Build(benchRoot string) (Data, error) {
	index, err := readIndex(benchRoot)
	if err != nil {
		return Data{}, err
	}
	hand, err := readHandRead(filepath.Join(benchRoot, "report", "handread.json"))
	if err != nil {
		return Data{}, err
	}
	data := Data{
		Counts:            index.Counts,
		NoReport:          index.NoReport,
		WithdrawalsLiveIn: WithdrawalsPath,
		GeneratedBy:       RegenerateWith,
	}
	for _, entry := range index.Entries {
		body, err := os.ReadFile(filepath.Join(benchRoot, "..", entry.Path))
		if err != nil {
			return Data{}, fmt.Errorf("reading %s: %w", entry.Path, err)
		}
		read, named := hand[entry.Path]
		if !named {
			return Data{}, fmt.Errorf("%s carries no headline figure in bench/report/handread.json: a report opens with a number and none may be invented here", entry.Path)
		}
		text := string(body)
		if !strings.Contains(plain(text), read.Figure) {
			return Data{}, fmt.Errorf("%s: the headline figure %q does not appear in the report it is taken from", entry.Path, read.Figure)
		}
		delete(hand, entry.Path)
		blocks := blocksOf(text)
		data.Reports = append(data.Reports, Report{
			Package:     entry.Package,
			Date:        entry.Date,
			Source:      entry.Path,
			Title:       entry.Title,
			Figure:      read.Figure,
			Says:        read.Says,
			BuiltFrom:   read.From,
			BuiltFromIs: read.From.Describe(),
			Sample:      read.Sample,
			Skips:       read.Skips,
			Conclusion:  quotable(entry),
			Unparsed:    entry.Conclusion == Unparsed,
			State:       entry.State,
			StateSource: entry.StateSource,
			StateNote:   entry.StateNote,
			Conditions:  conditionsOf(text),
			Arms:        armsOf(blocks),
			Body:        blocks,
		})
	}
	for path := range hand {
		return Data{}, fmt.Errorf("bench/report/handread.json names %s, which is not a dated report under bench/", path)
	}
	data.Dates = datesChart(data.Reports)
	return data, nil
}

func readHandRead(path string) (map[string]handRead, error) {
	var list []handRead
	if err := readJSON(path, &list); err != nil {
		return nil, err
	}
	byReport := make(map[string]handRead, len(list))
	for _, item := range list {
		switch item.From {
		case FromProse, FromRerun:
		default:
			return nil, fmt.Errorf("bench/report/handread.json: %s says it was built from %q, which is neither prose nor rerun", item.Report, item.From)
		}
		if item.Figure == "" || item.Says == "" || item.Sample == "" {
			return nil, fmt.Errorf("bench/report/handread.json: %s needs a figure, the sentence that says what it means, and the sample it rests on", item.Report)
		}
		byReport[item.Report] = item
	}
	return byReport, nil
}

func quotable(entry datedEntry) string {
	if entry.State == StateWithdrawn || entry.State == StateWithdrawnInPart {
		return "not quoted here: this report is " + string(entry.State) + ", and the note says by whom"
	}
	return entry.Conclusion
}

func conditionsOf(body string) []string {
	lines := strings.Split(body, "\n")
	headings := headingsOf(lines)
	if len(headings) < 2 {
		return nil
	}
	var said []string
	for _, line := range lines[headings[0].line+1 : headings[1].line] {
		if text := plain(strings.TrimSpace(line)); text != "" {
			said = append(said, text)
		}
	}
	return said
}

func armsOf(blocks []Block) *Block {
	for _, block := range blocks {
		if block.Kind == BlockTable && len(block.Head) > 1 && strings.Contains(strings.ToLower(block.Head[0]), "arm") {
			table := block
			return &table
		}
	}
	return nil
}

func datesChart(reports []Report) Chart {
	chart := Chart{Title: "dated reports written that day", Unit: "reports"}
	for i := len(reports) - 1; i >= 0; i-- {
		date := reports[i].Date
		if len(chart.Labels) > 0 && chart.Labels[len(chart.Labels)-1] == date {
			chart.Values[len(chart.Values)-1]++
			continue
		}
		chart.Labels = append(chart.Labels, date)
		chart.Values = append(chart.Values, 1)
	}
	return chart
}
