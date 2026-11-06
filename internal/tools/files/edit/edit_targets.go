package edit

import (
	"encoding/json"
	"fmt"
	"strings"

	"tofu/internal/tools/code/symbols"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/files/glob"
)

type EditChange struct {
	Path   string `json:"path"`
	Old    string `json:"old_string"`
	New    string `json:"new_string"`
	Symbol string `json:"symbol"`
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
		after, err := symbols.ReplaceDeclaration(root, target, change.Symbol, text, change.New)
		return after, "", err
	case change.Old == "":
		return text + change.New, "appended new_string to the end of " + target + ", since old_string is empty", nil
	case change.All && count > 1:
		return strings.ReplaceAll(text, change.Old, change.New), fmt.Sprintf("replaced %d occurrences of old_string in %s", count, target), nil
	}
	return glob.ReplaceOnce(text, target, change.Old, change.New)
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
