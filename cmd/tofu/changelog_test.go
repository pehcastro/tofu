package main

import (
	"bytes"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"tofu/interface/cli"
	"tofu/internal/konst"
	"tofu/internal/widget"
	"tofu/library/changelog"
)

const rootChangelogPath = "../../CHANGELOG.md"

func headlineOf(text string) string {
	line, _, _ := strings.Cut(text, "\n")
	return line
}

func headingsIn(text string) []string {
	var found []string
	for i, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		if i == 0 || line == "" || strings.HasPrefix(line, " ") {
			continue
		}
		found = append(found, line)
	}
	return found
}

func currentHeading(t *testing.T) string {
	t.Helper()
	for _, version := range parseChangelog(changelog.Markdown) {
		if version.Version == konst.Version {
			return strings.Replace(version.Heading, " - ", " · ", 1)
		}
	}
	t.Fatalf("the changelog carries no entry for %s, which is the version this binary reports", konst.Version)
	return ""
}

func TestTheEmbeddedChangelogIsTheFileAtTheRootByteForByte(t *testing.T) {
	root, err := os.ReadFile(rootChangelogPath)
	if err != nil {
		t.Fatalf("read %s: %v", rootChangelogPath, err)
	}
	if changelog.Markdown != string(root) {
		t.Fatalf("the embedded changelog is %d bytes and %s is %d: copy it into library/changelog and rebuild",
			len(changelog.Markdown), rootChangelogPath, len(root))
	}
}

func TestEveryVersionInTheFileIsParsedAndCarriesItsOwnHeading(t *testing.T) {
	versions := parseChangelog(changelog.Markdown)
	headers := 0
	for _, line := range strings.Split(changelog.Markdown, "\n") {
		if changelogVersionLine.MatchString(line) {
			headers++
		}
	}
	if len(versions) != headers || headers == 0 {
		t.Fatalf("the file carries %d version headers and the parser found %d", headers, len(versions))
	}
	for _, version := range versions {
		if !strings.HasPrefix(version.Heading, version.Version) {
			t.Fatalf("version %s has heading %q", version.Version, version.Heading)
		}
	}
	if versions[0].Version != konst.Version {
		t.Fatalf("the newest entry is %s and the binary says %s", versions[0].Version, konst.Version)
	}
}

func TestAHeadingThatIsNotAVersionIsNotFiledUnderThePreviousVersion(t *testing.T) {
	versions := parseChangelog(changelog.Markdown)
	for _, version := range versions {
		for _, line := range strings.Split(version.Body, "\n") {
			if strings.HasPrefix(line, changelogHeadingMark) {
				t.Fatalf("version %s carries the heading %q in its body", version.Version, line)
			}
		}
	}
	if strings.Contains(versions[len(versions)-1].Body, "Kept by hand") {
		t.Fatal("the prose above the first version header was filed under a version")
	}

	written := "# Changelog\n\nprose nobody versioned\n\n## 0.2.0 - 2026-09-18\n\nthe record\n\n## How to read this file\n\nnewest first\n\n## 0.1.0 - 2026-09-18\n\nthe first one\n"
	parsed := parseChangelog(written)
	if len(parsed) != 2 {
		t.Fatalf("want 2 versions from the written file, got %d", len(parsed))
	}
	if strings.Contains(parsed[0].Body, "newest first") {
		t.Fatalf("0.2.0 swallowed the heading that is not a version:\n%s", parsed[0].Body)
	}
	if strings.Contains(parsed[0].Body, "prose nobody versioned") {
		t.Fatal("0.2.0 swallowed the prose above it")
	}
}

func TestChangelogOrdersByNumbersThenPrereleaseWithItsDigitsAsNumbers(t *testing.T) {
	ascending := []string{
		"0.3.9", "0.3.10", "0.4.19",
		"0.5.0-rc", "0.5.0-rc-fix1", "0.5.0-rc-fix2", "0.5.0-rc-fix9", "0.5.0-rc-fix10", "0.5.0",
		"0.5.1-rc-fix99999999999999999999", "0.5.1-rc-fix100000000000000000000", "0.5.1",
	}
	for i, older := range ascending {
		low, ok := parseSemver(older)
		if !ok {
			t.Fatalf("%s does not parse", older)
		}
		if low.after(low) {
			t.Fatalf("%s reads as newer than itself", older)
		}
		for _, newer := range ascending[i+1:] {
			high, _ := parseSemver(newer)
			if !high.after(low) || low.after(high) {
				t.Fatalf("%s is not ordered after %s", newer, older)
			}
		}
	}

	written := "## 0.5.0-rc-fix2 - 2026-09-27\n\nsecond\n\n## 0.5.0-rc-fix1 - 2026-09-26\n\nfirst\n\n## 0.4.19 - 2026-09-25\n\nold\n"
	parsed := parseChangelog(written)
	var names []string
	for _, version := range parsed {
		names = append(names, version.Version)
	}
	if want := []string{"0.5.0-rc-fix2", "0.5.0-rc-fix1", "0.4.19"}; !slices.Equal(names, want) {
		t.Fatalf("the headings parse as %v, want %v", names, want)
	}
	for seen, want := range map[string][]string{
		"0.4.19":            {"0.5.0-rc-fix2", "0.5.0-rc-fix1"},
		"0.5.0-rc-fix1":     {"0.5.0-rc-fix2"},
		"0.5.0-rc-fix1+dev": {"0.5.0-rc-fix2"},
		"0.5.0-rc-fix2":     nil,
	} {
		var shown []string
		for _, version := range changelogSince(parsed, seen) {
			shown = append(shown, version.Version)
		}
		if !slices.Equal(shown, want) {
			t.Fatalf("since %s shows %v, want %v", seen, shown, want)
		}
	}
}

func TestTheJSONFormCarriesOneObjectPerVersion(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"changelog", "--json"}, strings.NewReader(""), &out, &errOut); code != exitOK {
		t.Fatalf("tofu changelog --json exited %d: %s", code, errOut.String())
	}
	var envelope struct {
		Data struct {
			Versions []changelogVersion `json:"versions"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("the json does not parse: %v\n%s", err, out.String())
	}
	versions := envelope.Data.Versions
	if len(versions) != len(parseChangelog(changelog.Markdown)) {
		t.Fatalf("the json carries %d objects and the file has %d versions", len(versions), len(parseChangelog(changelog.Markdown)))
	}
	for _, version := range versions {
		if version.Version == "" || version.Heading == "" || version.Body == "" {
			t.Fatalf("an object is missing a field: %+v", version)
		}
	}
}

func TestTheRenderedFormWrapsToTheWidthAndCarriesNoRawMarkdown(t *testing.T) {
	const width = konst.ProseWidthChars
	page := cli.Detect(&bytes.Buffer{}, nil)
	for _, line := range changelogLines(page, true, "", parseChangelog(changelog.Markdown)) {
		if widget.Cells(line) > width {
			t.Fatalf("a line is %d cells wide at width %d:\n%s", widget.Cells(line), width, line)
		}
		for _, syntax := range []string{"**", "`", "](", "### ", "## "} {
			if strings.Contains(line, syntax) {
				t.Fatalf("the rendered line carries raw markdown %q:\n%s", syntax, line)
			}
		}
	}
}

func TestMarkdownIsUnwrappedRatherThanDropped(t *testing.T) {
	written := "## 0.1.0 - 2026-09-18\n\n### Added\n\n- **breaking:** `tofu why` reads the [ledger](../internal/judge/ledger)\n"
	body := strings.Join(changelogBody(parseChangelog(written)[0].Body, konst.ProseWidthChars), "\n")
	for _, kept := range []string{"Added", "breaking:", "tofu why", "ledger"} {
		if !strings.Contains(body, kept) {
			t.Fatalf("%q was dropped rather than unwrapped:\n%s", kept, body)
		}
	}
	if strings.Contains(body, "internal/judge/ledger") {
		t.Fatalf("the link target was printed rather than the text:\n%s", body)
	}
}

func TestChangelogShowsTheCurrentVersionOnceAndThenSaysNothingIsNew(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	var first, errOut bytes.Buffer
	if code := run([]string{"changelog"}, strings.NewReader(""), &first, &errOut); code != exitOK {
		t.Fatalf("the first run exited %d: %s", code, errOut.String())
	}
	if !strings.HasSuffix(headlineOf(first.String()), "● 1 unread") {
		t.Fatalf("the first run leads with %q, want 1 unread", headlineOf(first.String()))
	}
	if want := []string{currentHeading(t)}; !slices.Equal(headingsIn(first.String()), want) {
		t.Fatalf("the first run printed the entries %v, want %v", headingsIn(first.String()), want)
	}
	if errOut.Len() != 0 {
		t.Fatalf("the first run wrote to stderr: %s", errOut.String())
	}

	var second bytes.Buffer
	if code := run([]string{"changelog"}, strings.NewReader(""), &second, &errOut); code != exitOK {
		t.Fatalf("the second run exited %d: %s", code, errOut.String())
	}
	if !strings.HasSuffix(headlineOf(second.String()), "✓ nothing new since "+konst.Version) {
		t.Fatalf("the second run leads with %q, want nothing new since %s", headlineOf(second.String()), konst.Version)
	}
	if entries := headingsIn(second.String()); entries != nil {
		t.Fatalf("the second run repeated the entries %v", entries)
	}
}

func TestAllPrintsEveryVersionAndDoesNotRecordAnything(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	var out, errOut bytes.Buffer
	if code := run([]string{"changelog", "--all"}, strings.NewReader(""), &out, &errOut); code != exitOK {
		t.Fatalf("tofu changelog --all exited %d: %s", code, errOut.String())
	}
	var want []string
	for _, version := range parseChangelog(changelog.Markdown) {
		want = append(want, strings.Replace(version.Heading, " - ", " · ", 1))
	}
	if !slices.Equal(headingsIn(out.String()), want) {
		t.Fatalf("--all printed the entries %v, want %v", headingsIn(out.String()), want)
	}
	if changelogSeen() != "" {
		t.Fatalf("--all recorded %q as read", changelogSeen())
	}
}

func TestDownAPipeTheChangelogWrapsToTheProseWidth(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	var out, errOut bytes.Buffer
	if code := run([]string{"changelog", "--all"}, strings.NewReader(""), &out, &errOut); code != exitOK {
		t.Fatalf("tofu changelog --all exited %d: %s", code, errOut.String())
	}
	widest := 0
	for _, line := range strings.Split(out.String(), "\n") {
		widest = max(widest, widget.Cells(line))
	}
	if widest > konst.ProseWidthChars {
		t.Fatalf("the widest line is %d cells, over the prose width %d", widest, konst.ProseWidthChars)
	}
}

func TestAnUnknownFlagIsRefused(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"changelog", "--every"}, strings.NewReader(""), &out, &errOut); code != exitUsage {
		t.Fatalf("an unknown flag exited %d, want %d", code, exitUsage)
	}
	if !strings.Contains(errOut.String(), changelogUsage) {
		t.Fatalf("the refusal does not name the flags: %s", errOut.String())
	}
}
