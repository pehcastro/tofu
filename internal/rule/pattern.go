package rule

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"tofu/internal/subagent"
)

const CheckerPattern = "pattern"

func (r Rule) watches(path string) bool {
	if r.Pattern == nil || r.Mode == ModeOff || slices.ContainsFunc(strings.Split(path, "/"), skipTreeDir) {
		return false
	}
	if r.Trigger.scope != "" && !ScopeReaches(r.Trigger.scope, path) {
		return false
	}
	listed, err := subagent.Matches(path, r.Paths)
	if err != nil || r.Paths != nil && !listed {
		return false
	}
	excepted, err := subagent.Matches(path, r.ExceptPaths)
	return err == nil && !excepted
}

func (r Rule) matches(path, content string) []Finding {
	var found []Finding
	for _, at := range r.Pattern.FindAllStringIndex(content, -1) {
		start := strings.LastIndexByte(content[:at[0]], '\n') + 1
		end := len(content)
		if next := strings.IndexByte(content[at[0]:], '\n'); next >= 0 {
			end = at[0] + next
		}
		found = append(found, Finding{
			RuleID:  r.ID,
			Target:  fmt.Sprintf("%s:%d", path, 1+strings.Count(content[:at[0]], "\n")),
			Detail:  strings.TrimSpace(content[start:end]),
			Matched: content[at[0]:at[1]],
		})
	}
	return found
}

func CheckWrite(rules []Rule, path, before, after string) []Fire {
	path = filepath.ToSlash(path)
	var fires []Fire
	for _, r := range rules {
		if !r.watches(path) {
			continue
		}
		held := map[string]int{}
		for _, old := range r.matches(path, before) {
			held[old.Matched]++
		}
		var added []Finding
		for _, found := range r.matches(path, after) {
			if held[found.Matched] > 0 {
				held[found.Matched]--
				continue
			}
			added = append(added, found)
		}
		if len(added) > 0 {
			fires = append(fires, Fire{RuleID: r.ID, Mode: r.Mode, Text: r.Text, Target: path, Findings: added, Blocked: r.Mode == ModeEnforced})
		}
	}
	return fires
}

func CheckFiles(rules []Rule, root string, files []string, now time.Time) ([]Fire, error) {
	var fires []Fire
	for _, file := range files {
		path := filepath.ToSlash(file)
		if !slices.ContainsFunc(rules, func(r Rule) bool { return r.watches(path) }) {
			continue
		}
		data, err := os.ReadFile(file)
		if errors.Is(err, fs.ErrNotExist) || bytes.IndexByte(data, 0) >= 0 {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, f := range CheckWrite(rules, path, "", string(data)) {
			at := slices.IndexFunc(fires, func(held Fire) bool { return held.RuleID == f.RuleID })
			if at < 0 {
				f.Target, f.At = root, now
				fires = append(fires, f)
				continue
			}
			fires[at].Findings = append(fires[at].Findings, f.Findings...)
		}
	}
	return fires, nil
}

func (f Fire) Lines() []Finding {
	return slices.CompactFunc(slices.Clone(f.Findings), func(a, b Finding) bool { return a.Target == b.Target })
}

func (f Fire) said() string {
	var found []string
	for _, one := range f.Lines() {
		found = append(found, fmt.Sprintf("%s `%s`", one.Target, one.Detail))
	}
	return fmt.Sprintf("rule %s (%s): %s. the rule: %s", f.RuleID, f.Mode, strings.Join(found, "; "), f.Text)
}

type Refusal struct{ Fires []Fire }

func (r Refusal) Error() string {
	said := make([]string, len(r.Fires))
	for i, f := range r.Fires {
		said[i] = f.said()
	}
	return "an enforced rule refuses this write, so nothing was written: " + strings.Join(said, "\n")
}

func Weigh(fires []Fire) ([]string, error) {
	var notes []string
	var refused Refusal
	for _, f := range fires {
		if f.Blocked {
			refused.Fires = append(refused.Fires, f)
		} else {
			notes = append(notes, "written, and recorded for "+f.said())
		}
	}
	if refused.Fires != nil {
		return nil, refused
	}
	return notes, nil
}
