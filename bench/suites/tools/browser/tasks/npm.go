package tasks

import (
	"fmt"
	"math/rand/v2"
	"net/url"
	"regexp"
	"strings"
)

var npmPackages = []string{"react", "lodash", "express", "axios", "chalk", "zod", "typescript", "vite", "dayjs", "commander", "rxjs", "semver"}

var (
	downloadsHeading = regexp.MustCompile(`heading "Weekly Downloads"`)
	versionHeading   = regexp.MustCompile(`heading "Version"`)
	licenseHeading   = regexp.MustCompile(`heading "License"`)
	semverShown      = regexp.MustCompile(`\b\d+\.\d+\.\d+(?:-[0-9A-Za-z.]+)?\b`)
	downloadCount    = regexp.MustCompile(`(?:^|[^\pL\pN=.,])(\d{1,3}(?:[.,]\d{3})+|\d{4,})`)
	licenseName      = regexp.MustCompile(`(?i)\b(?:MIT|ISC|Apache[- ]2\.0|BSD[- ][23][- ]Clause|0BSD|BlueOak[- ]1\.0\.0|MPL[- ]2\.0|Unlicense)\b`)
	digitSeparators  = strings.NewReplacer(".", "", ",", "")
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

func licensesIn(text string) []string {
	var found []string
	for _, name := range licenseName.FindAllString(text, -1) {
		found = append(found, strings.ToUpper(strings.ReplaceAll(name, " ", "-")))
	}
	return found
}

func versions(text string) []string { return semverShown.FindAllString(text, -1) }

func drawNPM(seed int64) Task {
	name := npmPackages[rand.New(rand.NewPCG(uint64(seed), 3)).IntN(len(npmPackages))]
	packagePage := onNPM("/package/" + name)
	shownUnder := func(heading *regexp.Regexp, values func(string) []string) func(Evidence) bool {
		return func(e Evidence) bool {
			page, found := e.lastPage(packagePage)
			return found && sharesOne(values(e.Run.Report), firstAfter(heading, page.Text, values))
		}
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
				return e.visitedAfter(searched, packagePage)
			}},
			{"the final tab is the drawn package's page", func(e Evidence) bool {
				final, found := e.final()
				return found && packagePage(final.URL)
			}},
			{"the report gives the version under the page's Version heading", shownUnder(versionHeading, versions)},
			{"the report gives the count under the page's Weekly Downloads heading", shownUnder(downloadsHeading, downloadCounts)},
			{"the report names the license under the page's License heading", shownUnder(licenseHeading, licensesIn)},
			{"the task stayed in one tab", oneTab},
		},
	}
}
