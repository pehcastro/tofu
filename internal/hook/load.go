package hook

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"maps"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"

	"tofu/internal/shell"
	"tofu/internal/sys"
)

type Event string

const (
	PreToolUse       Event = "PreToolUse"
	PostToolUse      Event = "PostToolUse"
	UserPromptSubmit Event = "UserPromptSubmit"
	Stop             Event = "Stop"
	SubagentStop     Event = "SubagentStop"
	SessionStart     Event = "SessionStart"
	SessionEnd       Event = "SessionEnd"
	GateVerdict      Event = "GateVerdict"
	SubagentSpawn    Event = "SubagentSpawn"
)

func (e Event) fired() bool {
	switch e {
	case PreToolUse, PostToolUse, UserPromptSubmit, Stop, SubagentStop, SessionStart, SessionEnd, GateVerdict, SubagentSpawn:
		return true
	}
	return false
}

type Level string

const (
	User    Level = "user"
	Project Level = "project"
	Local   Level = "local"
)

type Trust string

const (
	TrustUser    Trust = "user level"
	TrustTrusted Trust = "trusted"
	TrustOnce    Trust = "trusted for this turn"
	TrustNew     Trust = "not trusted"
	TrustChanged Trust = "changed since trusted"
	TrustRefused Trust = "refused"
)

func (t Trust) runs() bool { return t == TrustUser || t == TrustTrusted || t == TrustOnce }

type Answer int

const (
	AnswerAlways Answer = iota
	AnswerOnce
	AnswerRefuse
)

type Hook struct {
	Event   Event   `json:"event"`
	Matcher string  `json:"matcher"`
	Command string  `json:"command"`
	Shell   string  `json:"shell,omitempty"`
	Timeout int     `json:"timeout_seconds,omitempty"`
	File    string  `json:"file"`
	Level   Level   `json:"level"`
	Hash    string  `json:"hash"`
	Trust   Trust   `json:"trust"`
	Skipped string  `json:"skipped,omitempty"`
	Last    *Result `json:"last,omitempty"`
}

type Engine struct {
	project  string
	state    string
	hooks    []Hook
	problems []string
	bash     shell.Choice
	resolve  sync.Once
	bashErr  error
	mu       sync.Mutex
}

type handler struct {
	Type           string  `json:"type"`
	Command        string  `json:"command"`
	CommandWindows string  `json:"commandWindows"`
	Shell          string  `json:"shell"`
	Timeout        float64 `json:"timeout"`
	Async          bool    `json:"async"`
	If             string  `json:"if"`
}

type settingsFile struct {
	DisableAllHooks bool `json:"disableAllHooks"`
	Hooks           map[string][]struct {
		Matcher  string    `json:"matcher"`
		Handlers []handler `json:"hooks"`
	} `json:"hooks"`
}

type trustRecord struct {
	Hash    string    `json:"hash"`
	File    string    `json:"file"`
	Event   Event     `json:"event"`
	Command string    `json:"command"`
	Refused bool      `json:"refused,omitempty"`
	At      time.Time `json:"at"`
}

const (
	trustFile = "trusted.json"
	lastFile  = "last.json"
	slotsDir  = "slots"
)

func Load(project string, bash shell.Choice) *Engine {
	full, err := filepath.Abs(project)
	engine := &Engine{project: full, bash: bash}
	tofuHome, homeErr := sys.HomeConfigDir()
	if err = errors.Join(err, homeErr); err != nil {
		engine.problems = append(engine.problems, "no hook runs: "+err.Error())
		return engine
	}
	engine.state = filepath.Join(tofuHome, "hooks")
	home := filepath.Dir(tofuHome)
	var records []trustRecord
	engine.readJSON(trustFile, &records)
	last := map[string]*Result{}
	engine.readJSON(lastFile, &last)
	seen, disabledIn := map[string]string{}, ""
	for _, source := range []struct {
		path  string
		level Level
	}{
		{filepath.Join(home, ".claude", "settings.json"), User},
		{filepath.Join(home, ".codex", "hooks.json"), User},
		{filepath.Join(home, ".codex", "config.toml"), User},
		{filepath.Join(tofuHome, "hooks.json"), User},
		{filepath.Join(full, ".claude", "settings.json"), Project},
		{filepath.Join(full, ".codex", "hooks.json"), Project},
		{filepath.Join(full, ".codex", "config.toml"), Project},
		{filepath.Join(sys.StateDir(full), "hooks.json"), Project},
		{filepath.Join(full, ".claude", "settings.local.json"), Local},
	} {
		body, err := os.ReadFile(source.path)
		if errors.Is(err, fs.ErrNotExist) || slices.ContainsFunc(engine.hooks, func(held Hook) bool { return held.File == source.path }) {
			continue
		}
		var parsed settingsFile
		if err == nil {
			parsed, err = readSettings(source.path, body)
		}
		if err != nil {
			engine.problems = append(engine.problems, source.path+" was not read, so none of its hooks run: "+err.Error())
			continue
		}
		if parsed.DisableAllHooks {
			disabledIn = source.path
		}
		for _, event := range slices.Sorted(maps.Keys(parsed.Hooks)) {
			for _, group := range parsed.Hooks[event] {
				for _, given := range group.Handlers {
					hook := Hook{Event: Event(event), Matcher: group.Matcher, Command: given.Command, Shell: given.Shell,
						Timeout: int(math.Ceil(given.Timeout)), File: source.path, Level: source.level}
					if runtime.GOOS == "windows" && given.CommandWindows != "" {
						hook.Command = given.CommandWindows
					}
					hook.Skipped = skipReason(hook, given)
					identity := strings.Join([]string{event, hook.Matcher, hook.Command, hook.Shell}, "\x00")
					if first, twice := seen[identity]; twice && hook.Skipped == "" {
						hook.Skipped = "the same handler as in " + first + ", so it runs once"
					} else if !twice {
						seen[identity] = source.path
					}
					hook.Hash = engine.hash(hook)
					hook.Trust = trustOf(hook, records)
					hook.Last = last[hook.Hash]
					engine.hooks = append(engine.hooks, hook)
				}
			}
		}
	}
	if disabledIn != "" {
		for i := range engine.hooks {
			engine.hooks[i].Skipped = "disableAllHooks is set in " + disabledIn
		}
	}
	return engine
}

func readSettings(path string, body []byte) (settingsFile, error) {
	var parsed settingsFile
	if filepath.Ext(path) != ".toml" {
		return parsed, json.Unmarshal(body, &parsed)
	}
	var codex struct {
		Hooks map[string]any `toml:"hooks"`
	}
	if _, err := toml.Decode(string(body), &codex); err != nil {
		return parsed, err
	}
	delete(codex.Hooks, "state")
	asJSON, err := json.Marshal(map[string]any{"hooks": codex.Hooks})
	if err == nil {
		err = json.Unmarshal(asJSON, &parsed)
	}
	return parsed, err
}

func skipReason(hook Hook, given handler) string {
	_, badMatcher := matcherOf(hook.Matcher)
	switch {
	case !hook.Event.fired():
		return "tofu does not fire " + string(hook.Event) + " yet"
	case given.Type != "command" && given.Type != "":
		return given.Type + " hooks are not run by tofu yet"
	case given.Async:
		return "async hooks are not run by tofu yet"
	case given.If != "":
		return "the if field is not read by tofu yet, so this hook does not run rather than run more often than its author meant"
	case strings.TrimSpace(hook.Command) == "":
		return "it names no command"
	case strings.TrimSpace(hook.Command) == "rtk hook claude":
		return "tofu runs the rtk rewrite itself"
	case hook.Shell != "" && hook.Shell != "bash" && hook.Shell != "powershell":
		return "its shell " + hook.Shell + " is neither bash nor powershell"
	case badMatcher != nil:
		return "its matcher is not a regular expression tofu reads: " + badMatcher.Error()
	}
	return ""
}

func (e *Engine) Hooks() []Hook { return slices.Clone(e.hooks) }

func (e *Engine) Problems() []string { return slices.Clone(e.problems) }

func (e *Engine) Untrusted() []Hook {
	return slices.DeleteFunc(e.Hooks(), func(hook Hook) bool {
		return hook.Skipped != "" || (hook.Trust != TrustNew && hook.Trust != TrustChanged)
	})
}

func (e *Engine) Answer(hooks []Hook, answer Answer) error {
	var trust Trust
	switch answer {
	case AnswerAlways:
		trust = TrustTrusted
	case AnswerOnce:
		trust = TrustOnce
	case AnswerRefuse:
		trust = TrustRefused
	default:
		panic("hook: unknown answer")
	}
	var records []trustRecord
	e.readJSON(trustFile, &records)
	for _, hook := range hooks {
		for i := range e.hooks {
			if e.hooks[i].Hash == hook.Hash {
				e.hooks[i].Trust = trust
			}
		}
		records = append(slices.DeleteFunc(records, func(old trustRecord) bool { return sameHandler(old, hook) }),
			trustRecord{Hash: hook.Hash, File: hook.File, Event: hook.Event, Command: hook.Command, Refused: answer == AnswerRefuse, At: time.Now()})
	}
	if answer == AnswerOnce || len(hooks) == 0 {
		return nil
	}
	body, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	return sys.WriteFile(filepath.Join(e.state, trustFile), body, 0o644)
}

func sameHandler(record trustRecord, hook Hook) bool {
	return record.File == hook.File && record.Event == hook.Event && record.Command == hook.Command
}

func trustOf(hook Hook, records []trustRecord) Trust {
	at := slices.IndexFunc(records, func(record trustRecord) bool { return record.Hash == hook.Hash })
	switch {
	case hook.Level == User:
		return TrustUser
	case at >= 0 && records[at].Refused:
		return TrustRefused
	case at >= 0:
		return TrustTrusted
	case slices.ContainsFunc(records, func(record trustRecord) bool { return !record.Refused && sameHandler(record, hook) }):
		return TrustChanged
	}
	return TrustNew
}

func (e *Engine) hash(hook Hook) string {
	sum := sha256.New()
	described, _ := json.Marshal([]any{hook.File, hook.Event, hook.Matcher, hook.Command, hook.Shell, hook.Timeout})
	sum.Write(described)
	expanded := hook.Command
	for _, name := range []string{"${CLAUDE_PROJECT_DIR}", "$CLAUDE_PROJECT_DIR", "%CLAUDE_PROJECT_DIR%"} {
		expanded = strings.ReplaceAll(expanded, name, e.project)
	}
	for _, token := range regexp.MustCompile(`"[^"]*"|'[^']*'|\S+`).FindAllString(expanded, -1) {
		path := strings.Trim(token, `"'`)
		if !filepath.IsAbs(path) {
			path = filepath.Join(e.project, path)
		}
		inside, err := filepath.Rel(e.project, path)
		if err != nil || strings.HasPrefix(inside, "..") || !aScript(path) {
			continue
		}
		if body, err := os.ReadFile(path); err == nil {
			sum.Write([]byte("\x00" + filepath.ToSlash(inside) + "\x00"))
			sum.Write(body)
		}
	}
	return hex.EncodeToString(sum.Sum(nil))
}

func aScript(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".sh", ".bash", ".ps1", ".psm1", ".py", ".js", ".mjs", ".cjs", ".ts", ".rb", ".pl", ".bat", ".cmd", ".exe":
		return true
	}
	return false
}

func (e *Engine) readJSON(name string, into any) {
	body, err := os.ReadFile(filepath.Join(e.state, name))
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err == nil {
		err = json.Unmarshal(body, into)
	}
	if err != nil {
		e.problems = append(e.problems, filepath.Join(e.state, name)+" was not read: "+err.Error())
	}
}
