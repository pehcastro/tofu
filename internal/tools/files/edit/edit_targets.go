package edit

import (
	"encoding/json"
	"fmt"
	"go/token"
	"slices"
	"strings"

	"tofu/internal/tools/code/search/engine"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/files/glob"
)

type Insert string

const (
	InPlace Insert = ""
	Before  Insert = "before"
	After   Insert = "after"
)

type EditChange struct {
	Path   string `json:"path"`
	Old    string `json:"old_string"`
	New    string `json:"new_string"`
	Symbol string `json:"symbol"`
	Insert Insert `json:"insert"`
	All    bool   `json:"replace_all"`
}

func EditChanges(raw json.RawMessage) ([]EditChange, error) {
	var args struct {
		EditChange
		Edits []EditChange `json:"edits"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("edit: arguments are not the expected shape: %w", err)
	}
	if args.Old == "" && args.Symbol == "" && len(args.Edits) > 0 {
		return args.Edits, nil
	}
	return append([]EditChange{args.EditChange}, args.Edits...), nil
}

func Apply(root contract.Root, target, text string, change EditChange) (string, string, error) {
	switch count := strings.Count(text, change.Old); {
	case change.Symbol != "":
		after, err := atDeclaration(root, target, text, change)
		return after, "", err
	case change.Old == "":
		return text + change.New, "appended new_string to the end of " + target + ", since old_string is empty", nil
	case change.All && count > 1:
		return strings.ReplaceAll(text, change.Old, change.New), fmt.Sprintf("replaced %d occurrences of old_string in %s", count, target), nil
	}
	return glob.ReplaceOnce(text, target, change.Old, change.New)
}

func atDeclaration(root contract.Root, target, before string, change EditChange) (string, error) {
	parts := strings.Split(change.Symbol, ".")
	if len(parts) > 2 || slices.ContainsFunc(parts, func(part string) bool { return !token.IsIdentifier(part) }) {
		return "", fmt.Errorf("edit: symbol %q is neither a declared name nor a type and a method name: it is spelled total for a function, a type or a value, and Order.total for a method", change.Symbol)
	}
	graph := engine.Index(string(root), []string{target}, func(string) bool { return true })
	switch {
	case graph.Unreadable > 0:
		return "", fmt.Errorf("edit: could not read %s, so no declaration in it can be named. nothing was changed", graph.FirstFailed)
	case graph.Unparsed > 0:
		return "", fmt.Errorf("edit: %s does not parse, so no declaration in it can be named: repair it with old_string first", target)
	case graph.Parsed == 0:
		return "", fmt.Errorf("edit: symbol names a declaration in a .go, .rs, .ts, .tsx or .py file and %s is none of those: replace a stretch of text in it with old_string instead", target)
	}
	found := slices.DeleteFunc(slices.Clone(graph.Definitions), func(definition engine.Definition) bool { return definition.Symbol != change.Symbol })
	if len(found) == 0 {
		return "", fmt.Errorf("edit: %s declares no %s: %s. name a declaration it holds, or replace a stretch of text with old_string", target, change.Symbol, engine.Closest(change.Symbol, graph.Definitions))
	}
	if len(found) > 1 {
		named := make([]string, len(found))
		for i, definition := range found {
			named[i] = fmt.Sprintf("%s %s at lines %d to %d", definition.Kind, definition.Symbol, definition.FirstLine, definition.LastLine)
		}
		return "", fmt.Errorf("edit: %s declares %s %d times, as %s, and an edit names exactly one: replace the one you mean with old_string. nothing was changed",
			target, change.Symbol, len(found), strings.Join(named, " and "))
	}
	lines := strings.Split(before, "\n")
	first, last := found[0].FirstLine-1, found[0].LastLine
	var with []string
	if becomes := strings.Trim(change.New, "\n"); becomes != "" {
		with = strings.Split(becomes, "\n")
	}
	switch change.Insert {
	case InPlace:
	case Before:
		marks := []string{"//", "/*", "*"}
		if strings.HasSuffix(target, ".py") {
			marks = []string{"#"}
		}
		for first > 0 && slices.ContainsFunc(marks, func(mark string) bool { return strings.HasPrefix(strings.TrimSpace(lines[first-1]), mark) }) {
			first--
		}
		with, last = append(with, ""), first
	case After:
		with, first = append([]string{""}, with...), last
	default:
		return "", fmt.Errorf("edit: insert is %q, and it is before or after", change.Insert)
	}
	return strings.Join(slices.Concat(lines[:first], with, lines[last:]), "\n"), nil
}

func TargetPaths(tool string, raw json.RawMessage) ([]string, error) {
	if tool != "edit" {
		var one EditChange
		if err := json.Unmarshal(raw, &one); err != nil {
			return nil, fmt.Errorf("%s: arguments are not the expected shape: %w", tool, err)
		}
		return []string{one.Path}, nil
	}
	changes, err := EditChanges(raw)
	paths := make([]string, len(changes))
	for i, change := range changes {
		paths[i] = change.Path
	}
	return paths, err
}

func NoneWritten(changes int) string {
	if changes < 2 {
		return ""
	}
	return ". no file in this call was written"
}
