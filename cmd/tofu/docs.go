package main

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode"

	settingspkg "tofu/internal/settings"
	"tofu/library/docs"
)

const docsIndexFile = "index.yaml"

type docsEntry struct{ ask, do, check, topic string }

type docsPage struct{ topic, title, summary, body string }

type docsCorpus struct {
	pages   []docsPage
	entries []docsEntry
}

func (c docsCorpus) topics() []string {
	topics := make([]string, len(c.pages))
	for i, page := range c.pages {
		topics[i] = page.topic
	}
	return topics
}

func docsVerb(args []string, out, errOut io.Writer) int {
	corpus, err := loadDocs()
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "tofu docs: %v\n", err)
		return exitUsage
	}
	query := strings.Join(args, " ")
	switch {
	case query == "":
		printDocsIndex(out, corpus)
		return exitOK
	case strings.ContainsAny(query, " \t"):
		return searchDocs(query, corpus, out, errOut)
	}
	at := slices.IndexFunc(corpus.pages, func(page docsPage) bool { return page.topic == query })
	if at < 0 {
		closest := corpus.topics()
		slices.SortStableFunc(closest, func(a, b string) int { return cmp.Compare(editDistance(query, a), editDistance(query, b)) })
		_, _ = fmt.Fprintf(errOut, "tofu docs: no topic %q. closest first: %s\n", query, strings.Join(closest, ", "))
		return exitVerdict
	}
	page := corpus.pages[at]
	_, _ = fmt.Fprintf(out, "# %s\n\n%s", page.title, page.body)
	if page.topic == "settings" {
		printSettingsTable(out)
	}
	return exitOK
}

func loadDocs() (docsCorpus, error) {
	var corpus docsCorpus
	files := docs.Files()
	names, err := fs.Glob(files, "*.md")
	if err != nil {
		return corpus, err
	}
	for _, name := range names {
		data, err := fs.ReadFile(files, name)
		if err != nil {
			return corpus, err
		}
		page, err := parseDocsPage(string(data))
		if err != nil {
			return corpus, fmt.Errorf("%s: %w", name, err)
		}
		corpus.pages = append(corpus.pages, page)
	}
	data, err := fs.ReadFile(files, docsIndexFile)
	if err != nil {
		return corpus, err
	}
	if corpus.entries, err = parseDocsIndex(string(data)); err != nil {
		return corpus, err
	}
	for _, entry := range corpus.entries {
		if !slices.ContainsFunc(corpus.pages, func(page docsPage) bool { return page.topic == entry.topic }) {
			return corpus, fmt.Errorf("%s: %q names the topic %q, which has no page", docsIndexFile, entry.ask, entry.topic)
		}
	}
	return corpus, nil
}

func parseDocsPage(text string) (docsPage, error) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	rest, opened := strings.CutPrefix(text, "---\n")
	header, body, closed := strings.Cut(rest, "\n---\n")
	if !opened || !closed {
		return docsPage{}, errors.New("a page opens with front matter between two --- lines")
	}
	fields := map[string]string{}
	for _, line := range strings.Split(header, "\n") {
		key, value, _ := strings.Cut(line, ":")
		fields[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	page := docsPage{topic: fields["topic"], title: fields["title"], summary: fields["summary"], body: strings.TrimLeft(body, "\n")}
	if page.topic == "" || page.title == "" || page.summary == "" {
		return docsPage{}, errors.New("the front matter names a topic, a title and a summary")
	}
	return page, nil
}

func parseDocsIndex(text string) ([]docsEntry, error) {
	var entries []docsEntry
	for n, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if item, opens := strings.CutPrefix(line, "- "); opens {
			entries, line = append(entries, docsEntry{}), item
		}
		key, value, split := strings.Cut(strings.TrimSpace(line), ": ")
		if !split || len(entries) == 0 {
			return nil, fmt.Errorf("%s:%d: an entry is a list of ask, do, check and topic", docsIndexFile, n+1)
		}
		entry := &entries[len(entries)-1]
		switch key {
		case "ask":
			entry.ask = value
		case "do":
			entry.do = value
		case "check":
			entry.check = value
		case "topic":
			entry.topic = value
		default:
			return nil, fmt.Errorf("%s:%d: %q is not ask, do, check or topic", docsIndexFile, n+1, key)
		}
	}
	for _, entry := range entries {
		if entry.ask == "" || entry.do == "" || entry.check == "" || entry.topic == "" {
			return nil, fmt.Errorf("%s: %q lacks one of ask, do, check and topic", docsIndexFile, entry.ask)
		}
	}
	return entries, nil
}

func printDocsIndex(out io.Writer, corpus docsCorpus) {
	rows := make([][2]string, len(corpus.entries))
	for i, entry := range corpus.entries {
		rows[i] = [2]string{entry.ask, entry.do}
	}
	printDocsRows(out, rows)
	_, _ = fmt.Fprintf(out, "\ntopics: %s. tofu docs <topic> prints one, tofu docs \"a few words\" finds the closest.\n", strings.Join(corpus.topics(), ", "))
}

func printDocsRows(out io.Writer, rows [][2]string) {
	widest := 0
	for _, row := range rows {
		widest = max(widest, len(row[0]))
	}
	for _, row := range rows {
		_, _ = fmt.Fprintf(out, "%-*s  %s\n", widest, row[0], row[1])
	}
}

func searchDocs(query string, corpus docsCorpus, out, errOut io.Writer) int {
	asked := slices.Compact(slices.Sorted(slices.Values(docsWords(strings.ToLower(query)))))
	type hit struct {
		score int
		row   [2]string
	}
	var hits []hit
	score := func(text string, row [2]string) {
		words := docsWords(strings.ToLower(text))
		overlap := 0
		for _, word := range asked {
			if slices.Contains(words, word) {
				overlap++
			}
		}
		if overlap > 0 {
			hits = append(hits, hit{overlap, row})
		}
	}
	for _, entry := range corpus.entries {
		score(entry.ask+" "+entry.do+" "+entry.topic, [2]string{entry.ask, entry.do})
	}
	for _, page := range corpus.pages {
		score(page.topic+" "+page.title+" "+page.summary+" "+docsHeadings(page.body), [2]string{page.title, "tofu docs " + page.topic})
	}
	if len(hits) == 0 {
		_, _ = fmt.Fprintf(errOut, "tofu docs: nothing matches %q. tofu docs prints every question it answers\n", query)
		return exitVerdict
	}
	slices.SortStableFunc(hits, func(a, b hit) int { return cmp.Compare(b.score, a.score) })
	rows := make([][2]string, len(hits))
	for i, one := range hits {
		rows[i] = one.row
	}
	printDocsRows(out, rows)
	return exitOK
}

func docsHeadings(body string) string {
	var headings []string
	for _, line := range strings.Split(body, "\n") {
		if heading, is := strings.CutPrefix(line, "## "); is {
			headings = append(headings, heading)
		}
	}
	return strings.Join(headings, " ")
}

func docsWords(text string) []string {
	words := strings.FieldsFunc(text, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '.' })
	for i, word := range words {
		words[i] = strings.TrimRight(word, ".")
	}
	return words
}

func editDistance(a, b string) int {
	previous := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(a); i++ {
		current := make([]int, len(b)+1)
		current[0] = i
		for j := 1; j <= len(b); j++ {
			substitution := previous[j-1]
			if a[i-1] != b[j-1] {
				substitution++
			}
			current[j] = min(previous[j]+1, current[j-1]+1, substitution)
		}
		previous = current
	}
	return previous[len(b)]
}

func printSettingsTable(out io.Writer) {
	category := ""
	for _, spec := range settingspkg.Default() {
		if spec.Category != category {
			category = spec.Category
			_, _ = fmt.Fprintf(out, "\n%s\n", category)
		}
		restart := ""
		if spec.Restart {
			restart = "   (on the next start)"
		}
		_, _ = fmt.Fprintf(out, "  %s = %s   %s%s\n      %s\n", spec.Key, settingDefault(spec), settingTakes(spec), restart, spec.Description)
	}
}

func settingDefault(spec settingspkg.Spec) string {
	switch spec.Kind {
	case settingspkg.Bool:
		return strconv.FormatBool(spec.Default != 0)
	case settingspkg.Int:
		return strconv.Itoa(spec.Default)
	case settingspkg.Text:
		return cmp.Or(spec.DefaultText, "empty")
	}
	panic(fmt.Sprintf("settings: %s has an unknown kind %d", spec.Key, spec.Kind))
}

func settingTakes(spec settingspkg.Spec) string {
	switch {
	case spec.Kind == settingspkg.Bool:
		return "true or false"
	case spec.Kind == settingspkg.Int && spec.Most == math.MaxInt32:
		return fmt.Sprintf("at least %d %s", spec.Least, spec.Unit)
	case spec.Kind == settingspkg.Int:
		return fmt.Sprintf("%d to %d %s", spec.Least, spec.Most, spec.Unit)
	case spec.ListOf != nil:
		return "a comma list of " + strings.Join(spec.ListOf, ", ")
	case len(spec.Choices) > 0:
		return "one of " + strings.Join(spec.Choices, ", ")
	}
	return "any text"
}

func docsReport(out io.Writer) []error {
	corpus, err := loadDocs()
	if err != nil {
		return []error{fmt.Errorf("docs: %w", err)}
	}
	named := map[string]bool{}
	for _, page := range corpus.pages {
		for _, word := range docsWords(page.body) {
			named[word] = true
		}
	}
	for _, entry := range corpus.entries {
		for _, word := range docsWords(entry.ask + " " + entry.do + " " + entry.check) {
			named[word] = true
		}
	}
	var missing []error
	for _, spec := range settingspkg.Default() {
		if !named[spec.Key] {
			missing = append(missing, fmt.Errorf("docs: no page or index entry names the setting %s", spec.Key))
		}
	}
	_, _ = fmt.Fprintf(out, "%-14s %3d pages   %d index entries   %d settings missing\n", "docs", len(corpus.pages), len(corpus.entries), len(missing))
	return missing
}
