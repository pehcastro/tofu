package recipe

import (
	"cmp"
	"encoding/base64"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"tofu/internal/konst"
)

type evidence int

const (
	unsupported evidence = iota
	weak
	strong
	noise
)

type taskValues struct {
	words   map[string]bool
	dates   []string
	numbers []string
	derived []string
}

func valuesOf(task string) taskValues {
	values := taskValues{words: map[string]bool{}}
	for _, word := range strings.FieldsFunc(strings.ToLower(task), notLetter) {
		values.words[word] = true
	}
	rest := regexp.MustCompile(`(?m)^\s*\d+\.\s`).ReplaceAllString(task, " ")
	dates := `\b(?:\d{4}-\d{2}-\d{2}|(?:[A-Za-z]{3,9}\.?\s+\d{1,2}(?:st|nd|rd|th)?|\d{1,2}(?:st|nd|rd|th)?\s+(?:of\s+)?[A-Za-z]{3,9}\.?),?\s+\d{4})\b`
	rest = regexp.MustCompile(dates).ReplaceAllStringFunc(rest, func(date string) string {
		plain := strings.Join(strings.Fields(regexp.MustCompile(`(\d)(?:st|nd|rd|th)|\.|,|\bof\b`).ReplaceAllString(date, "$1")), " ")
		for _, layout := range []string{time.DateOnly, "January 2 2006", "Jan 2 2006", "2 January 2006", "2 Jan 2006"} {
			if parsed, err := time.Parse(layout, plain); err == nil {
				for _, earlier := range values.dates {
					from, _ := time.Parse(time.DateOnly, earlier)
					values.derived = append(values.derived, strconv.Itoa(int(parsed.Sub(from).Abs()/(konst.RecipeHoursPerDay*time.Hour))))
				}
				values.dates = append(values.dates, parsed.Format(time.DateOnly))
				return " "
			}
		}
		return date
	})
	for _, number := range regexp.MustCompile(`\d{1,3}(?:[.,]\d{3})+\b|\d+`).FindAllString(rest, -1) {
		values.numbers = append(values.numbers, cmp.Or(strings.TrimLeft(strings.NewReplacer(",", "", ".", "").Replace(number), "0"), "0"))
	}
	for i, first := range values.numbers {
		for _, second := range values.numbers[i+1:] {
			if len(first) == 1 && len(second) == 1 {
				values.derived = append(values.derived, strconv.Itoa(int(first[0]-'0')+int(second[0]-'0')))
			}
		}
	}
	return values
}

func notLetter(r rune) bool { return !unicode.IsLetter(r) }

func (v taskValues) judge(value string) evidence {
	if _, err := time.Parse(time.DateOnly, value); err == nil || idLike(value) {
		if body := value + " " + decoded(value); slices.ContainsFunc(v.dates, func(date string) bool { return strings.Contains(body, date) }) {
			return strong
		}
		return noise
	}
	if number, err := strconv.Atoi(value); err == nil {
		normal := strconv.Itoa(number)
		switch said := slices.Contains(v.numbers, normal); {
		case said && len(normal) > 1:
			return strong
		case said || slices.Contains(v.derived, normal):
			return weak
		}
		return unsupported
	}
	if !strings.ContainsAny(value, konst.RecipeCodeJoiners) && v.wordsMatch(value) {
		return strong
	}
	return unsupported
}

func idLike(value string) bool {
	longDigits := len(value) >= konst.RecipeShortestDigitID && strings.TrimFunc(value, unicode.IsDigit) == ""
	mixed := strings.IndexFunc(value, unicode.IsDigit) >= 0 || strings.ToLower(value) != value && strings.ToUpper(value) != value
	encoded := len(value) >= konst.RecipeShortestEncoded && strings.Trim(value, konst.RecipeEncodedAlphabet) == "" && mixed
	return strings.HasPrefix(value, konst.RecipePlaceIDPrefix) || longDigits || encoded
}

func decoded(value string) string {
	body, _ := base64.RawStdEncoding.DecodeString(strings.NewReplacer("-", "+", "_", "/", "=", "").Replace(value))
	return string(body)
}

func (v taskValues) wordsMatch(text string) bool {
	words, known := 0, 0
	for _, word := range strings.FieldsFunc(strings.ToLower(text), notLetter) {
		if len(word) >= konst.RecipeShortestWord {
			words++
			if v.words[word] {
				known++
			}
		}
	}
	return known*2 > words
}

func (v taskValues) carriedBy(page *url.URL) bool {
	pairs := pairsOf(page)
	for _, want := range append(slices.Clone(v.dates), v.numbers...) {
		if len(want) > 1 && !slices.ContainsFunc(pairs, func(have pair) bool { return have.value == want || strings.Contains(decoded(have.value), want) }) {
			return false
		}
	}
	return true
}
