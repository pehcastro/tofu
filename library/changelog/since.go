package changelog

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
)

type Release struct {
	Version string `json:"version"`
	Heading string `json:"heading"`
	Body    string `json:"body"`
}

const HeadingMark = "## "

func Parse(markdown string) []Release {
	versionLine := regexp.MustCompile(`^##\s+\[?(\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?)\]?`)
	var releases []Release
	var current Release
	var body []string
	open := false
	flush := func() {
		if open {
			current.Body = strings.Trim(strings.Join(body, "\n"), "\n")
			releases = append(releases, current)
		}
		open, body = false, nil
	}
	for _, line := range strings.Split(markdown, "\n") {
		if !strings.HasPrefix(line, HeadingMark) {
			if open {
				body = append(body, line)
			}
			continue
		}
		flush()
		match := versionLine.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		current = Release{Version: match[1], Heading: strings.TrimSpace(strings.TrimPrefix(line, HeadingMark))}
		open = true
	}
	flush()
	return releases
}

func Between(markdown, from, to string, capBytes int) string {
	releases := Parse(markdown)
	newest := max(slices.IndexFunc(releases, func(r Release) bool { return r.Version == to }), 0)
	oldest := slices.IndexFunc(releases, func(r Release) bool { return r.Version == from })
	if oldest <= newest {
		return ""
	}
	var text strings.Builder
	for at, release := range releases[newest:oldest] {
		section := HeadingMark + release.Heading + "\n\n" + release.Body + "\n\n"
		if text.Len()+len(section) <= capBytes {
			text.WriteString(section)
			continue
		}
		if cut := strings.LastIndex(section[:max(capBytes-text.Len(), 0)], "\n"); cut > 0 {
			text.WriteString(section[:cut] + "\n\n")
		}
		text.WriteString("cut at " + strconv.Itoa(capBytes) + " bytes, " + strconv.Itoa(oldest-newest-at) + " versions not whole here: tofu changelog --all prints them")
		break
	}
	return strings.TrimSpace(text.String())
}
