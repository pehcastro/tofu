package tasks

import (
	"fmt"
	"math/rand/v2"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

type film []string

var films = []film{
	{"The Godfather", "O Poderoso Chefão"},
	{"Spirited Away", "A Viagem de Chihiro"},
	{"City of God", "Cidade de Deus"},
	{"Parasite", "Parasita"},
	{"The Matrix", "Matrix"},
	{"Pulp Fiction", "Pulp Fiction: Tempo de Violência"},
	{"Back to the Future", "De Volta para o Futuro"},
	{"Amélie", "O Fabuloso Destino de Amélie Poulain"},
	{"Central Station", "Central do Brasil"},
	{"Alien", "Alien, o Oitavo Passageiro"},
	{"Casablanca"},
	{"Inception", "A Origem"},
	{"Seven Samurai", "Os Sete Samurais"},
}

var (
	imdbFind      = regexp.MustCompile(`^(?:/[a-z]{2}(?:-[a-z]{2})?)?/find/?$`)
	imdbTitle     = regexp.MustCompile(`^(?:/[a-z]{2}(?:-[a-z]{2})?)?/title/tt\d+/?$`)
	titleYear     = regexp.MustCompile(`\((\d{4})\)`)
	directorLabel = regexp.MustCompile(`(?i)"(?:directors?|direção|diretor(?:a|es)?)"`)
	linkName      = regexp.MustCompile(`link "([^"]+)"`)
	ratingShown   = regexp.MustCompile(`(\d[.,]\d)\s*/\s*10`)
	ratingSaid    = regexp.MustCompile(`\b(\d[.,]\d)\b`)
)

func onIMDb(path *regexp.Regexp) func(*url.URL) bool {
	return func(address *url.URL) bool {
		return strings.HasSuffix(address.Host, "imdb.com") && path.MatchString(address.Path)
	}
}

func (f film) titles(page Page) bool {
	return slices.ContainsFunc(f, func(name string) bool {
		return strings.HasPrefix(strings.ToLower(page.Title), strings.ToLower(name)+" (")
	})
}

func (f film) searched(query string) bool {
	return slices.ContainsFunc(f, func(name string) bool { return strings.Contains(strings.ToLower(query), strings.ToLower(name)) })
}

func directorsIn(page string) []string {
	var names []string
	for _, label := range directorLabel.FindAllStringIndex(page, -1) {
		if name := linkName.FindStringSubmatch(page[label[1]:]); name != nil {
			names = append(names, name[1])
		}
	}
	return names
}

func ratings(rating *regexp.Regexp, text string) []string {
	var values []string
	for _, match := range rating.FindAllStringSubmatch(text, -1) {
		values = append(values, strings.ReplaceAll(match[1], ",", "."))
	}
	return values
}

func drawIMDb(seed int64) Task {
	drawn := films[rand.New(rand.NewPCG(uint64(seed), 4)).IntN(len(films))]
	titlePage := func(e Evidence) (Page, bool) {
		for _, page := range slices.Backward(e.Pages) {
			if onIMDb(imdbTitle)(page.URL) && drawn.titles(page) {
				return page, true
			}
		}
		return Page{}, false
	}
	return Task{
		Name: "imdb",
		Prompt: fmt.Sprintf("Start at google.com and reach imdb.com. Search IMDb for the film %q and open its title page. "+
			"Report its release year, its director and its IMDb rating as the title page shows them. Do not sign in. Work in one tab.", drawn[0]),
		Steps: []Step{
			{"google.com came before imdb.com", func(e Evidence) bool {
				google := e.visited(onGoogle)
				return google >= 0 && google < e.visited(func(address *url.URL) bool { return strings.HasSuffix(address.Host, "imdb.com") })
			}},
			{"the drawn film was searched, then a title page opened", func(e Evidence) bool {
				searched := e.visited(func(address *url.URL) bool {
					return onIMDb(imdbFind)(address) && drawn.searched(address.Query().Get("q"))
				})
				return searched >= 0 && e.visited(onIMDb(imdbTitle)) > searched
			}},
			{"the final tab is the drawn film's title page", func(e Evidence) bool {
				final, found := e.final()
				return found && onIMDb(imdbTitle)(final.URL) && drawn.titles(final)
			}},
			{"the report gives the year the title page shows", func(e Evidence) bool {
				page, found := titlePage(e)
				year := titleYear.FindStringSubmatch(page.Title)
				return found && year != nil && namedIn(year[1], e.Run.Report)
			}},
			{"the report names a director the title page shows", func(e Evidence) bool {
				page, found := titlePage(e)
				return found && slices.ContainsFunc(directorsIn(page.Text), func(name string) bool { return namedIn(name, e.Run.Report) })
			}},
			{"the report gives the rating the title page shows", func(e Evidence) bool {
				page, found := titlePage(e)
				return found && sharesOne(ratings(ratingSaid, e.Run.Report), ratings(ratingShown, page.Text))
			}},
			{"the task stayed in one tab", oneTab},
		},
	}
}
