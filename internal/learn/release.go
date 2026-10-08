package learn

import (
	"cmp"
	"regexp"
	"slices"
	"strings"
	"time"
)

var (
	releaseHeading = regexp.MustCompile(`^## \[?(\d+\.\d+\.\d+[^\]\s]*)\]? - (\d{4}-\d{2}-\d{2})`)
	noteMarkup     = strings.NewReplacer("**", "", "`", "")
)

type Release struct {
	Version string   `json:"version"`
	Day     string   `json:"day"`
	Notes   []string `json:"-"`
}

func Releases(markdown string) []Release {
	var releases []Release
	for _, text := range strings.Split(strings.ReplaceAll(markdown, "\r\n", "\n"), "\n") {
		text = strings.TrimSpace(text)
		if found := releaseHeading.FindStringSubmatch(text); found != nil {
			releases = append(releases, Release{Version: found[1], Day: found[2]})
			continue
		}
		if len(releases) == 0 || text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		last := &releases[len(releases)-1]
		if note, bullet := strings.CutPrefix(text, "- "); bullet || len(last.Notes) == 0 {
			last.Notes = append(last.Notes, noteMarkup.Replace(note))
			continue
		}
		last.Notes[len(last.Notes)-1] += " " + noteMarkup.Replace(text)
	}
	return releases
}

func dayOf(at time.Time) string { return at.Local().Format(time.DateOnly) }

func builtOn(releases []Release, at time.Time) (least, most int) {
	day := dayOf(at)
	most = slices.IndexFunc(releases, func(r Release) bool { return r.Day <= day })
	least = slices.IndexFunc(releases, func(r Release) bool { return r.Day < day })
	if most < 0 {
		most = len(releases)
	}
	if least < 0 {
		least = len(releases)
	}
	return least, most
}

func builtText(releases []Release, at time.Time) string {
	if len(releases) == 0 {
		return ""
	}
	least, most := builtOn(releases, at)
	name := func(i int) string {
		if i == len(releases) {
			return "before " + releases[len(releases)-1].Version
		}
		return releases[i].Version
	}
	if least == most {
		return name(least)
	}
	return name(least) + " to " + name(most)
}

func (f *Finding) markFixed(releases []Release) {
	last := f.Quotes[len(f.Quotes)-1].At
	least, _ := builtOn(releases, last)
	f.Built, f.FixedIn = builtText(releases, last), f.offered
	if f.FixedIn == "" && f.byWords {
		f.FixedIn = noteMatch(releases[:least], stemsOf(cmp.Or(strings.Join(f.words, " "), f.Title)+" "+f.Rule))
	}
	at := slices.IndexFunc(releases, func(r Release) bool { return r.Version == f.FixedIn })
	if at < 0 || at >= least {
		f.FixedIn = ""
		return
	}
	f.FixedSameDay = releases[at].Day == dayOf(last)
}

func noteMatch(newer []Release, stems []string) string {
	for _, release := range slices.Backward(newer) {
		for _, note := range release.Notes {
			if both := shared(stemsOf(note), stems, nil); both >= FixNoteWords && 2*both >= len(stems) {
				return release.Version
			}
		}
	}
	return ""
}
