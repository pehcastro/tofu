package tasks

import (
	"fmt"
	"math/rand/v2"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

var npmPackages = []string{"react", "lodash", "express", "axios", "chalk", "zod", "typescript", "vite", "dayjs", "commander", "rxjs", "semver"}

var (
	licenses        = []string{"MIT", "ISC", "Apache-2.0", "BSD-2-Clause", "BSD-3-Clause", "0BSD", "BlueOak-1.0.0", "MPL-2.0", "Unlicense"}
	semverShown     = regexp.MustCompile(`\b\d+\.\d+\.\d+(?:-[0-9A-Za-z.]+)?\b`)
	weeklyLabel     = regexp.MustCompile(`(?i)weekly downloads|downloads semanais`)
	downloadCount   = regexp.MustCompile(`(?:^|[^\pL\pN=.,])(\d{1,3}(?:[.,]\d{3})+|\d{4,})`)
	digitSeparators = strings.NewReplacer(".", "", ",", "")
)

func onNPM(path string) func(*url.URL) bool {
	return func(address *url.URL) bool {
		return strings.HasSuffix(address.Host, "npmjs.com") && address.Path == path
	}
}

func downloadCounts(text string) []string {
	var found []string
	for _, match := range downloadCount.FindAllStringSubmatch(text, -1) {
		found = append(found, digitSeparators.Replace(match[1]))
	}
	return found
}

func weeklyDownloads(page string) []string {
	label := weeklyLabel.FindStringIndex(page)
	if label == nil {
		return nil
	}
	counts := downloadCounts(page[label[1]:])
	return counts[:min(1, len(counts))]
}

func licensesIn(text string) []string {
	spaced := strings.ReplaceAll(text, "-", " ")
	return slices.DeleteFunc(slices.Clone(licenses), func(license string) bool {
		return !namedIn(strings.ReplaceAll(license, "-", " "), spaced)
	})
}

func drawNPM(seed int64) Task {
	name := npmPackages[rand.New(rand.NewPCG(uint64(seed), 3)).IntN(len(npmPackages))]
	packagePage := onNPM("/package/" + name)
	shown := func(e Evidence, values func(string) []string) bool {
		page, found := e.lastPage(packagePage)
		return found && sharesOne(values(e.Run.Report), values(page.Text))
	}
	return Task{
		Name: "npm",
		Prompt: fmt.Sprintf("Start at google.com and reach npmjs.com. Search npm for the package %q and open its package page. "+
			"Report its latest version, its weekly downloads and its license exactly as the package page shows them. Do not sign in. Work in one tab.", name),
		Steps: []Step{
			{"google.com came before npmjs.com", func(e Evidence) bool {
				google := e.visited(onGoogle)
				return google >= 0 && google < e.visited(func(address *url.URL) bool { return strings.HasSuffix(address.Host, "npmjs.com") })
			}},
			{"the drawn package was searched, then its page opened", func(e Evidence) bool {
				searched := e.visited(func(address *url.URL) bool {
					return onNPM("/search")(address) && strings.EqualFold(strings.TrimSpace(address.Query().Get("q")), name)
				})
				return searched >= 0 && e.visited(packagePage) > searched
			}},
			{"the final tab is the drawn package's page", func(e Evidence) bool {
				final, found := e.final()
				return found && packagePage(final.URL)
			}},
			{"the report gives a version the package page shows", func(e Evidence) bool {
				return shown(e, func(text string) []string { return semverShown.FindAllString(text, -1) })
			}},
			{"the report gives the weekly downloads the package page shows", func(e Evidence) bool {
				page, found := e.lastPage(packagePage)
				return found && sharesOne(downloadCounts(e.Run.Report), weeklyDownloads(page.Text))
			}},
			{"the report names a license the package page shows", func(e Evidence) bool { return shown(e, licensesIn) }},
			{"the task stayed in one tab", oneTab},
		},
	}
}
