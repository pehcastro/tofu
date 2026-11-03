package tasks

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const booksHost = "books.toscrape.com"

var (
	bookPrice    = regexp.MustCompile(`£(\d+\.\d\d)`)
	bookStock    = regexp.MustCompile(`In stock \((\d+) available\)`)
	bookTitle    = regexp.MustCompile(`(?m)^\s*- heading "([^"]+)" \[level=1`)
	categoryPage = regexp.MustCompile(`Page \d+ of (\d+)`)
	bookPath     = regexp.MustCompile(`^/catalogue/[^/]+_\d+/index\.html$`)
)

func inMystery(address *url.URL) bool {
	return strings.HasSuffix(address.Host, booksHost) && strings.Contains(address.Path, "/category/books/mystery_")
}

func mysteryPagesSeen(e Evidence) ([]Page, bool) {
	var seen []Page
	pageCount := 1
	for _, page := range e.Pages {
		if !inMystery(page.URL) {
			continue
		}
		seen = append(seen, page)
		if match := categoryPage.FindStringSubmatch(page.Text); match != nil {
			pageCount, _ = strconv.Atoi(match[1])
		}
	}
	for number := 2; number <= pageCount; number++ {
		if !slices.ContainsFunc(seen, func(page Page) bool { return strings.HasSuffix(page.URL.Path, fmt.Sprintf("/page-%d.html", number)) }) {
			return seen, false
		}
	}
	return seen, len(seen) > 0
}

func finalBook(e Evidence) (Page, bool) {
	final, found := e.final()
	return final, found && strings.HasSuffix(final.URL.Host, booksHost) && bookPath.MatchString(final.URL.Path)
}

func firstOf(pattern *regexp.Regexp, text string) string {
	if match := pattern.FindStringSubmatch(text); match != nil {
		return match[1]
	}
	return ""
}

var books = Task{
	Name: "books",
	Prompt: "On books.toscrape.com, start at the home page and open the Mystery category. Go through every page of that category and find the cheapest book. " +
		"Open that book's page, stay on it, and report its title, its price and how many are in stock. Work in one tab and buy nothing.",
	Steps: []Step{
		{"the home page came before the category", func(e Evidence) bool {
			home := e.visited(func(address *url.URL) bool {
				return strings.HasSuffix(address.Host, booksHost) && (address.Path == "/" || address.Path == "/index.html")
			})
			return home >= 0 && home < e.visited(inMystery)
		}},
		{"the Mystery category was reached", func(e Evidence) bool { return e.visited(inMystery) >= 0 }},
		{"every page of the category was seen", func(e Evidence) bool {
			_, all := mysteryPagesSeen(e)
			return all
		}},
		{"the final tab is a book page", func(e Evidence) bool {
			_, isBook := finalBook(e)
			return isBook
		}},
		{"the book is the cheapest on every page of the category", func(e Evidence) bool {
			book, isBook := finalBook(e)
			pages, all := mysteryPagesSeen(e)
			if !isBook || !all {
				return false
			}
			var prices []float64
			for _, page := range pages {
				for _, price := range bookPrice.FindAllStringSubmatch(page.Text, -1) {
					value, _ := strconv.ParseFloat(price[1], 64)
					prices = append(prices, value)
				}
			}
			chosen, err := strconv.ParseFloat(firstOf(bookPrice, book.Text), 64)
			return len(prices) > 0 && err == nil && chosen == slices.Min(prices)
		}},
		{"the report names the book's title", func(e Evidence) bool {
			book, isBook := finalBook(e)
			return isBook && reportSays(e, firstOf(bookTitle, book.Text))
		}},
		{"the report gives the book's price", func(e Evidence) bool {
			book, isBook := finalBook(e)
			return isBook && reportSays(e, "£"+firstOf(bookPrice, book.Text))
		}},
		{"the report gives the book's stock count", func(e Evidence) bool {
			book, isBook := finalBook(e)
			stock := firstOf(bookStock, book.Text)
			return isBook && stock != "" && regexp.MustCompile(`(?i)\b`+stock+`\b[^\n]*(available|stock)|(available|stock)[^\n]*\b`+stock+`\b`).MatchString(e.Run.Report)
		}},
		{"the task stayed in one tab", oneTab},
	},
}
