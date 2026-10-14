package guard

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf16"

	"tofu/internal/sys"
)

type Rule string

const (
	Emdash         Rule = "emdash"
	Comments       Rule = "comments"
	LibraryWording Rule = "library-wording"
	Shell          Rule = "shell"
	LocTrack       Rule = "loc-track"
	Snapshot       Rule = "snapshot"
	MinifyGate     Rule = "minify-gate"
	RequireTicket  Rule = "require-ticket"
	OwnPaths       Rule = "own-paths"
	TicketLog      Rule = "ticket-log"
	ResearchUsed   Rule = "research-used"
)

func Rules() []Rule {
	return []Rule{Emdash, Comments, LibraryWording, Shell, LocTrack, Snapshot, MinifyGate, RequireTicket, OwnPaths, TicketLog, ResearchUsed}
}

type Env struct {
	Root   string
	Temp   string
	Cache  string
	Boardy bool
}

type Answer struct {
	Code   int
	Stdout string
	Stderr string
}

type event struct {
	ToolName       string `json:"tool_name"`
	HookEventName  string `json:"hook_event_name"`
	SessionID      string `json:"session_id"`
	AgentID        string `json:"agent_id"`
	StopHookActive bool   `json:"stop_hook_active"`
	ToolInput      struct {
		FilePath     string `json:"file_path"`
		Content      string `json:"content"`
		OldString    string `json:"old_string"`
		NewString    string `json:"new_string"`
		ReplaceAll   bool   `json:"replace_all"`
		Command      string `json:"command"`
		Prompt       string `json:"prompt"`
		SubagentType string `json:"subagent_type"`
	} `json:"tool_input"`
}

func Hook(rule Rule, env Env, stdin []byte) (Answer, error) {
	if rule == Emdash {
		return emdash(stdin), nil
	}
	var ev event
	if json.Unmarshal(stdin, &ev) != nil {
		return Answer{}, nil
	}
	switch rule {
	case Comments:
		return comments(ev), nil
	case LibraryWording:
		return libraryWording(ev), nil
	case Shell:
		return shell(env, ev)
	case LocTrack:
		return Answer{}, locTrack(env, ev)
	case Snapshot:
		snapshot(env, ev)
		return Answer{}, nil
	case MinifyGate:
		return minifyGate(env, ev)
	case RequireTicket:
		return requireTicket(env, ev)
	case OwnPaths:
		return ownPaths(env, ev)
	case TicketLog:
		return ticketLog(env, ev)
	case ResearchUsed:
		return researchUsed(env, ev), nil
	}
	return Answer{}, fmt.Errorf("%q is not a rule: %v", rule, Rules())
}

func deny(reason string) Answer {
	out, _ := json.Marshal(map[string]map[string]string{"hookSpecificOutput": {
		"hookEventName":            "PreToolUse",
		"permissionDecision":       "deny",
		"permissionDecisionReason": reason,
	}})
	return Answer{Stdout: string(out) + "\n"}
}

func refuse(code int, text string) Answer {
	return Answer{Code: code, Stderr: text + "\n"}
}

func blank(s string) bool { return strings.TrimSpace(s) == "" }

func orDefault(s, fallback string) string {
	if blank(s) {
		return fallback
	}
	return s
}

func foldPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix)
}

func foldSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && strings.EqualFold(s[len(s)-len(suffix):], suffix)
}

func utf16Len(s string) int { return len(utf16.Encode([]rune(s))) }

var newline = sys.LazyRegexp(`\r?\n`)

const byteOrderMark = string(rune(0xFEFF))

func fileLines(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	text := strings.TrimSuffix(strings.TrimPrefix(string(data), byteOrderMark), "\n")
	if text == "" {
		return nil
	}
	return newline().Split(strings.TrimSuffix(text, "\r"), -1)
}

func appendLine(path, line string) error {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, err = file.WriteString(line + "\n")
	return errors.Join(err, file.Close())
}

func ownsGlobs(lines []string) []string {
	var globs []string
	inFront, inOwns := false, false
	for _, line := range lines {
		if line == "---" {
			if inFront {
				break
			}
			inFront = true
			continue
		}
		if !inFront {
			continue
		}
		if ownsKey().MatchString(line) {
			inOwns = true
			continue
		}
		if !inOwns {
			continue
		}
		if item := listItem().FindStringSubmatch(line); item != nil {
			globs = append(globs, strings.TrimSpace(item[1]))
		} else if line != "" && !unicode.IsSpace(rune(line[0])) {
			inOwns = false
		}
	}
	return globs
}

var (
	ownsKey  = sys.LazyRegexp(`(?i)^owns:\s*$`)
	listItem = sys.LazyRegexp(`^\s*-\s*(.+?)\s*$`)
)

func ownsMatch(rel string, globs []string) bool {
	for _, glob := range globs {
		pattern := regexp.QuoteMeta(glob)
		pattern = strings.ReplaceAll(pattern, `\*\*`, `.*`)
		pattern = strings.ReplaceAll(pattern, `\*`, `[^/]*`)
		if matched, _ := regexp.MatchString("(?i)^"+pattern+"$", rel); matched {
			return true
		}
	}
	return false
}

var drivePath = sys.LazyRegexp(`^[A-Za-z]:/`)

func relativePath(root, path string) string {
	normRoot := strings.ReplaceAll(strings.TrimRight(root, `\/`), `\`, "/")
	normPath := strings.ReplaceAll(path, `\`, "/")
	if foldPrefix(normPath, normRoot+"/") {
		return normPath[len(normRoot)+1:]
	}
	if drivePath().MatchString(normPath) || strings.HasPrefix(normPath, "//") {
		return ""
	}
	return normPath
}

func underRoot(root, path string) (rel string, inside bool) {
	full := path
	if !filepath.IsAbs(full) {
		full = filepath.Join(root, full)
	}
	full = filepath.Clean(full)
	rootFull := filepath.Clean(root)
	if !foldPrefix(full, rootFull) {
		return "", false
	}
	return relativePath(rootFull, full), true
}
