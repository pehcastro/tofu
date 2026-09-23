package rule

import (
	"cmp"
	"fmt"
	"io"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"

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
		"typescript": {".ts", ".tsx"},
		"yaml":       {".yaml", ".yml"},
	}
}

type Task struct {
	Text  string
	Paths []string
	Verb  Verb
}

type Trigger struct {
	condition *regexp.Regexp
	scope     string
	language  string
	verb      Verb
}

func (t Trigger) AlwaysOn() bool {
	return t.condition == nil && t.scope == "" && t.language == "" && t.verb == VerbNone
}

type declaredTrigger struct {
	condition string
	scope     string
	language  string
	task      string
}

func newTrigger(d declaredTrigger, file, id string) (Trigger, error) {
	t := Trigger{scope: d.scope, language: d.language, verb: Verb(d.task)}
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
	if known := languageExtensions(); d.language != "" && known[d.language] == nil {
		return Trigger{}, fmt.Errorf("%s: rule %q declares the language %q, and a language is one of %s", file, id, d.language, strings.Join(slices.Sorted(maps.Keys(known)), ", "))
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
			if matched, err := subagent.Matches(p, []string{t.scope}); err == nil && matched {
				reached = p
				break
			}
		}
		if reached == "" {
			return false, fmt.Sprintf("the scope %s reaches none of the paths the task names", t.scope)
		}
		why = append(why, fmt.Sprintf("the scope %s reached %s", t.scope, reached))
	}
	if t.language != "" {
		extensions := languageExtensions()[t.language]
		reached := ""
		for _, p := range task.Paths {
			if slices.Contains(extensions, strings.ToLower(path.Ext(p))) {
				reached = p
				break
			}
		}
		if reached == "" {
			return false, fmt.Sprintf("no path the task names is %s", t.language)
		}
		why = append(why, fmt.Sprintf("the language %s reached %s", t.language, reached))
	}
	if t.verb != VerbNone {
		if task.Verb != t.verb {
			return false, fmt.Sprintf("the task is %s rather than %s", cmp.Or(string(task.Verb), "unnamed"), t.verb)
		}
		why = append(why, fmt.Sprintf("the task is %s", t.verb))
	}
	return true, strings.Join(why, " and ")
}

type Match struct {
	RuleID string
	Fires  bool
	Why    string
}

func Index(rules []Rule, task Task) []Match {
	index := make([]Match, len(rules))
	for i, r := range rules {
		fires, why := r.Trigger.firesFor(task)
		index[i] = Match{RuleID: r.ID, Fires: fires, Why: why}
	}
	return index
}

func WriteIndex(w io.Writer, index []Match) {
	firing, widest := 0, 0
	for _, m := range index {
		if m.Fires {
			firing++
		}
		widest = max(widest, len(m.RuleID))
	}
	_, _ = fmt.Fprintf(w, "%d of %d rules fire\n", firing, len(index))
	for _, m := range index {
		mark := "     "
		if m.Fires {
			mark = "fires"
		}
		_, _ = fmt.Fprintf(w, "%s %-*s %s\n", mark, widest, m.RuleID, m.Why)
	}
}
