package api

import (
	"encoding/json"
	"fmt"

	"boji/bench/corpus"
)

type GateCase struct {
	Name  string
	State any
}

type SizeFixture struct {
	Label string
	State any
}

var gateCaseFiles = []string{
	"case-1-ls.json",
	"case-2-force-push-tests.json",
	"case-3-force-push-requested.json",
	"case-4-rm-rf.json",
	"case-5-curl-exfil-planted.json",
	"case-6-sed-named-file.json",
}

var sizeFixtureFiles = []struct{ label, file string }{
	{"300", "size-300.json"},
	{"1k", "size-1k.json"},
	{"4k", "size-4k.json"},
	{"12k", "size-12k.json"},
	{"28k", "size-28k.json"},
}

func GateCases() ([]GateCase, error) {
	cases := make([]GateCase, 0, len(gateCaseFiles))
	for _, name := range gateCaseFiles {
		state, err := loadState(name)
		if err != nil {
			return nil, err
		}
		cases = append(cases, GateCase{Name: name, State: state})
	}
	return cases, nil
}

func SizeFixtures() ([]SizeFixture, error) {
	fixtures := make([]SizeFixture, 0, len(sizeFixtureFiles))
	for _, entry := range sizeFixtureFiles {
		state, err := loadState(entry.file)
		if err != nil {
			return nil, err
		}
		fixtures = append(fixtures, SizeFixture{Label: entry.label, State: state})
	}
	return fixtures, nil
}

func loadState(name string) (any, error) {
	raw, err := corpus.Files().ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("bench/corpus: reading %s: %w", name, err)
	}
	var state any
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, fmt.Errorf("bench/corpus: %s is not valid JSON: %w", name, err)
	}
	return state, nil
}
