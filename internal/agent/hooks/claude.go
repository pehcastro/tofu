package hook

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

func claudeName(tool string) string {
	switch tool {
	case "bash":
		return "Bash"
	case "edit":
		return "Edit"
	case "write":
		return "Write"
	case "read":
		return "Read"
	case "glob":
		return "Glob"
	case "search":
		return "Grep"
	case "spawn":
		return "Agent"
	case "fetch":
		return "WebFetch"
	case "web_search":
		return "WebSearch"
	}
	return tool
}

func toolNames(tool string) []string {
	names := []string{tool, claudeName(tool)}
	switch tool {
	case "edit", "write":
		names = append(names, "apply_patch")
	case "spawn":
		names = append(names, "Task", "spawn_agent")
	}
	return names
}

func renamedKeys(tool string) [][2]string {
	switch tool {
	case "edit", "write", "read":
		return [][2]string{{"path", "file_path"}}
	case "spawn":
		return [][2]string{{"task", "prompt"}, {"agent", "subagent_type"}, {"mission", "description"}}
	}
	return nil
}

func (e *Engine) toClaude(tool string, args json.RawMessage) json.RawMessage {
	var fields map[string]json.RawMessage
	if json.Unmarshal(args, &fields) != nil {
		return args
	}
	for _, keys := range renamedKeys(tool) {
		value, held := fields[keys[0]]
		if !held {
			continue
		}
		delete(fields, keys[0])
		var path string
		if keys[0] == "path" && json.Unmarshal(value, &path) == nil && !filepath.IsAbs(path) {
			value, _ = json.Marshal(filepath.Join(e.project, path))
		}
		fields[keys[1]] = value
	}
	renamed, _ := json.Marshal(fields)
	return renamed
}

func (e *Engine) fromClaude(tool string, args json.RawMessage) (json.RawMessage, bool) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(args, &fields) != nil || fields == nil {
		return nil, false
	}
	for _, keys := range renamedKeys(tool) {
		value, held := fields[keys[1]]
		if !held {
			continue
		}
		delete(fields, keys[1])
		var path string
		if keys[0] == "path" && json.Unmarshal(value, &path) == nil && filepath.IsAbs(path) {
			if inside, err := filepath.Rel(e.project, path); err == nil && !strings.HasPrefix(inside, "..") {
				value, _ = json.Marshal(filepath.ToSlash(inside))
			}
		}
		fields[keys[0]] = value
	}
	renamed, _ := json.Marshal(fields)
	return renamed, true
}

func matcherOf(matcher string) (*regexp.Regexp, error) {
	if matcher == "" || matcher == "*" || plainMatcher(matcher) {
		return nil, nil
	}
	return regexp.Compile(matcher)
}

func plainMatcher(matcher string) bool {
	return regexp.MustCompile(`^[A-Za-z0-9_\-\s,|]*$`).MatchString(matcher)
}

func matches(matcher string, subjects []string) bool {
	if matcher == "" || matcher == "*" || subjects == nil {
		return true
	}
	if plainMatcher(matcher) {
		return slices.ContainsFunc(strings.FieldsFunc(matcher, func(r rune) bool { return r == '|' || r == ',' }), func(name string) bool {
			return slices.Contains(subjects, strings.TrimSpace(name))
		})
	}
	pattern, err := matcherOf(matcher)
	return err == nil && slices.ContainsFunc(subjects, pattern.MatchString)
}
