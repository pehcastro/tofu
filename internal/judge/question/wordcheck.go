package question

import (
	"regexp"
	"sort"
	"strings"

	"boji/internal/judge/jev"
)

func arithmeticPhrases() []string {
	return []string{
		"how many",
		"how often",
		"how far",
		"how long ago",
		"count the",
		"number of",
		"sum of",
		"total of",
		"add up",
		"subtract",
		"multiply",
		"divide",
		"percentage of",
		"average of",
		"earlier than",
		"later than",
		"before or after",
		"which is larger",
		"which is greater",
		"which is longer",
	}
}

func arithmeticIn(words []string) []string {
	seen := map[string]bool{}
	for _, w := range words {
		low := strings.ToLower(w)
		for _, phrase := range arithmeticPhrases() {
			if strings.Contains(low, phrase) {
				seen[phrase] = true
			}
		}
	}
	return sorted(seen)
}

var (
	backtickedField = regexp.MustCompile("`([^`]+)`")
	shapedField     = regexp.MustCompile(`^[a-z_][a-z0-9_]*(\[[0-9]+\])?(\.[a-z0-9_]+(\[[0-9]+\])?)*$`)
	fieldIndex      = regexp.MustCompile(`\[[0-9]+\]`)
)

func normalizeField(ref string) string {
	return fieldIndex.ReplaceAllString(ref, "")
}

func fieldFindings(words []string, state []string) (unknown, unparseable []string) {
	seenUnknown := map[string]bool{}
	seenUnparseable := map[string]bool{}
	for _, w := range words {
		for _, m := range backtickedField.FindAllStringSubmatch(w, -1) {
			ref := m[1]
			if !shapedField.MatchString(ref) {
				seenUnparseable[ref] = true
				continue
			}
			if inState(normalizeField(ref), state) {
				continue
			}
			seenUnknown[ref] = true
		}
	}
	return sorted(seenUnknown), sorted(seenUnparseable)
}

func inState(ref string, state []string) bool {
	for _, s := range state {
		if ref == s || strings.HasPrefix(ref, s+".") {
			return true
		}
	}
	return false
}

func hasBlanketQuestion(questions []Question) bool {
	for _, q := range questions {
		named := false
		for _, w := range q.Words() {
			if backtickedField.MatchString(w) {
				named = true
				break
			}
		}
		if !named {
			return true
		}
	}
	return false
}

func fieldRefs(words []string) []string {
	var out []string
	for _, w := range words {
		for _, m := range backtickedField.FindAllStringSubmatch(w, -1) {
			if ref := m[1]; shapedField.MatchString(ref) {
				out = append(out, normalizeField(ref))
			}
		}
	}
	return out
}

func unusedFields(set Set) []string {
	var refs []string
	for _, q := range set.Questions {
		refs = append(refs, fieldRefs(q.Words())...)
	}
	var out []string
	for _, s := range set.State {
		used := false
		for _, ref := range refs {
			if inState(ref, []string{s}) {
				used = true
				break
			}
		}
		if !used {
			out = append(out, s)
		}
	}
	return out
}

func requestTokens(set Set) (stateTokens, longestQuestionTokens, totalTokens int) {
	stateBytes := 0
	for _, s := range set.State {
		stateBytes += len(s)
	}
	totalBytes := stateBytes
	for _, q := range set.Questions {
		qBytes := len(q.Name)
		for _, w := range q.Words() {
			qBytes += len(w)
		}
		for _, o := range q.Options {
			qBytes += len(o.Name)
		}
		totalBytes += qBytes
		if tokens := jev.EstimateTokens(qBytes); tokens > longestQuestionTokens {
			longestQuestionTokens = tokens
		}
	}
	return jev.EstimateTokens(stateBytes), longestQuestionTokens, jev.EstimateTokens(totalBytes)
}

func sorted(set map[string]bool) []string {
	var out []string
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
