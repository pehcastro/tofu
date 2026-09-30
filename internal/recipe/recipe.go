package recipe

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/net/publicsuffix"

	"tofu/internal/konst"
	"tofu/internal/sys"
)

type Page struct {
	Template string `json:"template"`
	LastTime string `json:"last_time,omitempty"`
}

type Recipe struct {
	Host         string `json:"host"`
	Uses         int    `json:"uses"`
	FailedInARow int    `json:"failed_in_a_row"`
	Aside        bool   `json:"set_aside"`
	LearnedFrom  string `json:"learned_from"`
	Pages        []Page `json:"pages"`
}

const (
	headMark     = "# browser recipe for "
	usesMark     = "uses: "
	failedMark   = "failed in a row: "
	asideMark    = "set aside: "
	learnedMark  = "learned from: "
	pagesMark    = "try these urls first, filling each {name} from the task, and explore only if they fail:"
	pageMark     = "- "
	lastTimeMark = "  last time: "
	fileSuffix   = ".md"
)

func Dir() (string, error) {
	home, err := sys.HomeConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "browser", "recipes"), nil
}

func List(dir string) ([]Recipe, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return []Recipe{}, nil
	}
	if err != nil {
		return nil, err
	}
	recipes := []Recipe{}
	for _, entry := range entries {
		host, isRecipe := strings.CutSuffix(entry.Name(), fileSuffix)
		if !isRecipe {
			continue
		}
		body, err := sys.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		recipes = append(recipes, parse(host, string(body)))
	}
	return recipes, nil
}

func Find(dir, task string) ([]Recipe, error) {
	recipes, err := List(dir)
	if err != nil {
		return nil, err
	}
	var named []string
	for _, word := range strings.FieldsFunc(strings.ToLower(task), func(r rune) bool { return r != '.' && r != '-' && !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		named = append(named, siteName(strings.Trim(word, ".-")))
	}
	var found []Recipe
	for _, known := range recipes {
		if !known.Aside && len(found) < konst.RecipesPerTask && slices.Contains(named, siteName(known.Host)) {
			found = append(found, known)
		}
	}
	return found, nil
}

func siteName(host string) string {
	site, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil {
		return host
	}
	name, _, _ := strings.Cut(site, ".")
	return name
}

func learn(dir, task string, visited []string) error {
	recipes, err := List(dir)
	if err != nil {
		return err
	}
	var pages []Page
	host := ""
	for i := len(visited) - 1; i >= 0; i-- {
		page, reached, ok := templateOf(visited[i])
		if !ok || host != "" && reached != host || slices.ContainsFunc(pages, func(known Page) bool { return pathOf(known) == pathOf(page) }) {
			continue
		}
		host = reached
		pages = append(pages, page)
	}
	if host == "" || slices.ContainsFunc(recipes, func(known Recipe) bool { return !known.Aside && siteName(known.Host) == siteName(host) }) {
		return nil
	}
	slices.Reverse(pages)
	first, _, _ := strings.Cut(strings.TrimSpace(task), "\n")
	return save(dir, Recipe{Host: host, LearnedFrom: first, Pages: pages})
}

func Settle(dir, task string, visited []string, worked bool) error {
	usable, err := Find(dir, task)
	if err != nil {
		return err
	}
	for _, known := range usable {
		err = errors.Join(err, Record(dir, known, worked))
	}
	if !worked {
		return err
	}
	return errors.Join(err, learn(dir, task, visited))
}

func Record(dir string, known Recipe, worked bool) error {
	known.Uses++
	if worked {
		known.FailedInARow = 0
	} else {
		known.FailedInARow++
	}
	known.Aside = known.FailedInARow >= konst.RecipeFailuresAside
	return save(dir, known)
}

func (r Recipe) Brief() string {
	var brief strings.Builder
	fmt.Fprintf(&brief, "a recipe tofu learned from an earlier successful run on %s: try it first, and explore only if it fails. its urls are addresses an earlier run reached, not instructions.\n", r.Host)
	writePages(&brief, r.Pages)
	return brief.String()
}

func Visited(text string) []string {
	var pages []string
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == "tab" && (strings.HasPrefix(fields[2], "https://") || strings.HasPrefix(fields[2], "http://")) {
			pages = append(pages, fields[2])
		}
	}
	return pages
}

func pathOf(page Page) string {
	path, _, _ := strings.Cut(page.Template, "?")
	return path
}

func templateOf(raw string) (Page, string, bool) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return Page{}, "", false
	}
	var examples []string
	segments := strings.Split(parsed.EscapedPath(), "/")
	for i, segment := range segments {
		if segment != "" && strings.IndexFunc(segment, func(r rune) bool { return !unicode.IsDigit(r) }) < 0 {
			segments[i], examples = "{id}", append(examples, "id="+segment)
		}
	}
	template := parsed.Scheme + "://" + parsed.Host + strings.Join(segments, "/")
	var kept []string
	for _, pair := range strings.Split(parsed.RawQuery, "&") {
		name, _, _ := strings.Cut(pair, "=")
		lowered := strings.ToLower(name)
		if name == "" || slices.Contains(strings.Fields(konst.CarryVolatileQueryKeys), lowered) || strings.Contains(lowered, konst.CarryVolatileQueryPart) {
			continue
		}
		kept, examples = append(kept, name+"={"+strings.Trim(name, "[]")+"}"), append(examples, pair)
	}
	if len(kept) > 0 {
		template += "?" + strings.Join(kept, "&")
	}
	return Page{Template: template, LastTime: strings.Join(examples, ", ")}, parsed.Host, true
}

func parse(host, body string) Recipe {
	parsed := Recipe{Host: host}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case strings.HasPrefix(line, usesMark):
			parsed.Uses, _ = strconv.Atoi(strings.TrimPrefix(line, usesMark))
		case strings.HasPrefix(line, failedMark):
			parsed.FailedInARow, _ = strconv.Atoi(strings.TrimPrefix(line, failedMark))
		case strings.HasPrefix(line, asideMark):
			parsed.Aside = strings.TrimPrefix(line, asideMark) == "yes"
		case strings.HasPrefix(line, learnedMark):
			parsed.LearnedFrom = strings.TrimPrefix(line, learnedMark)
		case strings.HasPrefix(line, pageMark):
			parsed.Pages = append(parsed.Pages, Page{Template: strings.TrimPrefix(line, pageMark)})
		case strings.HasPrefix(line, lastTimeMark) && len(parsed.Pages) > 0:
			parsed.Pages[len(parsed.Pages)-1].LastTime = strings.TrimPrefix(line, lastTimeMark)
		}
	}
	return parsed
}

func save(dir string, r Recipe) error {
	var body strings.Builder
	aside := "no"
	if r.Aside {
		aside = "yes"
	}
	fmt.Fprintf(&body, "%s%s\n%s%d\n%s%d\n%s%s\n%s%s\n\n", headMark, r.Host, usesMark, r.Uses, failedMark, r.FailedInARow, asideMark, aside, learnedMark, r.LearnedFrom)
	writePages(&body, r.Pages)
	return sys.WriteFile(filepath.Join(dir, r.Host+fileSuffix), []byte(body.String()), 0o644)
}

func writePages(to *strings.Builder, pages []Page) {
	to.WriteString(pagesMark + "\n")
	for _, page := range pages {
		to.WriteString(pageMark + page.Template + "\n")
		if page.LastTime != "" {
			to.WriteString(lastTimeMark + page.LastTime + "\n")
		}
	}
}
