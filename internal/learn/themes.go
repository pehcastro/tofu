package learn

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"tofu/internal/memory"
	"tofu/internal/session"
)

type Bucket string

const (
	BucketApply    Bucket = "apply"
	BucketUpstream Bucket = "upstream"
)

type Target string

const (
	TargetMemory   Target = "memory"
	TargetRule     Target = "rule"
	TargetUpstream Target = "upstream"
)

const (
	forkCarry   = "fork carry"
	leadTurnEnd = "lead turn end"
)

var (
	corrected   = regexp.MustCompile(`(?i)\b(?:again|i said|i asked|i told|as i said|lol|you should|your job|yourself|where is|never asked|can't i just|you can just|not verified|nothing i asked)\b|\bwhy\b[^.!?\n]*\?`)
	askedPerson = regexp.MustCompile(`(?i)(decision for you|your call|you need to decide|until you decide|what i need from you|tell me (if|when|which|whether)|should i (start|do|go|build)|do you want|want me to|let me know|still open for you|\?\s*$)`)
	turnEnded   = regexp.MustCompile(`(?i)\byou should be the one\b|\bwhy (?:u |you )?(?:idle|stopp?ed|stop|waiting|ask)\b|\bcan't i just\b|\byou can just\b|\byour job\b|\bkeep working\b`)
	clauseEnd   = regexp.MustCompile(`[.?!,;:()]+\s*|\s+(?:and|then|but|so)\s+|\n+`)
	directing   = regexp.MustCompile(`(?i)\b(?:yourself|you should|your job|you can just|i want|must)\b`)
	toTheLead   = strings.NewReplacer(" yourself ", " itself ", " you are ", " the lead is ", " you're ", " the lead is ", " your ", " the lead's ", " you ", " the lead ", " u ", " the lead ", " lol ", " ", " pls ", " ", " please ", " ")
	sentenceEnd = regexp.MustCompile(`[.?!]+\s+|\n+`)
	tokens      = regexp.MustCompile(`[\pL\pN']+`)
)

const stopwords = " the and for that this with from have has had are was were been being but not you your yours yourself its it's it is in on of to a an as at be by do does did done can can't cant could would should will just like then than them they their there here what when where which who why how all any some more most much many very also too so if or no yes ok okay lol btw pls please idk ig imo wte etc i'm im me my mine we our us he she his her into out up down over again still now only even ever about after before because while one two get got make made let lets see say said thing things way really maybe sure need want wanna gonna kinda those these something anything everything nothing other others same each every being think know tho though "

type Quote struct {
	Session string    `json:"session"`
	At      time.Time `json:"at"`
	Text    string    `json:"text"`
}

type Check struct {
	Session string    `json:"session"`
	At      time.Time `json:"at"`
	Request string    `json:"request"`
	Present bool      `json:"present"`
}

type Theme struct {
	Words      []string `json:"words"`
	Quotes     []Quote  `json:"quotes"`
	Places     int      `json:"places"`
	Checks     []Check  `json:"checks,omitempty"`
	AboutAgent bool     `json:"about_agent"`
	Mechanism  string   `json:"mechanism,omitempty"`
}

func (t Theme) Present() int {
	present := 0
	for _, c := range t.Checks {
		if c.Present {
			present++
		}
	}
	return present
}

type Proposal struct {
	ID        int          `json:"id"`
	Key       string       `json:"key"`
	Bucket    Bucket       `json:"bucket"`
	Target    Target       `json:"target"`
	Scope     memory.Scope `json:"scope,omitempty"`
	Text      string       `json:"text"`
	Said      string       `json:"said,omitempty"`
	Session   string       `json:"session,omitempty"`
	Retire    string       `json:"retire,omitempty"`
	Mechanism string       `json:"mechanism,omitempty"`
	WrittenBy string       `json:"written_by,omitempty"`
	Places    int          `json:"places"`
	Themes    []Theme      `json:"themes"`
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
		if len([]rune(word)) < ShortestWord || strings.Contains(stopwords, " "+word+" ") || unicode.IsDigit([]rune(word)[0]) {
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

func themesOf(said []Said, sent requests) []Theme {
	units := unitsOf(said)
	rare := telling(units, len(said))
	var corrections, remarks, asked []int
	for i, u := range units {
		if !corrected.MatchString(u.text) {
			remarks = append(remarks, i)
			continue
		}
		corrections = append(corrections, i)
		if turnEnded.MatchString(u.text) && askedPerson.MatchString(said[u.said].Before) && !slices.ContainsFunc(asked, func(a int) bool { return units[a].said == u.said }) {
			asked = append(asked, i)
		}
	}
	var themes []Theme
	for _, group := range groupsOf(units, corrections, nil) {
		themes = append(themes, themeOf(group, units, said, nil, sent, ""))
	}
	for _, group := range groupsOf(units, remarks, rare) {
		theme := themeOf(group, units, said, rare, sent, "")
		theme.AboutAgent = false
		themes = append(themes, theme)
	}
	if len(asked) > 1 {
		themes = append(themes, themeOf(asked, units, said, nil, sent, leadTurnEnd))
	}
	slices.SortStableFunc(themes, func(a, b Theme) int {
		return cmp.Or(b.Places-a.Places, b.Quotes[len(b.Quotes)-1].At.Compare(a.Quotes[len(a.Quotes)-1].At))
	})
	return themes
}

func groupsOf(units []unit, picked []int, rare map[string]bool) [][]int {
	var groups [][]int
	for _, i := range picked {
		u := units[i]
		best, most := -1, SharedTellingWords-1
		for g, group := range groups {
			if slices.ContainsFunc(group, func(member int) bool { return units[member].said == u.said }) {
				continue
			}
			for _, member := range group {
				n, other := shared(u.stems, units[member].stems, rare), units[member].stems
				if n > most && n*SharedOfShorter >= min(shared(u.stems, u.stems, rare), shared(other, other, rare)) {
					best, most = g, n
				}
			}
		}
		if best < 0 {
			groups = append(groups, []int{i})
			continue
		}
		groups[best] = append(groups[best], i)
	}
	return slices.DeleteFunc(groups, func(group []int) bool { return len(group) < 2 })
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
	return strings.Contains(r[said.id][said.Answered], flat(words))
}

func flat(text string) string { return strings.Join(strings.Fields(text), " ") }

func themeOf(group []int, units []unit, said []Said, rare map[string]bool, sent requests, mechanism string) Theme {
	slices.SortStableFunc(group, func(a, b int) int { return said[units[a].said].At.Compare(said[units[b].said].At) })
	theme := Theme{AboutAgent: true, Mechanism: mechanism}
	places, count := map[string]bool{}, map[string]int{}
	for k, at := range group {
		u, s := units[at], said[units[at].said]
		theme.Quotes = append(theme.Quotes, Quote{Session: s.Session, At: s.At, Text: u.text})
		places[s.Place] = true
		for _, stem := range u.stems {
			if rare == nil || rare[stem] {
				count[stem]++
			}
		}
		earlier := slices.DeleteFunc(slices.Clone(group[:k]), func(e int) bool { return said[units[e].said].Session == s.Session })
		if len(earlier) == 0 || s.Answered == "" {
			continue
		}
		check := Check{Session: s.Session, At: s.At, Request: s.Answered}
		for _, e := range earlier {
			check.Present = check.Present || sent.holds(s, units[e].text)
		}
		theme.Checks = append(theme.Checks, check)
	}
	for stem, n := range count {
		if n > 1 {
			theme.Words = append(theme.Words, stem)
		}
	}
	slices.SortFunc(theme.Words, func(a, b string) int { return cmp.Or(count[b]-count[a], strings.Compare(a, b)) })
	theme.Places = len(places)
	return theme
}

func (t Theme) Label() string {
	if t.Mechanism != "" {
		return "you corrected a turn that ended asking you"
	}
	return strings.Join(t.Words[:min(len(t.Words), 3)], " · ")
}

type Known struct {
	Memory    []memory.Entry
	Decisions []Decision
}

func propose(themes []Theme, known Known) []Proposal {
	var proposals []Proposal
	upstream := Proposal{Bucket: BucketUpstream, Target: TargetUpstream, Mechanism: forkCarry}
	lost, retired := map[string]bool{}, map[string]bool{}
	for _, theme := range themes {
		if !theme.AboutAgent || theme.Places < CorroboratingPlaces {
			continue
		}
		if theme.Mechanism == "" && theme.Present() < len(theme.Checks) {
			upstream.Themes = append(upstream.Themes, theme)
			for _, q := range theme.Quotes {
				lost[q.Session] = true
			}
		}
		latest, places := theme.Quotes[len(theme.Quotes)-1], strconv.Itoa(theme.Places)
		proposal := Proposal{Bucket: BucketApply, Target: TargetMemory, Scope: memory.Global, Said: latest.Text, Session: latest.Session, Places: theme.Places, Themes: []Theme{theme},
			Text: statement("Said in "+places+" sessions, to hold without being told again: ", directive(theme)+".")}
		if theme.Mechanism == leadTurnEnd {
			proposal.Text = "Decide the next step and keep working, and ask only what cannot be decided without asking; a turn that ended asking was corrected in " + places + " sessions."
		}
		if entry, again := needed(theme, known.Memory, retired); again {
			proposal.Target, proposal.Scope, proposal.Retire, proposal.Text, proposal.Said = TargetRule, entry.Scope, entry.ID, entry.Text, entry.Said
			retired[entry.ID] = true
		}
		proposals = append(proposals, proposal)
		if theme.Mechanism == leadTurnEnd {
			proposals = append(proposals, Proposal{Bucket: BucketUpstream, Target: TargetUpstream, Mechanism: leadTurnEnd, Places: theme.Places, Themes: []Theme{theme},
				Text: "a lead turn ended handing you a choice and your next message corrected it, in " + places + " sessions; only a sub-agent's turn end is judged"})
		}
	}
	if len(upstream.Themes) > 0 {
		upstream.Places = len(lost)
		upstream.Text = "your earlier words were missing from the request when the mistake came back, across " + strconv.Itoa(len(lost)) + " sessions"
		proposals = append(proposals, upstream)
	}
	for i := range proposals {
		proposals[i].Key = keyOf(proposals[i])
	}
	proposals = slices.DeleteFunc(proposals, func(p Proposal) bool { return quiet(p, known.Decisions) })
	slices.SortStableFunc(proposals, func(a, b Proposal) int { return b.Places - a.Places })
	return proposals
}

func needed(theme Theme, entries []memory.Entry, retired map[string]bool) (memory.Entry, bool) {
	for _, entry := range slices.DeleteFunc(slices.Clone(entries), func(e memory.Entry) bool { return retired[e.ID] }) {
		stems := stemsOf(entry.Text + " " + entry.Said)
		places := map[string]bool{}
		for _, q := range theme.Quotes {
			if q.At.After(entry.At) && shared(stemsOf(q.Text), stems, nil) >= SharedTellingWords {
				places[q.Session] = true
			}
		}
		if len(places) >= CorroboratingPlaces {
			return entry, true
		}
	}
	return memory.Entry{}, false
}

func directive(theme Theme) string {
	best, score := "", -1
	for _, q := range theme.Quotes {
		for _, clause := range clauseEnd.Split(q.Text, -1) {
			points := shared(stemsOf(clause), theme.Words, nil)
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

func Statement(reply string) (string, bool) {
	text := strings.Trim(strings.TrimSpace(reply), `"`)
	_, named := memory.PersonIn(text)
	return text, text != "" && !named && !strings.ContainsAny(text, "\r\n") && len(text) <= StatementBytes
}

func (p Proposal) Prompt() string {
	var quotes strings.Builder
	for _, theme := range p.Themes {
		for _, q := range theme.Quotes {
			quotes.WriteString("- " + flat(q.Text) + "\n")
		}
	}
	return "Write the one rule a coding assistant should follow from now on, judging from these chat messages sent to it in different sessions:\n\n" + quotes.String() +
		"\nState the rule itself in plain words, imperative or declarative, and name no one: no name, no \"the person\", \"the user\", he or she. Keep it under " +
		strconv.Itoa(StatementAskedBytes) + " characters, and reply with that sentence only."
}

func statement(lead, words string) string {
	text := lead + flat(words)
	if len(text) <= StatementBytes {
		return text
	}
	cut := strings.LastIndex(text[:StatementBytes], " ")
	return text[:max(cut, len(lead))] + " ..."
}

func keyOf(p Proposal) string {
	first := p.Themes[0].Mechanism + "\x00" + flat(p.Themes[0].Quotes[0].Text)
	if p.Target == TargetUpstream {
		first = p.Mechanism
	}
	sum := sha256.Sum256([]byte(string(p.Target) + "\x00" + first))
	return hex.EncodeToString(sum[:4])
}
