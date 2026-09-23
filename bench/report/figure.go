package report

import (
	"fmt"
	"math"
	"regexp"
)

type Figure string

var bareCount = regexp.MustCompile(`^-?[0-9][0-9,]*(\.[0-9]+)?$`)

func (f Figure) IsBareCount() bool { return bareCount.MatchString(string(f)) }

func percentFigure(value float64) Figure { return Figure(fmt.Sprintf("%.1f%%", value)) }

func pointsFigure(value float64) Figure { return Figure(fmt.Sprintf("%.1f points", value)) }

func multipleFigure(one, other float64) Figure {
	high, low := math.Max(one, other), math.Min(one, other)
	if low == 0 {
		return Figure(fmt.Sprintf("no multiple: the lower method scored %s", percentFigure(low)))
	}
	return Figure(fmt.Sprintf("%.2fx", high/low))
}

func countFigure(count int, unit string) Figure {
	return Figure(fmt.Sprintf("%d %s", count, unit))
}
