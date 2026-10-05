package rule

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"tofu/internal/subagent"
)

type Verb string

const (
	VerbNone    Verb = ""
	VerbDebug   Verb = "debug"
	VerbExplore Verb = "explore"
	VerbReview  Verb = "review"
	VerbWrite   Verb = "write"
)

func languageExtensions() map[string][]string {
	return map[string][]string{
		"c":          {".c", ".h"},
		"cpp":        {".cc", ".cpp", ".hpp"},
		"csharp":     {".cs"},
		"go":         {".go"},
		"java":       {".java"},
		"javascript": {".js", ".jsx"},
		"markdown":   {".md"},
		"python":     {".py", ".pyi"},
		"ruby":       {".rb"},
		"rust":       {".rs"},
		"shell":      {".sh", ".bash", ".ps1"},
		"svelte":     {".svelte"},
		"typescript": {".ts", ".tsx"},
		"vue":        {".vue"},
		"yaml":       {".yaml", ".yml"},
	}
}

type Role string

const (
	RoleAny          Role = ""
	RoleOrchestrator Role = "orchestrator"
	RoleSubAgent     Role = "sub-agent"
)

type Task struct {
	Text       string
	Paths      []string
	Verb       Verb
	Language   string
	Role       Role
	Frameworks []string
}

func (t Task) touchesTests() bool {
	notAWord := func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }
	for _, word := range strings.FieldsFunc(strings.ToLower(t.Text), notAWord) {
		if slices.Contains([]string{"test", "tests", "spec", "coverage", "e2e"}, word) {
			return true
		}
	}
	for _, held := range t.Paths {
		base := path.Base(held)
		if strings.HasSuffix(base, "_test.go") || strings.Contains(base, ".test.") || strings.Contains(base, ".spec.") {
			return true
		}
		for _, dir := range strings.Split(path.Dir(held), "/") {
			if slices.Contains([]string{"test", "tests", "__tests__"}, dir) {
				return true
			}
		}
	}
	return false
}

func KnownLanguage(name string) bool { return languageExtensions()[name] != nil }

func LanguageOf(file string) string {
	extension := strings.ToLower(path.Ext(file))
	for language, extensions := range languageExtensions() {
		if slices.Contains(extensions, extension) {
			return language
		}
	}
	return ""
}

type Trigger struct {
	condition  *regexp.Regexp
	scope      string
	languages  []string
	frameworks []string
	verb       Verb
	role       Role
}

func (t Trigger) AlwaysOn() bool {
	return t.condition == nil && t.scope == "" && t.languages == nil && t.frameworks == nil && t.verb == VerbNone && t.role == RoleAny
}

func anyOf(declared string, known []string, field, file, id string) ([]string, error) {
	if declared == "" {
		return nil, nil
	}
	values := strings.Split(declared, ",")
	for i, value := range values {
		values[i] = strings.TrimSpace(value)
		if !slices.Contains(known, values[i]) {
			return nil, fmt.Errorf("%s: rule %q declares the %s %q, and a %s is one of %s", file, id, field, values[i], field, strings.Join(known, ", "))
		}
	}
	return values, nil
}

type declaredTrigger struct {
	condition string
	scope     string
	language  string
	framework string
	task      string
	role      string
}

func newTrigger(d declaredTrigger, file, id string) (Trigger, error) {
	t := Trigger{scope: d.scope, verb: Verb(d.task), role: Role(d.role)}
	switch t.role {
	case RoleAny, RoleOrchestrator, RoleSubAgent:
	default:
		return Trigger{}, fmt.Errorf("%s: rule %q declares the role %q, and a role is %s or %s", file, id, d.role, RoleOrchestrator, RoleSubAgent)
	}
	if d.condition != "" {
		pattern, err := regexp.Compile(d.condition)
		if err != nil {
			return Trigger{}, fmt.Errorf("%s: rule %q declares the condition %q and it is not a regular expression: %v", file, id, d.condition, err)
		}
		t.condition = pattern
	}
	if d.scope != "" {
		if _, err := subagent.Matches("a/path/the/scope/is/tested/against", []string{d.scope}); err != nil {
			return Trigger{}, fmt.Errorf("%s: rule %q declares the scope %q and it is not a path glob: %v", file, id, d.scope, err)
		}
	}
	var err error
	if t.languages, err = anyOf(d.language, slices.Sorted(maps.Keys(languageExtensions())), "language", file, id); err != nil {
		return Trigger{}, err
	}
	if t.frameworks, err = anyOf(d.framework, knownFrameworks(), "framework", file, id); err != nil {
		return Trigger{}, err
	}
	switch t.verb {
	case VerbNone, VerbDebug, VerbExplore, VerbReview, VerbWrite:
	default:
		return Trigger{}, fmt.Errorf("%s: rule %q declares the task %q, and a task is %s, %s, %s or %s", file, id, d.task, VerbDebug, VerbExplore, VerbReview, VerbWrite)
	}
	return t, nil
}

func (t Trigger) firesFor(task Task) (bool, string) {
	if t.AlwaysOn() {
		return true, "always on, the rule declares no trigger"
	}
	var why []string
	if t.role != RoleAny {
		if task.Role != t.role {
			return false, fmt.Sprintf("the prompt is not the %s's", t.role)
		}
		why = append(why, fmt.Sprintf("the prompt is the %s's", t.role))
	}
	if t.condition != nil {
		found := t.condition.FindString(task.Text)
		if found == "" {
			return false, fmt.Sprintf("the condition %s matches nothing in the task", t.condition)
		}
		why = append(why, fmt.Sprintf("the condition %s matched %q", t.condition, found))
	}
	if t.scope != "" {
		reached := ""
		for _, p := range task.Paths {
			if scopeReaches(t.scope, p) {
				reached = p
				break
			}
		}
		if reached == "" {
			return false, fmt.Sprintf("the scope %s reaches none of the paths the task names", t.scope)
		}
		why = append(why, fmt.Sprintf("the scope %s reached %s", t.scope, reached))
	}
	if t.languages != nil {
		language, reached := "", ""
		for _, p := range task.Paths {
			if language = LanguageOf(p); slices.Contains(t.languages, language) {
				reached = p
				break
			}
		}
		if reached == "" && slices.Contains(t.languages, task.Language) {
			language, reached = task.Language, "the language the sub-agent declares"
		}
		if reached == "" {
			return false, fmt.Sprintf("no path the task names is %s", strings.Join(t.languages, " or "))
		}
		why = append(why, fmt.Sprintf("the language %s reached %s", language, reached))
	}
	if t.frameworks != nil {
		listed := slices.IndexFunc(t.frameworks, func(framework string) bool { return slices.Contains(task.Frameworks, framework) })
		if listed < 0 {
			return false, fmt.Sprintf("no package.json the task reaches lists %s", strings.Join(t.frameworks, " or "))
		}
		why = append(why, fmt.Sprintf("a package.json lists %s", t.frameworks[listed]))
	}
	if t.verb != VerbNone {
		if task.Verb != t.verb {
			return false, fmt.Sprintf("the task is %s rather than %s", cmp.Or(string(task.Verb), "unnamed"), t.verb)
		}
		why = append(why, fmt.Sprintf("the task is %s", t.verb))
	}
	return true, strings.Join(why, " and ")
}

func scopeReaches(scope, held string) bool {
	if !strings.Contains(held, "*") {
		matched, err := subagent.Matches(held, []string{scope})
		return err == nil && matched
	}
	var roster subagent.Roster
	var collision subagent.CollisionError
	return roster.Hold(subagent.SubAgent{ID: "scope", Owns: []string{scope}}) == nil &&
		errors.As(roster.Hold(subagent.SubAgent{ID: "owner", Owns: []string{held}}), &collision)
}

type Match struct {
	RuleID string
	Fires  bool
	Why    string
}

func (r Rule) forTheWriterOnly() bool {
	return r.Concern == ConcernCodeRules && r.Shapes == ShapeWriting && r.Trigger.role == RoleAny && (r.Trigger.languages != nil || r.Trigger.frameworks != nil)
}

func Index(rules []Rule, task Task) []Match {
	index := make([]Match, len(rules))
	for i, r := range rules {
		fires, why := r.Trigger.firesFor(task)
		if fires && task.Role == RoleOrchestrator && r.forTheWriterOnly() {
			fires, why = false, "a rule for how a language or a framework is written reaches the agent that writes that code, and the prompt is the orchestrator's"
		}
		index[i] = Match{RuleID: r.ID, Fires: fires, Why: why}
	}
	return index
}
