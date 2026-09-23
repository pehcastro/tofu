package tools

import (
	"fmt"
	"go/parser"
	"go/token"
	"slices"
	"strings"

	"tofu/internal/search"
	"tofu/internal/turn"
)

func goParse(shown, body string) error {
	_, err := parser.ParseFile(token.NewFileSet(), shown, body, parser.SkipObjectResolution)
	return err
}

func goStillParses(shown, before, after string) error {
	if !strings.HasSuffix(shown, ".go") || goParse(shown, before) != nil {
		return nil
	}
	if err := goParse(shown, after); err != nil {
		return fmt.Errorf("edit: this edit would leave %s unable to parse as go: %w. nothing was written, and %s parses as it stands", shown, err, shown)
	}
	return nil
}

func declarations(found []search.Definition) string {
	if len(found) == 0 {
		return "no declaration there carries that name"
	}
	named := make([]string, len(found))
	for i, definition := range found {
		named[i] = fmt.Sprintf("%s %s at lines %d to %d", definition.Kind, definition.Symbol, definition.FirstLine, definition.LastLine)
	}
	return strings.Join(named, " and ")
}

func replaceDeclaration(root turn.Root, shown, symbol, before, becomes string) (string, error) {
	if !strings.HasSuffix(shown, ".go") {
		return "", fmt.Errorf("edit: symbol names a go declaration and %s is not a go file: replace a stretch of text in it with old_string instead", shown)
	}
	parts := strings.Split(symbol, ".")
	if len(parts) > 2 || slices.ContainsFunc(parts, func(part string) bool { return !token.IsIdentifier(part) }) {
		return "", fmt.Errorf("edit: symbol %q is neither a declared name nor a receiver and a method name: it is spelled Resolve for a function, a type or a value, and Root.Resolve for a method", symbol)
	}
	graph, err := search.Symbols(string(root), []string{shown}, parts[len(parts)-1])
	if err != nil {
		return "", fmt.Errorf("edit: %w", err)
	}
	if graph.Unparsed > 0 {
		return "", fmt.Errorf("edit: %s does not parse as go, so no declaration in it can be named: repair it with old_string first", shown)
	}
	found := slices.DeleteFunc(slices.Clone(graph.Definitions), func(definition search.Definition) bool { return definition.Symbol != symbol })
	if len(found) > 1 {
		return "", fmt.Errorf("edit: %s declares %s %d times, as %s, and an edit names exactly one: replace the one you mean with old_string. nothing was changed",
			shown, symbol, len(found), declarations(found))
	}
	if len(found) == 0 {
		return "", fmt.Errorf("edit: %s declares no %s: %s. name a declaration it holds, or replace a stretch of text with old_string",
			shown, symbol, declarations(graph.Definitions))
	}

	lines := strings.Split(before, "\n")
	var text []string
	if trimmed := strings.Trim(becomes, "\n"); trimmed != "" {
		text = strings.Split(trimmed, "\n")
	}
	return strings.Join(slices.Concat(lines[:found[0].FirstLine-1], text, lines[found[0].LastLine:]), "\n"), nil
}
