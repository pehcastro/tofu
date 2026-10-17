package learn

import (
	"cmp"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"tofu/internal/learn/words"
	"tofu/internal/memory"
	"tofu/internal/session"
	"tofu/internal/sys"
)

var (
	askedPerson = sys.LazyRegexp(`(?i)(decision for you|your call|you need to decide|until you decide|what i need from you|tell me (if|when|which|whether)|should i (start|do|go|build)|do you want|want me to|let me know|still open for you|\?\s*$)`)
	turnEnded   = sys.LazyRegexp(`(?i)\byou should be the one\b|\bwhy (?:u |you )?(?:idle|stopp?ed|stop|waiting|ask)\b|\bcan't i just\b|\byou can just\b|\byour job\b|\bkeep working\b`)
	directing   = sys.LazyRegexp(`(?i)\b(?:yourself|you should|your job|you can just|i want|must)\b`)
	toTheLead   = strings.NewReplacer(" yourself ", " itself ", " you are ", " the lead is ", " you're ", " the lead is ", " your ", " the lead's ", " you ", " the lead ", " u ", " the lead ", " lol ", " ", " pls ", " ", " please ", " ", " ok ", " ")
)

func localFindings(said []Said, sent requests) []Finding {
	texts := make([]string, len(said))
	for i, s := range said {
		texts[i] = s.Text
	}
	units := words.Units(texts)
	rare := words.Telling(units, len(said))
	var asked []words.Unit
	for _, u := range units {
		if words.Corrects(u.Text) && turnEnded().MatchString(u.Text) && askedPerson().MatchString(said[u.Said].Before) && !slices.ContainsFunc(asked, func(a words.Unit) bool { return a.Said == u.Said }) {
			asked = append(asked, u)
		}
	}
	var found []Finding
	for _, theme := range words.Themes(units, rare) {
		if !theme.Corrects {
			f := findingOf(theme.Units, said, rare, sent)
			f.Class, f.byWords = ClassProject, true
			f.Title = "You said about the project: " + strings.Join(f.words[:min(len(f.words), ShownWords)], ", ")
			f.Reason = "a remark that names no correction of the lead"
			found = append(found, f)
			continue
		}
		f := findingOf(theme.Units, said, nil, sent)
		f.Class, f.byWords = ClassPersonal, true
		f.Title = "You corrected the lead about " + strings.Join(f.words[:min(len(f.words), ShownWords)], ", ")
		f.Reason = "you corrected how the lead behaved; grouped by shared words, with no model"
		rule := directive(f)
		if _, named := memory.PersonIn(rule); lineFault(rule) == "" && !named && len(words.Stems(rule)) >= words.StrongSharedWords {
			f.Rule = strings.ToUpper(rule[:1]) + rule[1:] + "."
		}
		found = append(found, f)
	}
	if len(asked) > 1 {
		f := findingOf(asked, said, nil, sent)
		f.Class, f.Mechanism, f.byWords = ClassLibrary, leadTurnEnd, true
		f.Title = "The lead ended its turn offering or asking about a next step it could run itself"
		f.Rule = "Decide the next step and keep working; ask only what cannot be decided without asking."
		f.Reason = "the lead's turn ended asking you and your next message corrected it; nothing in tofu judges whether the lead should end its turn"
		found = append(found, f)
	}
	return found
}

type requests map[string]map[string]string

func (r requests) holds(said Said, quoted string) bool {
	if r[said.id] == nil {
		r[said.id] = map[string]string{}
		exchanges, _ := said.store.Exchanges(said.id)
		blobs, _ := said.store.Blobs(said.id)
		for _, exchange := range exchanges {
			var sent strings.Builder
			for _, hash := range exchange.Messages {
				var body session.MessageBody
				if json.Unmarshal(blobs[hash], &body) == nil {
					sent.WriteString(body.Content + "\n")
				}
			}
			r[said.id][exchange.Request] = flat(sent.String())
		}
	}
	quoted = flat(quoted)
	cut := min(len(quoted), CheckedBytes)
	for cut < len(quoted) && !utf8.RuneStart(quoted[cut]) {
		cut++
	}
	return strings.Contains(r[said.id][said.Answered], quoted[:cut])
}

func flat(text string) string { return strings.Join(strings.Fields(text), " ") }

func findingOf(group []words.Unit, said []Said, rare map[string]bool, sent requests) Finding {
	slices.SortStableFunc(group, func(a, b words.Unit) int { return said[a.Said].At.Compare(said[b.Said].At) })
	var f Finding
	places, count := map[string]bool{}, map[string]int{}
	for k, u := range group {
		s := said[u.Said]
		f.Quotes = append(f.Quotes, Quote{Session: s.Session, At: s.At, Text: u.Text, Calls: s.Calls})
		places[s.Place] = true
		for _, stem := range u.Stems {
			if rare == nil || rare[stem] {
				count[stem]++
			}
		}
		earlier := slices.DeleteFunc(slices.Clone(group[:k]), func(e words.Unit) bool { return said[e.Said].Session == s.Session })
		if len(earlier) == 0 || s.Answered == "" {
			continue
		}
		check := Check{Session: s.Session, At: s.At, Request: s.Answered}
		for _, e := range earlier {
			check.Present = check.Present || sent.holds(s, e.Text)
		}
		f.Checks = append(f.Checks, check)
	}
	for stem, n := range count {
		if n > 1 {
			f.words = append(f.words, stem)
		}
	}
	slices.SortFunc(f.words, func(a, b string) int { return cmp.Or(count[b]-count[a], strings.Compare(a, b)) })
	f.Sessions = len(places)
	return f
}

func directive(f Finding) string {
	best, score := "", -1
	for _, q := range f.Quotes {
		for _, clause := range words.Clauses(q.Text) {
			points := words.Shared(words.Stems(clause), f.words, nil)
			if directing().MatchString(clause) {
				points += SharedTellingWords
			}
			if clause = flat(clause); clause != "" && (points > score || points == score && len(clause) < len(best)) {
				best, score = clause, points
			}
		}
	}
	return strings.TrimSpace(toTheLead.Replace(" " + strings.ToLower(best) + " "))
}

func lineFault(text string) string {
	switch {
	case text == "" || strings.ContainsAny(text, "\r\n"):
		return "it is not one line"
	case len(text) > StatementBytes:
		return "it is over " + strconv.Itoa(StatementBytes) + " bytes"
	case strings.Contains(text, "[") || words.Profane(text):
		return "it carries a tag or filler"
	}
	return ""
}

func ruleFault(text string, quotes []Quote) string {
	if fault := lineFault(text); fault != "" {
		return fault
	}
	if who, named := memory.PersonIn(text); named {
		return "it names " + strings.TrimSpace(who)
	}
	said := words.Tokens(text)
	for _, q := range quotes {
		quoted := " " + strings.Join(words.Tokens(q.Text), " ") + " "
		for i := 0; i+QuotedRunWords <= len(said); i++ {
			if strings.Contains(quoted, " "+strings.Join(said[i:i+QuotedRunWords], " ")+" ") {
				return "it copies the person's words"
			}
		}
	}
	return ""
}
