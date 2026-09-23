package llm

import (
	"fmt"
	"slices"
	"strings"
)

type Effort string

const (
	EffortNone    Effort = "none"
	EffortMinimal Effort = "minimal"
	EffortLow     Effort = "low"
	EffortMedium  Effort = "medium"
	EffortHigh    Effort = "high"
	EffortXHigh   Effort = "xhigh"
	EffortMax     Effort = "max"
)

const EffortDefault = EffortMedium

func (e Effort) Thinks() bool { return e != "" && e != EffortNone }

func Efforts() []Effort {
	return []Effort{EffortNone, EffortMinimal, EffortLow, EffortMedium, EffortHigh, EffortXHigh, EffortMax}
}

func ParseEffort(raw string) (Effort, error) {
	effort := Effort(strings.TrimSpace(raw))
	if !slices.Contains(Efforts(), effort) {
		return "", fmt.Errorf("%q is no thinking effort: the levels are %s", raw, EffortList(Efforts()))
	}
	return effort, nil
}

func EffortList(levels []Effort) string {
	names := make([]string, len(levels))
	for index, level := range levels {
		names[index] = string(level)
	}
	return strings.Join(names, ", ")
}
