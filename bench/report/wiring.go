package report

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"tofu/internal/judge/method"
)

const (
	CallersPath = "bench/report/callers.json"
	NotWired    = "not wired"
)

type caller struct {
	Point   string `json:"point"`
	File    string `json:"file,omitempty"`
	Symbol  string `json:"symbol,omitempty"`
	Nothing string `json:"nothing_calls_it_because,omitempty"`
}

type Wiring struct {
	Point      string `json:"point"`
	On         bool   `json:"on"`
	SwitchedOn string `json:"switched_on"`
	Costs      string `json:"costs,omitempty"`
	Why        string `json:"why"`
	Measured   string `json:"measured,omitempty"`
	CalledBy   string `json:"called_by,omitempty"`
}

func readCallers(path string) (map[string]caller, error) {
	var list []caller
	if err := readJSON(path, &list); err != nil {
		return nil, err
	}
	byPoint := make(map[string]caller, len(list))
	for _, item := range list {
		byPoint[item.Point] = item
	}
	return byPoint, nil
}

func checkCallers(table method.Table, callers map[string]caller, tree string) error {
	for _, item := range callers {
		chosen, err := table.Of(item.Point)
		if err != nil {
			return fmt.Errorf("%s names %s and %s names no such decision", CallersPath, item.Point, table.File)
		}
		if chosen.Method == method.Unwired {
			return fmt.Errorf("%s says what calls %s and %s:%d leaves it unwired, so nothing calls it", CallersPath, item.Point, table.File, chosen.Line)
		}
		if item.File == "" {
			if item.Nothing == "" {
				return fmt.Errorf("%s: %s names no caller and does not say why nothing calls it", CallersPath, item.Point)
			}
			continue
		}
		body, err := os.ReadFile(filepath.Join(tree, filepath.FromSlash(item.File)))
		if err != nil {
			return fmt.Errorf("%s: %s says %s calls it, which is not in the tree", CallersPath, item.Point, item.File)
		}
		if !strings.Contains(string(body), item.Symbol) {
			return fmt.Errorf("%s: %s says %s calls it at %s, and that file never names %s", CallersPath, item.Point, item.File, item.Symbol, item.Symbol)
		}
	}
	for _, chosen := range table.Choices {
		if _, named := callers[chosen.Point]; !named && chosen.Method != method.Unwired {
			return fmt.Errorf("%s:%d says %s decides %s and %s does not say what calls it", table.File, chosen.Line, chosen.Method, chosen.Point, CallersPath)
		}
	}
	return nil
}

func wiringOf(table method.Table, callers map[string]caller) []Wiring {
	rows := make([]Wiring, 0, len(table.Choices))
	for _, chosen := range table.Choices {
		called := callers[chosen.Point]
		row := Wiring{Point: chosen.Point, SwitchedOn: NotWired, Costs: "nothing, and no call is made", Why: chosen.Why, Measured: chosen.Measured, CalledBy: called.File}
		switch {
		case chosen.Method == method.Unwired:
		case called.File == "":
			row.SwitchedOn = NotWired + ": " + called.Nothing
		default:
			row.On, row.SwitchedOn, row.Costs = true, "yes, through "+chosen.Method.String(), chosen.Cost
		}
		rows = append(rows, row)
	}
	return rows
}

func wiringByPoint(rows []Wiring) map[string]Wiring {
	byPoint := make(map[string]Wiring, len(rows))
	for _, row := range rows {
		byPoint[row.Point] = row
	}
	return byPoint
}
