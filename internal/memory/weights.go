package memory

import (
	"fmt"
	"strconv"
	"strings"

	library "tofu/library/memory"
)

type Weight struct {
	Precedence int   `json:"precedence"`
	ViewBytes  int   `json:"view_bytes"`
	PromotesTo Scope `json:"promotes_to,omitempty"`
}

const (
	weightsFile = "library/memory/scopes@1.yaml"
	scopeCount  = 4
)

func loadWeights() (map[Scope]Weight, error) {
	weights := map[Scope]Weight{}
	var scope Scope
	inScopes := false
	for i, raw := range strings.Split(strings.ReplaceAll(string(library.Scopes()), "\r\n", "\n"), "\n") {
		key, value, _ := strings.Cut(strings.TrimSpace(raw), ":")
		value = strings.TrimSpace(value)
		var err error
		switch indent := len(raw) - len(strings.TrimLeft(raw, " ")); {
		case key == "":
		case indent == 0:
			inScopes = key == "scopes"
		case !inScopes:
			err = fmt.Errorf("nothing is indented outside scopes")
		case indent == 2:
			scope, err = ParseScope(key)
			weights[scope] = Weight{}
		default:
			w := weights[scope]
			switch key {
			case "precedence":
				w.Precedence, err = strconv.Atoi(value)
			case "view_bytes":
				w.ViewBytes, err = strconv.Atoi(value)
			case "promotes_to":
				if value != "none" {
					w.PromotesTo, err = ParseScope(value)
				}
			default:
				err = fmt.Errorf("unknown field %q", key)
			}
			weights[scope] = w
		}
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", weightsFile, i+1, err)
		}
	}
	seen := map[int]bool{}
	for scope, w := range weights {
		if w.Precedence < 1 || w.Precedence > scopeCount || seen[w.Precedence] || w.ViewBytes < 1 {
			return nil, fmt.Errorf("%s: %s needs its own precedence from 1 to %d and a view of at least one byte", weightsFile, scope, scopeCount)
		}
		seen[w.Precedence] = true
	}
	if len(weights) != scopeCount {
		return nil, fmt.Errorf("%s weighs %d scopes, and there are %d", weightsFile, len(weights), scopeCount)
	}
	return weights, nil
}
