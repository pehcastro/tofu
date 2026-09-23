package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

func routedVerbs(t *testing.T) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
	if err != nil {
		t.Fatalf("parsing main.go: %v", err)
	}
	var verbs []string
	ast.Inspect(file, func(node ast.Node) bool {
		clause, ok := node.(*ast.CaseClause)
		if !ok {
			return true
		}
		for _, expr := range clause.List {
			literal, ok := expr.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				continue
			}
			verb, err := strconv.Unquote(literal.Value)
			if err != nil || strings.HasPrefix(verb, "-") {
				continue
			}
			verbs = append(verbs, verb)
		}
		return true
	})
	if len(verbs) == 0 {
		t.Fatal("no verb was found in main.go, so this test proves nothing")
	}
	return verbs
}

func TestUsageTextNamesEveryVerb(t *testing.T) {
	notInUsage := map[string]string{
		"help":    "help prints the usage text rather than appearing in it",
		"catalog": "catalog is a rename shim that prints where library went",
	}
	for _, verb := range routedVerbs(t) {
		if _, exempt := notInUsage[verb]; exempt {
			continue
		}
		if !strings.Contains(usage, "\n  "+verb+" ") {
			t.Errorf("tofu routes %q and the usage text does not name it", verb)
		}
	}
}
