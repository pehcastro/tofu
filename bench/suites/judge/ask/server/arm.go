package server

import "regexp"

type Script struct {
	Name    string
	Command string
}

type Fit string

const (
	FitAct Fit = "act"
	FitAsk Fit = "ask"
)

var startShape = regexp.MustCompile(`\b(dev|start|serve)\b`)

var word = regexp.MustCompile(`[a-zA-Z]+`)

func CandidatesByShape(scripts []Script) []string {
	var out []string
	for _, s := range scripts {
		if startShape.MatchString(s.Command) {
			out = append(out, s.Name)
		}
	}
	return out
}

func CandidatesByName(task string, scripts []Script) []string {
	words := map[string]bool{}
	for _, w := range word.FindAllString(task, -1) {
		words[w] = true
	}
	var out []string
	for _, s := range scripts {
		if words[s.Name] {
			out = append(out, s.Name)
		}
	}
	return out
}

func Decide(candidates []string) Fit {
	if len(candidates) == 1 {
		return FitAct
	}
	return FitAsk
}
