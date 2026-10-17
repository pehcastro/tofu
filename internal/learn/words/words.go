package words

import (
	"slices"
	"strings"
	"unicode"

	"tofu/internal/sys"
)

const (
	StrongSharedWords = 3
	SharedOfShorter   = 3
	AgreeingClauses   = 2
	TellingFloor      = 3
	TellingShare      = 10
	ShortMessageWords = 30
	ShortestWord      = 3
	RepeatQuoteBytes  = 200
	RepeatCandidates  = 60
)

var (
	corrected   = sys.LazyRegexp(`(?i)\b(?:you|u)\b[^.?!,;:\n]*\bagain\b|\bagain\b[^.?!,;:\n]*\b(?:you|u)\b|\b(?:i said|i asked|i told|as i said|lol|you should|your job|yourself|where is|never asked|can't i just|you can just|not verified|nothing i asked)\b|\bwhy\b[^.!?\n]*\?`)
	sentenceEnd = sys.LazyRegexp(`[.?!]+\s+|\n+`)
	clauseEnd   = sys.LazyRegexp(`[.?!,;:()]+\s*|\s+(?:and|then|but|so)\s+|\n+`)
	tokens      = sys.LazyRegexp(`[\pL\pN']+`)
)

const (
	stopwords = " the and for that this with from have has had are was were been being but not you your yours yourself its it's it is in on of to a an as at be by do does did done can can't cant could would should will just like then than them they their there here what when where which who why how all any some more most much many very also too so if or no yes ok okay btw pls please idk ig imo etc i'm im me my mine we our us he she his her into out up down over again still now only even ever about after before because while one two get got make made let lets see say said thing things way really maybe sure need want wanna gonna kinda those these something anything everything nothing other others same each every being think know tho though yeah yep nope hey don't dont didn't isn't doesn't won't aren't wasn't you're youre i've i'll you'll we're they're that's there's what's let's literally basically "
	profane   = " fuck fucking fucked fuckin wtf shit shitty damn lmao lol omg bruh crap "
)

func Tokens(text string) []string { return tokens().FindAllString(strings.ToLower(text), -1) }

func Profane(text string) bool {
	return slices.ContainsFunc(Tokens(text), func(word string) bool { return strings.Contains(profane, " "+word+" ") })
}

func Corrects(text string) bool { return corrected().MatchString(text) }

func Clauses(text string) []string { return clauseEnd().Split(text, -1) }

func Stems(text string) []string {
	var stems []string
	for _, word := range Tokens(text) {
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

type Unit struct {
	Said  int
	Text  string
	Stems []string
	spans [][]string
}

func Units(said []string) []Unit {
	var units []Unit
	for i, text := range said {
		pieces := []string{text}
		if len(strings.Fields(text)) > ShortMessageWords {
			pieces = sentenceEnd().Split(text, -1)
		}
		for _, piece := range pieces {
			if piece = strings.TrimSpace(piece); piece != "" {
				units = append(units, Unit{Said: i, Text: piece, Stems: Stems(piece)})
			}
		}
	}
	return units
}

func Telling(units []Unit, messages int) map[string]bool {
	count, seen := map[string]int{}, map[string]int{}
	for _, u := range units {
		for _, stem := range u.Stems {
			if last, found := seen[stem]; !found || last != u.Said {
				count[stem]++
			}
			seen[stem] = u.Said
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

func Shared(a, b []string, rare map[string]bool) int {
	both := 0
	for _, stem := range a {
		if (rare == nil || rare[stem]) && slices.Contains(b, stem) {
			both++
		}
	}
	return both
}

func Alike(a, b []string, rare map[string]bool) int {
	n := Shared(a, b, rare)
	if n < StrongSharedWords || n*SharedOfShorter < min(Shared(a, a, rare), Shared(b, b, rare)) {
		return 0
	}
	return n
}

type Theme struct {
	Corrects bool
	Units    []Unit
}

func Themes(units []Unit, rare map[string]bool) []Theme {
	var corrections, remarks []Unit
	for _, u := range units {
		if Corrects(u.Text) {
			clauses := Clauses(u.Text)
			for i := range max(1, len(clauses)-AgreeingClauses+1) {
				u.spans = append(u.spans, Stems(strings.Join(clauses[i:min(len(clauses), i+AgreeingClauses)], " ")))
			}
			corrections = append(corrections, u)
			continue
		}
		remarks = append(remarks, u)
	}
	var themes []Theme
	for _, group := range groupsOf(corrections, agreed) {
		themes = append(themes, Theme{Corrects: true, Units: group})
	}
	for _, group := range groupsOf(remarks, func(a, b Unit) int { return Alike(a.Stems, b.Stems, rare) }) {
		themes = append(themes, Theme{Units: group})
	}
	return themes
}

func Candidates(earlier []string, said string) []int {
	if slices.Contains(earlier, said) {
		return nil
	}
	stems := Stems(said)
	shared := make([]int, len(earlier))
	var picked []int
	for i := len(earlier) - 1; i >= 0; i-- {
		if earlier[i] != "" && !slices.Contains(earlier[i+1:], earlier[i]) {
			shared[i] = Shared(stems, Stems(earlier[i]), nil)
			picked = append(picked, i)
		}
	}
	slices.SortStableFunc(picked, func(a, b int) int { return shared[b] - shared[a] })
	return picked[:min(len(picked), RepeatCandidates)]
}

func agreed(a, b Unit) int {
	most := 0
	for _, ours := range a.spans {
		for _, theirs := range b.spans {
			most = max(most, Alike(ours, theirs, nil))
		}
	}
	return most
}

func groupsOf(units []Unit, alike func(a, b Unit) int) [][]Unit {
	var groups [][]Unit
	for _, u := range units {
		best, most := -1, 0
		for g, group := range groups {
			if slices.ContainsFunc(group, func(member Unit) bool { return member.Said == u.Said }) {
				continue
			}
			for _, member := range group {
				if n := alike(u, member); n > most {
					best, most = g, n
				}
			}
		}
		if best < 0 {
			groups = append(groups, []Unit{u})
			continue
		}
		groups[best] = append(groups[best], u)
	}
	return slices.DeleteFunc(groups, func(group []Unit) bool { return len(group) < 2 })
}
