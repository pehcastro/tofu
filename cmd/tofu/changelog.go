package main

import (
	"errors"
	"io"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"tofu/interface/cli"
	"tofu/internal/konst"
	"tofu/internal/sys"
	"tofu/internal/widget"
)

const (
	changelogUsage       = "tofu changelog [--all] [--json]"
	changelogAllFlag     = "--all"
	changelogSeenFile    = "changelog-seen"
	changelogSeenMode    = 0o600
	changelogHeadingMark = "## "
	changelogSectionMark = "### "
	changelogBulletMark  = "- "
)

var (
	changelogVersionLine = regexp.MustCompile(`^##\s+\[?(\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?)\]?`)
	prereleaseRun        = regexp.MustCompile(`\d+|\D+`)
	markdownLink         = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	markdownBold         = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	markdownCode         = regexp.MustCompile("`([^`]*)`")
)

type semver struct {
	major      int
	minor      int
	patch      int
	prerelease string
}

func (v semver) after(other semver) bool {
	if v.major != other.major {
		return v.major > other.major
	}
	if v.minor != other.minor {
		return v.minor > other.minor
	}
	if v.patch != other.patch {
		return v.patch > other.patch
	}
	if v.prerelease == "" || other.prerelease == "" {
		return v.prerelease == "" && other.prerelease != ""
	}
	mine, theirs := prereleaseRun.FindAllString(v.prerelease, -1), prereleaseRun.FindAllString(other.prerelease, -1)
	for i := range min(len(mine), len(theirs)) {
		run, other := mine[i], theirs[i]
		if run[0] >= '0' && run[0] <= '9' && other[0] >= '0' && other[0] <= '9' {
			run, other = strings.TrimLeft(run, "0"), strings.TrimLeft(other, "0")
			if len(run) != len(other) {
				return len(run) > len(other)
			}
		}
		if run != other {
			return run > other
		}
	}
	return len(mine) > len(theirs)
}

func parseSemver(text string) (semver, bool) {
	withoutBuild, _, _ := strings.Cut(strings.TrimSpace(text), "+")
	core, prerelease, _ := strings.Cut(withoutBuild, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return semver{}, false
	}
	major, majorErr := strconv.Atoi(parts[0])
	minor, minorErr := strconv.Atoi(parts[1])
	patch, patchErr := strconv.Atoi(parts[2])
	if majorErr != nil || minorErr != nil || patchErr != nil {
		return semver{}, false
	}
	return semver{major: major, minor: minor, patch: patch, prerelease: prerelease}, true
}

type changelogVersion struct {
	Version string `json:"version"`
	Heading string `json:"heading"`
	Body    string `json:"body"`

	number semver
}

func parseChangelog(markdown string) []changelogVersion {
	var versions []changelogVersion
	var current changelogVersion
	var body []string
	open := false
	flush := func() {
		if open {
			current.Body = strings.Trim(strings.Join(body, "\n"), "\n")
			versions = append(versions, current)
		}
		open, body = false, nil
	}
	for _, line := range strings.Split(markdown, "\n") {
		if !strings.HasPrefix(line, changelogHeadingMark) {
			if open {
				body = append(body, line)
			}
			continue
		}
		flush()
		match := changelogVersionLine.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		number, _ := parseSemver(match[1])
		current = changelogVersion{
			Version: match[1],
			Heading: strings.TrimSpace(strings.TrimPrefix(line, changelogHeadingMark)),
			number:  number,
		}
		open = true
	}
	flush()
	return versions
}

func changelogSince(versions []changelogVersion, seen string) []changelogVersion {
	last, known := parseSemver(seen)
	var shown []changelogVersion
	for _, version := range versions {
		if !known && version.Version == konst.Version {
			shown = append(shown, version)
		}
		if known && version.number.after(last) {
			shown = append(shown, version)
		}
	}
	return shown
}

func plainMarkdown(text string) string {
	text = markdownLink.ReplaceAllString(text, "$1")
	text = markdownBold.ReplaceAllString(text, "$1")
	return markdownCode.ReplaceAllString(text, "$1")
}

func changelogWrap(text, first, rest string, width int) []string {
	lines := widget.Wrap(text, width-widget.Cells(first))
	for i := range lines {
		if i == 0 {
			lines[i] = first + lines[i]
			continue
		}
		lines[i] = rest + lines[i]
	}
	return lines
}

func changelogBody(markdown string, width int) []string {
	deep := reportIndent + reportIndent
	var lines []string
	gap := false
	for _, raw := range strings.Split(markdown, "\n") {
		text := plainMarkdown(strings.TrimSpace(raw))
		switch {
		case text == "":
			gap = len(lines) > 0
		case strings.HasPrefix(text, changelogSectionMark):
			lines = append(lines, "", reportIndent+strings.TrimPrefix(text, changelogSectionMark))
			gap = false
		case strings.HasPrefix(text, changelogBulletMark):
			lines = append(lines, changelogWrap(strings.TrimPrefix(text, changelogBulletMark), deep+changelogBulletMark, deep+reportIndent, width)...)
			gap = false
		default:
			if gap {
				lines = append(lines, "")
			}
			lines = append(lines, changelogWrap(text, reportIndent, reportIndent, width)...)
			gap = false
		}
	}
	for len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	return lines
}

func changelogLines(page cli.Page, all bool, seen string, shown []changelogVersion) []string {
	verdict := cli.Verdict{Mark: cli.Active, Text: strconv.Itoa(len(shown)) + " unread"}
	switch {
	case all:
		verdict = cli.Verdict{Text: plural(len(shown), "version")}
	case len(shown) == 0 && seen == "":
		verdict = cli.Verdict{Mark: cli.Done, Text: "nothing new"}
	case len(shown) == 0:
		verdict = cli.Verdict{Mark: cli.Done, Text: "nothing new since " + seen}
	}
	lines := page.Title("Changelog", []string{"tofu " + konst.Version}, verdict)
	for _, version := range shown {
		lines = append(lines, "", page.Subject(strings.Replace(version.Heading, " - ", " · ", 1)))
		lines = append(lines, changelogBody(version.Body, page.Width)...)
	}
	return lines
}

func changelogSeen() string {
	dir, err := sys.HomeConfigDir()
	if err != nil {
		return ""
	}
	data, err := sys.ReadFile(filepath.Join(dir, changelogSeenFile))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func recordChangelogSeen() error {
	dir, err := sys.HomeConfigDir()
	if err != nil {
		return err
	}
	return sys.WriteFile(filepath.Join(dir, changelogSeenFile), []byte(konst.Version+"\n"), changelogSeenMode)
}

func changelogVerb(args []string, markdown string, out, errOut io.Writer) int {
	o := verbOutput{verb: "changelog", usageLine: changelogUsage, out: out, errOut: errOut}
	all := false
	for _, arg := range args {
		switch arg {
		case changelogAllFlag:
			all = true
		case jsonFlag:
			o.asJSON = true
		default:
			return o.usage(errors.New("unknown argument " + strconv.Quote(arg)))
		}
	}
	versions := parseChangelog(markdown)
	if o.asJSON {
		return o.done(true, struct {
			Versions []changelogVersion `json:"versions"`
		}{versions}, nil)
	}
	if all {
		return o.done(true, nil, func(page cli.Page) []string { return changelogLines(page, true, "", versions) })
	}
	seen := changelogSeen()
	if err := recordChangelogSeen(); err != nil {
		o.errorLine(cli.Problem{What: "the version you read was not recorded: " + err.Error()})
	}
	return o.done(true, nil, func(page cli.Page) []string {
		return changelogLines(page, false, seen, changelogSince(versions, seen))
	})
}
