package subagent

import (
	"path"
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
	return named + " is a tree-wide command: the parent runs it, not a child"
}
