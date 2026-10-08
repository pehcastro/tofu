package learn

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

const groupingTask = `Below are the messages one person typed to a coding agent run by tofu, a coding-agent harness, across many sessions, each with the end of what the agent said just before it. Find what kept going wrong.

Group the messages that correct the agent, complain, or report a problem about the same thing. A message goes in one group at most. Leave out a message that only asks for new work, answers, approves or chats. A group may hold one message.

Class each group as one of:
- defect: tofu itself misbehaved, such as a lost message or picture, a screen, a command or a tool that failed or did the wrong thing. A bug report for tofu's author.
- library: the agent's built-in behaviour should change for everyone, such as stopping to ask what it could decide, handing back work it could run, or reporting work it did not check.
- personal: how this person wants the agent to work, a preference not everyone shares.
- setting: one of the settings listed at the end would fix it; name its key and the value.
- project: about the thing being built, its design or its bugs, not about how tofu or the agent behaved.

For each group write:
- title: what went wrong, in plain words, one line under 100 characters, quoting no one; call the person "you".
- rule: for library, personal and setting, the one instruction to the agent that would prevent it, such as "Run a finished build and open it without being asked.", in plain words, under 160 characters, never copying the person's words; it never mentions the person, the user or the owner. Empty for defect and project.
- reason: one short sentence on why it is that class.
- fixed_in: a version from the release notes only when one of its notes says it fixed exactly this; otherwise empty.

Reply with JSON only, in this shape:
{"findings": [{"messages": [3, 17], "title": "", "rule": "", "class": "personal", "reason": "", "fixed_in": "", "setting": "", "value": ""}]}`

type grouped struct {
	Findings []struct {
		Messages []int  `json:"messages"`
		Title    string `json:"title"`
		Rule     string `json:"rule"`
		Class    Class  `json:"class"`
		Reason   string `json:"reason"`
		FixedIn  string `json:"fixed_in"`
		Setting  string `json:"setting"`
		Value    string `json:"value"`
	} `json:"findings"`
}

func (r Run) offered(releases []Release) []Release {
	if len(r.Said) == 0 {
		return nil
	}
	least, _ := builtOn(releases, r.Said[0].At)
	return releases[:least]
}

func clipped(text string, bytes int) string {
	text = flat(text)
	if len(text) <= bytes {
		return text
	}
	for !utf8.RuneStart(text[bytes]) {
		bytes--
	}
	return text[:bytes] + " ..."
}

func (r Run) Prompt(known Known) string {
	var prompt strings.Builder
	prompt.WriteString(groupingTask + "\n\nThe messages:\n")
	for i, s := range r.Said {
		fmt.Fprintf(&prompt, "\n[%d] %s, session %s\nthe agent before: %s\nthe person: %s\n", i, s.At.Local().Format("2006-01-02 15:04"), s.Session, clipped(s.Before, PromptBeforeBytes), clipped(s.Text, PromptMessageBytes))
	}
	prompt.WriteString("\nRelease notes of the tofu versions out since the first message, newest first:\n")
	for _, release := range r.offered(known.Releases) {
		for _, note := range release.Notes {
			fmt.Fprintf(&prompt, "- %s, %s: %s\n", release.Version, release.Day, clipped(note, PromptNoteBytes))
		}
	}
	prompt.WriteString("\nSettings tofu has:\n")
	keys := make([]string, 0, len(known.Settings))
	for key := range known.Settings {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		fmt.Fprintf(&prompt, "- %s: %s\n", key, clipped(known.Settings[key], PromptBeforeBytes))
	}
	return prompt.String()
}

func (r *Run) Group(answer string, known Known, by string) error {
	from, to := strings.Index(answer, "{"), strings.LastIndex(answer, "}")
	if from < 0 || to < from {
		return errors.New("the model's reply holds no JSON object")
	}
	var reply grouped
	if err := json.Unmarshal([]byte(answer[from:to+1]), &reply); err != nil {
		return fmt.Errorf("the model's reply is not the findings asked for: %w", err)
	}
	offered := r.offered(known.Releases)
	refuse := func(n int, why string) {
		r.Sent.Dropped++
		if len(r.Sent.Refused) < ShownRefusals {
			r.Sent.Refused = append(r.Sent.Refused, fmt.Sprintf("group %d: %s", n+1, why))
		}
	}
	used := map[int]bool{}
	var found []Finding
	sent := requests{}
	for n, g := range reply.Findings {
		_, setting := known.Settings[g.Setting]
		title := flat(g.Title)
		switch {
		case !slices.Contains(classes, g.Class):
			refuse(n, "no class "+string(g.Class))
			continue
		case g.Class == ClassSetting && (!setting || g.Value == ""):
			refuse(n, "no setting "+g.Setting)
			continue
		case lineFault(title) != "":
			refuse(n, "the title was refused: "+lineFault(title))
			continue
		}
		var units []unit
		for _, m := range g.Messages {
			if m < 0 || m >= len(r.Said) || used[m] {
				refuse(n, fmt.Sprintf("message %d is not one to use", m))
				continue
			}
			used[m] = true
			units = append(units, unit{said: m, text: r.Said[m].Text})
		}
		if len(units) == 0 {
			refuse(n, "no message left")
			continue
		}
		f := findingOf(units, r.Said, nil, sent)
		f.Class, f.Title, f.Reason, f.WrittenBy = g.Class, title, clipped(g.Reason, StatementBytes), by
		if g.Class == ClassSetting {
			f.Setting, f.Value = g.Setting, g.Value
		}
		rule := strings.Trim(strings.TrimSpace(g.Rule), `"`)
		fault := ruleFault(rule, f.Quotes)
		switch {
		case rule == "":
		case g.Class == ClassDefect || g.Class == ClassProject:
			refuse(n, "a "+string(g.Class)+" carries no rule")
		case fault != "":
			refuse(n, "the rule was refused: "+fault)
		default:
			f.Rule = rule
		}
		if slices.ContainsFunc(offered, func(release Release) bool { return release.Version == g.FixedIn }) {
			f.offered = g.FixedIn
		} else if g.FixedIn != "" {
			refuse(n, "no release "+g.FixedIn+" was offered")
		}
		found = append(found, f)
	}
	r.Mode, r.Model = ModeModel, by
	r.settle(found, known)
	return nil
}
