package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/term"

	"boji/catalog/changelog"
	"boji/internal/konst"
	"boji/internal/sys"
	"boji/internal/widget"
)

const (
	changelogFlags       = "usage: boji changelog [--all] [--json]"
	changelogAllFlag     = "--all"
	changelogSeenFile    = "changelog-seen"
	changelogSeenMode    = 0o600
	changelogHeadingMark = "## "
	changelogSectionMark = "### "
	changelogBulletMark  = "- "
)

var (
	changelogVersionLine = regexp.MustCompile(`^##\s+\[?(\d+\.\d+\.\d+)\]?`)
	markdownLink         = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	markdownBold         = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	markdownCode         = regexp.MustCompile("`([^`]*)`")
)

type semverTriple struct {
	major int
	minor int
	patch int
}

func (t semverTriple) after(other semverTriple) bool {
	if t.major != other.major {
		return t.major > other.major
	}
	if t.minor != other.minor {
		return t.minor > other.minor
	}
	return t.patch > other.patch
}

func parseSemver(text string) (semverTriple, bool) {
	parts := strings.Split(strings.TrimSpace(text), ".")
	if len(parts) != 3 {
		return semverTriple{}, false
	}
	major, majorErr := strconv.Atoi(parts[0])
	minor, minorErr := strconv.Atoi(parts[1])
	patch, patchErr := strconv.Atoi(parts[2])
	if majorErr != nil || minorErr != nil || patchErr != nil {
		return semverTriple{}, false
	}
	return semverTriple{major: major, minor: minor, patch: patch}, true
}

type changelogVersion struct {
	Version string `json:"version"`
	Heading string `json:"heading"`
	Body    string `json:"body"`

	number semverTriple
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
			lines = append(lines, "", reportIndent+strings.TrimPrefix(text, changelogSectionMark), "")
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

func changelogText(headline string, versions []changelogVersion, width int) string {
	body := strings.Builder{}
	body.WriteString(headline + "\n")
	for _, version := range versions {
		body.WriteString("\n" + version.Heading + "\n\n")
		body.WriteString(strings.Join(changelogBody(version.Body, width), "\n") + "\n")
	}
	return body.String()
}

func changelogHeadline(count int, seen string) string {
	head := "boji " + konst.Version + ", "
	switch count {
	case 0:
		return head + "nothing new since " + seen
	case 1:
		return head + "1 version you have not read"
	}
	return head + strconv.Itoa(count) + " versions you have not read"
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

func changelogWidth(out io.Writer) int {
	file, isFile := out.(*os.File)
	if !isFile {
		return konst.ProseWidthChars
	}
	width, _, err := term.GetSize(file.Fd())
	if err != nil || width <= 0 {
		return konst.ProseWidthChars
	}
	return width
}

func changelogVerb(args []string, out, errOut io.Writer) int {
	all, asJSON := false, false
	for _, arg := range args {
		switch arg {
		case changelogAllFlag:
			all = true
		case jsonFlag:
			asJSON = true
		default:
			_, _ = fmt.Fprintln(errOut, changelogFlags)
			return exitUsage
		}
	}
	versions := parseChangelog(changelog.Markdown)
	if asJSON {
		if err := writeJSON(out, versions); err != nil {
			_, _ = fmt.Fprintf(errOut, "boji changelog: %v\n", err)
			return exitVerdict
		}
		return exitOK
	}
	width := changelogWidth(out)
	if all {
		_, _ = fmt.Fprint(out, changelogText("boji "+konst.Version+", every version", versions, width))
		return exitOK
	}
	seen := changelogSeen()
	shown := changelogSince(versions, seen)
	_, _ = fmt.Fprint(out, changelogText(changelogHeadline(len(shown), seen), shown, width))
	if err := recordChangelogSeen(); err != nil {
		_, _ = fmt.Fprintf(errOut, "boji changelog: the version you read was not recorded: %v\n", err)
	}
	return exitOK
}
