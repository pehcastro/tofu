package learn

import (
	"cmp"
	"encoding/json"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"tofu/internal/memory"
	"tofu/internal/session"
)

var (
	corrected   = regexp.MustCompile(`(?i)\b(?:again|i said|i asked|i told|as i said|lol|you should|your job|yourself|where is|never asked|can't i just|you can just|not verified|nothing i asked)\b|\bwhy\b[^.!?\n]*\?`)
	askedPerson = regexp.MustCompile(`(?i)(decision for you|your call|you need to decide|until you decide|what i need from you|tell me (if|when|which|whether)|should i (start|do|go|build)|do you want|want me to|let me know|still open for you|\?\s*$)`)
	turnEnded   = regexp.MustCompile(`(?i)\byou should be the one\b|\bwhy (?:u |you )?(?:idle|stopp?ed|stop|waiting|ask)\b|\bcan't i just\b|\byou can just\b|\byour job\b|\bkeep working\b`)
	clauseEnd   = regexp.MustCompile(`[.?!,;:()]+\s*|\s+(?:and|then|but|so)\s+|\n+`)
	directing   = regexp.MustCompile(`(?i)\b(?:yourself|you should|your job|you can just|i want|must)\b`)
	toTheLead   = strings.NewReplacer(" yourself ", " itself ", " you are ", " the lead is ", " you're ", " the lead is ", " your ", " the lead's ", " you ", " the lead ", " u ", " the lead ", " lol ", " ", " pls ", " ", " please ", " ", " ok ", " ")
	sentenceEnd = regexp.MustCompile(`[.?!]+\s+|\n+`)
	tokens      = regexp.MustCompile(`[\pL\pN']+`)
)

const (
	stopwords = " the and for that this with from have has had are was were been being but not you your yours yourself its it's it is in on of to a an as at be by do does did done can can't cant could would should will just like then than them they their there here what when where which who why how all any some more most much many very also too so if or no yes ok okay btw pls please idk ig imo etc i'm im me my mine we our us he she his her into out up down over again still now only even ever about after before because while one two get got make made let lets see say said thing things way really maybe sure need want wanna gonna kinda those these something anything everything nothing other others same each every being think know tho though yeah yep nope hey don't dont didn't isn't doesn't won't aren't wasn't you're youre i've i'll you'll we're they're that's there's what's let's literally basically "
	profane   = " fuck fucking fucked fuckin wtf shit shitty damn lmao lol omg bruh crap "
)

func profaneIn(text string) bool {
	return slices.ContainsFunc(tokens.FindAllString(strings.ToLower(text), -1), func(word string) bool { return strings.Contains(profane, " "+word+" ") })
}

type unit struct {
	said  int
	text  string
	stems []string
}

func stemsOf(text string) []string {
	var stems []string
	for _, word := range tokens.FindAllString(strings.ToLower(text), -1) {
		word = strings.TrimSuffix(strings.Trim(word, "'"), "'s")
		if len([]rune(word)) < ShortestWord || strings.Contains(stopwords, " "+word+" ") || strings.Contains(profane, " "+word+" ") || unicode.IsDigit([]rune(word)[0]) {
			continue
		}
		for _, suffix := range []string{"ing", "ed", "es", "s"} {
			if cut, found := strings.CutSuffix(word, suffix); found && len(cut) >= ShortestWord {
				word = cut
				if n := len(word); suffix != "s" && n > ShortestWord && word[n-1] == word[n-2] {
					word = word[:n-1]
				}
				if stem, endsInI := strings.CutSuffix(word, "i"); endsInI && suffix != "ing" {
					word = stem + "y"
				}
				break
			}
		}
		if !slices.Contains(stems, word) {
			stems = append(stems, word)
		}
	}
	return stems
}

func unitsOf(said []Said) []unit {
	var units []unit
	for i, s := range said {
		pieces := []string{s.Text}
		if len(strings.Fields(s.Text)) > ShortMessageWords {
			pieces = sentenceEnd.Split(s.Text, -1)
		}
		for _, piece := range pieces {
			if piece = strings.TrimSpace(piece); piece != "" {
				units = append(units, unit{said: i, text: piece, stems: stemsOf(piece)})
			}
		}
	}
	return units
}

func telling(units []unit, messages int) map[string]bool {
	count, seen := map[string]int{}, map[string]int{}
	for _, u := range units {
		for _, stem := range u.stems {
			if last, found := seen[stem]; !found || last != u.said {
				count[stem]++
			}
			seen[stem] = u.said
		}
	}
	most := max(TellingFloor, messages/TellingShare)
	rare := map[string]bool{}
	for stem, n := range count {
		if n <= most {
			rare[stem] = true
		}
	}
	return rare
}

func shared(a, b []string, rare map[string]bool) int {
	both := 0
	for _, stem := range a {
		if (rare == nil || rare[stem]) && slices.Contains(b, stem) {
			both++
		}
	}
	return both
}

func localFindings(said []Said, sent requests) []Finding {
	units := unitsOf(said)
	rare := telling(units, len(said))
	var corrections, remarks, asked []unit
	for _, u := range units {
		if !corrected.MatchString(u.text) {
			remarks = append(remarks, u)
			continue
		}
		corrections = append(corrections, u)
		if turnEnded.MatchString(u.text) && askedPerson.MatchString(said[u.said].Before) && !slices.ContainsFunc(asked, func(a unit) bool { return a.said == u.said }) {
			asked = append(asked, u)
		}
	}
	var found []Finding
	for _, group := range groupsOf(corrections, nil) {
		f := findingOf(group, said, nil, sent)
		f.Class, f.byWords = ClassPersonal, true
		f.Title = "You corrected the lead about " + strings.Join(f.words[:min(len(f.words), ShownWords)], ", ")
		f.Reason = "you corrected how the lead behaved; grouped by shared words, with no model"
		rule := directive(f)
		if _, named := memory.PersonIn(rule); lineFault(rule) == "" && !named && len(stemsOf(rule)) >= StrongSharedWords {
			f.Rule = strings.ToUpper(rule[:1]) + rule[1:] + "."
		}
		found = append(found, f)
	}
	for _, group := range groupsOf(remarks, rare) {
		f := findingOf(group, said, rare, sent)
		f.Class, f.byWords = ClassProject, true
		f.Title = "You said about the project: " + strings.Join(f.words[:min(len(f.words), ShownWords)], ", ")
		f.Reason = "a remark that names no correction of the lead"
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

func groupsOf(units []unit, rare map[string]bool) [][]unit {
	var groups [][]unit
	for _, u := range units {
		best, most := -1, StrongSharedWords-1
		for g, group := range groups {
			if slices.ContainsFunc(group, func(member unit) bool { return member.said == u.said }) {
				continue
			}
			for _, member := range group {
				n := shared(u.stems, member.stems, rare)
				if n > most && n*SharedOfShorter >= min(shared(u.stems, u.stems, rare), shared(member.stems, member.stems, rare)) {
					best, most = g, n
				}
			}
		}
		if best < 0 {
			groups = append(groups, []unit{u})
			continue
		}
		groups[best] = append(groups[best], u)
	}
	return slices.DeleteFunc(groups, func(group []unit) bool { return len(group) < 2 })
}

type requests map[string]map[string]string

func (r requests) holds(said Said, words string) bool {
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
	words = flat(words)
	cut := min(len(words), CheckedBytes)
	for cut < len(words) && !utf8.RuneStart(words[cut]) {
		cut++
	}
	return strings.Contains(r[said.id][said.Answered], words[:cut])
}

func flat(text string) string { return strings.Join(strings.Fields(text), " ") }

func findingOf(group []unit, said []Said, rare map[string]bool, sent requests) Finding {
	slices.SortStableFunc(group, func(a, b unit) int { return said[a.said].At.Compare(said[b.said].At) })
	var f Finding
	places, count := map[string]bool{}, map[string]int{}
	for k, u := range group {
		s := said[u.said]
		f.Quotes = append(f.Quotes, Quote{Session: s.Session, At: s.At, Text: u.text, Calls: s.Calls})
		places[s.Place] = true
		for _, stem := range u.stems {
			if rare == nil || rare[stem] {
				count[stem]++
			}
		}
		earlier := slices.DeleteFunc(slices.Clone(group[:k]), func(e unit) bool { return said[e.said].Session == s.Session })
		if len(earlier) == 0 || s.Answered == "" {
			continue
		}
		check := Check{Session: s.Session, At: s.At, Request: s.Answered}
		for _, e := range earlier {
			check.Present = check.Present || sent.holds(s, e.text)
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
		for _, clause := range clauseEnd.Split(q.Text, -1) {
			points := shared(stemsOf(clause), f.words, nil)
			if directing.MatchString(clause) {
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
	case strings.Contains(text, "[") || profaneIn(text):
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
	words := tokens.FindAllString(strings.ToLower(text), -1)
	for _, q := range quotes {
		quoted := " " + strings.Join(tokens.FindAllString(strings.ToLower(q.Text), -1), " ") + " "
		for i := 0; i+QuotedRunWords <= len(words); i++ {
			if strings.Contains(quoted, " "+strings.Join(words[i:i+QuotedRunWords], " ")+" ") {
				return "it copies the person's words"
			}
		}
	}
	return ""
}
