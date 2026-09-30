package tasks

import (
	"fmt"
	"math/rand/v2"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
)

var youtubeQueries = []string{
	"Kurzgesagt black holes", "NASA Artemis launch", "Tiny Desk Concert", "Go programming tutorial", "how to make pão de queijo",
	"chess Italian game opening", "Tom Jobim Garota de Ipanema live", "4K drone footage Iceland", "SpaceX Starship flight test", "Veritasium",
}

const youtubeQuality = "1080p"

var qualityChoice = regexp.MustCompile(`(?m)^\s*- menuitemradio "(\d{3,4}p[^"]*)" \[([^\]]*)\]`)

func onYouTube(path string) func(*url.URL) bool {
	return func(address *url.URL) bool {
		return strings.HasSuffix(address.Host, "youtube.com") && address.Path == path
	}
}

func chosenQuality(page Page) (string, bool) {
	shown := false
	for _, choice := range qualityChoice.FindAllStringSubmatch(page.Text, -1) {
		shown = true
		if strings.Contains(choice[2], "checked=true") {
			return choice[1], true
		}
	}
	return "", shown
}

func drawYouTube(seed int64, _ time.Time) Task {
	query := youtubeQueries[rand.New(rand.NewPCG(uint64(seed), 2)).IntN(len(youtubeQueries))]
	return Task{
		Name: "youtube",
		Prompt: fmt.Sprintf("Start at google.com and reach youtube.com. Search YouTube for %q and open the first result that is not an ad. "+
			"Play it, set the quality to %s through the player's settings menu, and report the video's title, its channel, and the quality the player shows. Do not sign in. Work in one tab.",
			query, youtubeQuality),
		Steps: []Step{
			{"google.com came before YouTube", func(e Evidence) bool {
				google := e.visited(onGoogle)
				return google >= 0 && google < e.visited(func(address *url.URL) bool { return strings.HasSuffix(address.Host, "youtube.com") })
			}},
			{"the drawn query was searched, then a video opened", func(e Evidence) bool {
				searched := e.visited(func(address *url.URL) bool {
					return onYouTube("/results")(address) && strings.EqualFold(address.Query().Get("search_query"), query)
				})
				return searched >= 0 && e.visited(onYouTube("/watch")) > searched
			}},
			{"the final tab is a video", func(e Evidence) bool {
				final, found := e.final()
				return found && onYouTube("/watch")(final.URL) && final.URL.Query().Get("v") != ""
			}},
			{"the report names the video's title", func(e Evidence) bool {
				final, found := e.final()
				return found && reportSays(e, strings.TrimSuffix(final.Title, " - YouTube"))
			}},
			{"the report says 1080p, and the quality menu shows it chosen where a snapshot shows the menu", func(e Evidence) bool {
				for _, page := range slices.Backward(e.Pages) {
					if chosen, shown := chosenQuality(page); shown && onYouTube("/watch")(page.URL) {
						return strings.HasPrefix(chosen, youtubeQuality) && reportSays(e, youtubeQuality)
					}
				}
				return reportSays(e, youtubeQuality)
			}},
			{"the task stayed in one tab", oneTab},
		},
	}
}
