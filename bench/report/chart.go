package report

import (
	"regexp"
	"strconv"
	"strings"
)

const (
	chartMinRows     = 5
	chartMinDistinct = 3
)

var numberCell = regexp.MustCompile(`^(\$?)(-?[0-9][0-9,]*(?:\.[0-9]+)?)\s*(%|percent|ms|s|x|times|bytes|tokens|KB|MB)?$`)

type Chart struct {
	Title  string    `json:"title"`
	Unit   string    `json:"unit"`
	Labels []string  `json:"labels"`
	Values []float64 `json:"values"`
}

func chartFor(table Block) (Chart, bool) {
	if len(table.Rows) < chartMinRows || len(table.Head) < 2 {
		return Chart{}, false
	}
	for column := 1; column < len(table.Head); column++ {
		values, unit, ok := numericColumn(table, column)
		if !ok || distinct(values) < chartMinDistinct {
			continue
		}
		labels := make([]string, 0, len(table.Rows))
		for _, row := range table.Rows {
			labels = append(labels, row[0])
		}
		return Chart{Title: table.Head[column], Unit: unit, Labels: labels, Values: values}, true
	}
	return Chart{}, false
}

func numericColumn(table Block, column int) ([]float64, string, bool) {
	values := make([]float64, 0, len(table.Rows))
	unit := ""
	for _, row := range table.Rows {
		if column >= len(row) {
			return nil, "", false
		}
		value, cellUnit, ok := numberOf(row[column])
		if !ok || (len(values) > 0 && cellUnit != unit) {
			return nil, "", false
		}
		unit = cellUnit
		values = append(values, value)
	}
	return values, unit, true
}

func numberOf(cell string) (float64, string, bool) {
	match := numberCell.FindStringSubmatch(strings.TrimSpace(cell))
	if match == nil {
		return 0, "", false
	}
	value, err := strconv.ParseFloat(strings.ReplaceAll(match[2], ",", ""), 64)
	if err != nil {
		return 0, "", false
	}
	return value, match[1] + match[3], true
}

func distinct(values []float64) int {
	seen := map[float64]bool{}
	for _, value := range values {
		seen[value] = true
	}
	return len(seen)
}
