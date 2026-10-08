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
	"time"
	"unicode"

	"tofu/interface/cli"
	"tofu/internal/host"
	settingspkg "tofu/internal/settings"
	"tofu/internal/widget"
	"tofu/library/docs"
)

const (
	docsIndexFile = "index.yaml"
	docsUsage     = `tofu docs [topic | "a few words"] [--json]`
)

type (
	docsEntry  = host.DocsEntry
	docsPage   = host.DocsPage
	settingDoc = host.SettingDoc
)

type docsCorpus host.DocsCorpus

type docsMatch struct {
	Text string `json:"text"`
	Do   string `json:"do"`
}

func (c docsCorpus) topics() []string {
	topics := make([]string, len(c.Pages))
	for i, page := range c.Pages {
		topics[i] = page.Topic
	}
	return topics
}

func docsVerb(args []string, out, errOut io.Writer) int {
	o := verbOutput{verb: "docs", usageLine: docsUsage, asJSON: slices.Contains(args, jsonFlag), out: out, errOut: errOut}
	query := strings.Join(slices.DeleteFunc(slices.Clone(args), func(arg string) bool { return arg == jsonFlag }), " ")
	corpus, err := loadDocs()
	if err != nil {
		return o.fail(err)
	}
	switch {
	case query == "":
		index := corpus
		index.Pages = slices.Clone(corpus.Pages)
		for i := range index.Pages {
			index.Pages[i].Body = ""
		}
		return show(out, o.asJSON, cli.Envelope{Verb: o.verb, OK: true, At: time.Now(), Data: index},
			func(page cli.Page) []string { return docsIndexPage(page, corpus) })
	case strings.ContainsAny(query, " \t"):
		return searchDocs(o, query, corpus)
	}
	at := slices.IndexFunc(corpus.Pages, func(page docsPage) bool { return page.Topic == query })
	if at < 0 {
		closest := corpus.topics()
		slices.SortStableFunc(closest, func(a, b string) int { return cmp.Compare(editDistance(query, a), editDistance(query, b)) })
		return o.fail(problemError{What: fmt.Sprintf("no topic %q", query), Hint: "tofu docs " + closest[0]})
	}
	topic := host.DocsTopic{DocsPage: corpus.Pages[at]}
	if topic.Topic == "settings" {
		topic.Settings = settingDocs()
	}
	return show(out, o.asJSON, cli.Envelope{Verb: o.verb, OK: true, At: time.Now(), Data: topic}, func(page cli.Page) []string {
		lines := append([]string{page.Subject(topic.Title)}, cli.Indent(page.Label(topic.Summary))...)
		return append(append(lines, ""), append(docsBody(page, topic.Body), settingsTable(page, topic.Settings)...)...)
	})
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
		corpus.Pages = append(corpus.Pages, page)
	}
	data, err := fs.ReadFile(files, docsIndexFile)
	if err != nil {
		return corpus, err
	}
	if corpus.Entries, err = parseDocsIndex(string(data)); err != nil {
		return corpus, err
	}
	for _, entry := range corpus.Entries {
		if !slices.ContainsFunc(corpus.Pages, func(page docsPage) bool { return page.Topic == entry.Topic }) {
			return corpus, fmt.Errorf("%s: %q names the topic %q, which has no page", docsIndexFile, entry.Ask, entry.Topic)
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
	page := docsPage{Topic: fields["topic"], Title: fields["title"], Summary: fields["summary"], Body: strings.TrimLeft(body, "\n")}
	if page.Topic == "" || page.Title == "" || page.Summary == "" {
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
			entry.Ask = value
		case "do":
			entry.Do = value
		case "check":
			entry.Check = value
		case "topic":
			entry.Topic = value
		default:
			return nil, fmt.Errorf("%s:%d: %q is not ask, do, check or topic", docsIndexFile, n+1, key)
		}
	}
	for _, entry := range entries {
		if entry.Ask == "" || entry.Do == "" || entry.Check == "" || entry.Topic == "" {
			return nil, fmt.Errorf("%s: %q lacks one of ask, do, check and topic", docsIndexFile, entry.Ask)
		}
	}
	return entries, nil
}

func docsIndexPage(page cli.Page, corpus docsCorpus) []string {
	lines := page.Title("Docs", []string{strconv.Itoa(len(corpus.Entries)) + " asks"}, cli.Verdict{Text: strconv.Itoa(len(corpus.Pages)) + " topics"})
	for _, topic := range corpus.Pages {
		lines = append(lines, "", page.Subject(topic.Topic)+cli.Gap+page.Label(topic.Summary))
		for _, entry := range corpus.Entries {
			if entry.Topic == topic.Topic {
				lines = append(lines, docsMatchLines(page, docsMatch{entry.Ask, entry.Do})...)
			}
		}
	}
	return append(append(lines, ""), cli.Indent(page.Hint("tofu docs <topic>"), page.Hint(`tofu docs "a few words"`))...)
}

func docsMatchLines(page cli.Page, match docsMatch) []string {
	return cli.Indent(match.Text, cli.Gap+page.Hint(match.Do))
}

func searchDocs(o verbOutput, query string, corpus docsCorpus) int {
	asked := slices.Compact(slices.Sorted(slices.Values(docsWords(strings.ToLower(query)))))
	type hit struct {
		score int
		match docsMatch
	}
	var hits []hit
	score := func(text string, match docsMatch) {
		words := docsWords(strings.ToLower(text))
		overlap := 0
		for _, word := range asked {
			if slices.Contains(words, word) {
				overlap++
			}
		}
		if overlap > 0 {
			hits = append(hits, hit{overlap, match})
		}
	}
	for _, entry := range corpus.Entries {
		score(entry.Ask+" "+entry.Do+" "+entry.Topic, docsMatch{entry.Ask, entry.Do})
	}
	for _, page := range corpus.Pages {
		score(page.Topic+" "+page.Title+" "+page.Summary+" "+docsHeadings(page.Body), docsMatch{page.Title, "tofu docs " + page.Topic})
	}
	if len(hits) == 0 {
		return o.fail(problemError{What: fmt.Sprintf("nothing matches %q", query), Hint: "tofu docs"})
	}
	slices.SortStableFunc(hits, func(a, b hit) int { return cmp.Compare(b.score, a.score) })
	matches := make([]docsMatch, len(hits))
	for i, one := range hits {
		matches[i] = one.match
	}
	data := struct {
		Query   string      `json:"query"`
		Matches []docsMatch `json:"matches"`
	}{query, matches}
	return show(o.out, o.asJSON, cli.Envelope{Verb: o.verb, OK: true, At: time.Now(), Data: data}, func(page cli.Page) []string {
		lines := append(page.Title("Docs", []string{strconv.Quote(query)}, cli.Verdict{Text: strconv.Itoa(len(matches)) + " matches"}), "")
		for _, match := range matches {
			lines = append(lines, docsMatchLines(page, match)...)
		}
		return lines
	})
}

func docsBody(page cli.Page, body string) []string {
	width := page.Width - len(cli.Gap)
	var lines []string
	lead, text := "", ""
	flush := func() {
		if text == "" {
			return
		}
		for i, line := range widget.Wrap(text, width-len(lead)) {
			if i > 0 {
				lead = strings.Repeat(" ", len(lead))
			}
			lines = append(lines, cli.Gap+lead+line)
		}
		lead, text = "", ""
	}
	blank := func() {
		if len(lines) > 0 && lines[len(lines)-1] != "" {
			lines = append(lines, "")
		}
	}
	for _, line := range strings.Split(strings.TrimRight(body, "\n"), "\n") {
		heading := strings.TrimLeft(line, "#")
		switch {
		case heading != line && strings.HasPrefix(heading, " "):
			flush()
			blank()
			lines = append(lines, page.Section(strings.TrimSpace(heading), cli.Verdict{}))
		case strings.TrimSpace(line) == "":
			flush()
			blank()
		case strings.HasPrefix(line, "    "):
			flush()
			lines = append(lines, cli.Gap+line)
		case strings.HasPrefix(line, "- "):
			flush()
			lead, text = "- ", line[len("- "):]
		default:
			text = strings.TrimSpace(text + " " + strings.TrimSpace(line))
		}
	}
	flush()
	return lines
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

func settingDocs() []settingDoc {
	var table []settingDoc
	for _, spec := range settingspkg.Default() {
		table = append(table, settingDoc{Key: spec.Key, Category: spec.Category, Default: settingDefault(spec), Takes: settingTakes(spec), Restart: spec.Restart, Description: spec.Description})
	}
	return table
}

func settingsTable(page cli.Page, table []settingDoc) []string {
	rows := make([]cli.Row, len(table))
	for i, setting := range table {
		rows[i] = cli.Row{Cells: []string{setting.Key, setting.Default}}
		if setting.Restart {
			rows[i].Detail = "on the next start"
		}
	}
	var lines []string
	for i, line := range page.Rows(rows) {
		if i == 0 || table[i].Category != table[i-1].Category {
			lines = append(lines, "", page.Section(table[i].Category, cli.Verdict{}))
		}
		lines = append(lines, cli.Indent(line)...)
		under := strings.Repeat(cli.Gap, 3)
		for _, wrapped := range widget.Wrap(table[i].Takes, page.Width-len(under)) {
			lines = append(lines, under+wrapped)
		}
		for _, wrapped := range widget.Wrap(table[i].Description, page.Width-len(under)) {
			lines = append(lines, under+page.Label(wrapped))
		}
	}
	return lines
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

func usageVerbs() []string {
	_, listing, _ := strings.Cut(usage, "\nVerbs:\n")
	var verbs []string
	for _, line := range strings.Split(listing, "\n") {
		if name, indented := strings.CutPrefix(line, "  "); indented && name != "" && name[0] != ' ' {
			verbs = append(verbs, strings.Fields(name)[0])
		}
	}
	return verbs
}

func docsCoverage(verbs []string) (docsCorpus, []error) {
	corpus, err := loadDocs()
	if err != nil {
		return corpus, []error{fmt.Errorf("docs: %w", err)}
	}
	texts := make([]string, 0, len(corpus.Pages)+len(corpus.Entries))
	for _, page := range corpus.Pages {
		texts = append(texts, page.Body)
	}
	for _, entry := range corpus.Entries {
		texts = append(texts, entry.Ask+" "+entry.Do+" "+entry.Check)
	}
	named := map[string]bool{}
	for _, text := range texts {
		words := docsWords(text)
		for i, word := range words {
			named[word] = true
			if i > 0 && words[i-1] == "tofu" {
				named["tofu "+word] = true
			}
		}
	}
	var missing []error
	for _, spec := range settingspkg.Default() {
		if !named[spec.Key] {
			missing = append(missing, fmt.Errorf("docs: no page or index entry names the setting %s", spec.Key))
		}
	}
	for _, verb := range verbs {
		if !named["tofu "+verb] {
			missing = append(missing, fmt.Errorf("docs: no page or index entry names the verb %s as tofu %s", verb, verb))
		}
	}
	return corpus, missing
}
