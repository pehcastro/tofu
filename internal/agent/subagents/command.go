package subagent

import (
	"os"
	"path"
	"slices"
	"strings"
)

func cleanedField(field string) string {
	return path.Clean(strings.ReplaceAll(field, `\`, "/"))
}

func toolName(field string) string {
	return strings.TrimSuffix(strings.ToLower(path.Base(field)), ".exe")
}

var recursesOnCurrentDirectory = map[string]bool{
	"gofmt":     true,
	"goimports": true,
}

var walksTheModuleWithNoPath = map[string]bool{
	"golangci-lint": true,
	"staticcheck":   true,
}

func namesTheWholeTree(tool, field string) bool {
	switch cleanedField(field) {
	case "...":
		return true
	case "all":
		return tool == "go"
	case ".":
		return recursesOnCurrentDirectory[tool]
	default:
		return false
	}
}

func walksWithoutAPath(tool string, rest []string) bool {
	if !walksTheModuleWithNoPath[tool] {
		return false
	}
	for _, field := range rest {
		if field != "run" && !strings.HasPrefix(field, "-") {
			return false
		}
	}
	return true
}

func treeWideRefusal(tool, field string) string {
	named := strings.TrimSpace(tool + " " + strings.ReplaceAll(field, "/", " "))
	return named + " is a tree-wide command: the orchestrator runs it, not a sub-agent"
}

func reachesOnlyHeldFolders(tool string, owns []string) bool {
	if slices.Contains(owns, "**") {
		return true
	}
	goFiles, anyHeld := tool == "go" || recursesOnCurrentDirectory[tool] || walksTheModuleWithNoPath[tool], false
	for queue := []string{"."}; len(queue) > 0; queue = queue[1:] {
		entries, err := os.ReadDir(queue[0])
		if err != nil {
			return false
		}
		reached, holds := false, false
		for _, entry := range entries {
			name, named := entry.Name(), path.Join(queue[0], entry.Name())
			switch {
			case entry.IsDir() && !strings.HasPrefix(name, ".") && !strings.HasPrefix(name, "_") && !slices.Contains([]string{"testdata", "vendor", "node_modules"}, name):
				queue = append(queue, named)
			case entry.IsDir(), goFiles && path.Ext(name) != ".go":
			default:
				mine, _ := Matches(named, owns)
				reached, holds = true, holds || mine
			}
		}
		if reached && !holds {
			return false
		}
		anyHeld = anyHeld || holds
	}
	return anyHeld
}
