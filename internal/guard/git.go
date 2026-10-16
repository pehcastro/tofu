package guard

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"

	"tofu/internal/sys"
)

const subjectLimit = 72

var (
	commentLine = sys.LazyRegexp(`^\s*#`)
	attribution = sys.LazyRegexp(`(?i)^\s*(co-authored-by|signed-off-by|assisted-by|generated-by)\s*:`)
	toolCredits = []func() *regexp.Regexp{
		sys.LazyRegexp(`(?i)\b(generated|written|authored|created|produced|assisted|helped|co-?authored|made)\b[^.]{0,40}\b(by|with|using)\b[^.]{0,20}\b(claude|anthropic|copilot|cursor|chatgpt|openai|an? (ai|agent|bot|llm|model))\b`),
		sys.LazyRegexp(`(?i)\b(claude|anthropic|copilot|cursor|chatgpt|openai)\b[^.]{0,20}\b(wrote|generated|authored|made|helped|assisted)\b`),
		sys.LazyRegexp(`(?i)\bwith\s+(claude|copilot|cursor|chatgpt)\s*$`),
	}
	conventional     = sys.LazyRegexp(`(?i)^(feat|fix|docs|refactor|test|chore|bench|hook)(\([a-z0-9._/-]+\))?!?: .+`)
	releaseSubject   = sys.LazyRegexp(`(?i)^chore\(release\): (\S+)$`)
	changelogSection = sys.LazyRegexp(`(?i)^### (\w+)`)
	versionHeading   = sys.LazyRegexp(`(?i)^## `)
	quotedName       = sys.LazyRegexp("`([A-Za-z_][A-Za-z0-9_.-]*)`")
	docsWaiver       = sys.LazyRegexp(`(?i)^Docs: none, (\S+\s+){2,}\S+`)
)

func CommitMsg(dir, file string) Answer {
	body, read := messageBody(file)
	if !read {
		return Answer{}
	}
	for _, line := range body {
		switch {
		case attribution().MatchString(line):
			return refuseCommit("no attribution trailers. No agent, bot or harness is ever a co-author of this project.")
		case anyMatch(toolCredits, line):
			return refuseCommit("a commit message credits a tool for the work: " + strings.TrimSpace(line))
		case strings.ContainsRune(line, 0x2014):
			return refuseCommit("no em dash, anywhere in this project.")
		case strings.Contains(line, `"`):
			return refuseCommit("no double quote in a commit message. The owner commits from PowerShell.")
		}
	}
	subject := ""
	if len(body) > 0 {
		subject = body[0]
	}
	switch {
	case blank(subject):
		return refuseCommit("the message is empty.")
	case !conventional().MatchString(subject):
		return refuseCommit("the subject is not a conventional commit: " + subject)
	case utf16Len(subject) > subjectLimit:
		return refuseCommit(fmt.Sprintf("the subject is %d characters, the limit is %d.", utf16Len(subject), subjectLimit))
	case strings.HasSuffix(subject, "."):
		return refuseCommit("the subject ends with a period.")
	}
	release := releaseSubject().FindStringSubmatch(subject)
	if release == nil {
		return Answer{}
	}
	if why := releaseRefusal(dir); why != "" {
		return refuse(1, why)
	}
	return releaseDocs(dir, release[1], body)
}

func ReleaseDocs(dir, file string) Answer {
	body, read := messageBody(file)
	if !read || len(body) == 0 {
		return Answer{}
	}
	release := releaseSubject().FindStringSubmatch(body[0])
	if release == nil {
		return Answer{}
	}
	return releaseDocs(dir, release[1], body)
}

func messageBody(file string) ([]string, bool) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, false
	}
	var body []string
	for _, line := range newline().Split(string(data), -1) {
		if !commentLine().MatchString(line) {
			body = append(body, line)
		}
	}
	return body, true
}

func refuseCommit(why string) Answer {
	return refuse(1, "commit refused: "+why+`

  type(scope): a plain sentence
  types: feat fix docs refactor test chore bench hook
  under 72 characters, present tense, no trailing period`)
}

func releaseDocs(dir, version string, message []string) Answer {
	changelog := gitLines(dir, "show", ":CHANGELOG.md")
	header := regexp.MustCompile(`(?i)^## ` + regexp.QuoteMeta(version) + `(\s|$)`)
	start := slices.IndexFunc(changelog, header.MatchString)
	if start < 0 {
		return refuse(1, "release refused: CHANGELOG.md has no entry headed "+version+".")
	}
	var names []string
	section, addsOrChanges := "", false
	for _, line := range changelog[start+1:] {
		if versionHeading().MatchString(line) {
			break
		}
		if heading := changelogSection().FindStringSubmatch(line); heading != nil {
			section = heading[1]
			addsOrChanges = addsOrChanges || announces(section)
			continue
		}
		if announces(section) {
			for _, name := range quotedName().FindAllStringSubmatch(line, -1) {
				names = append(names, name[1])
			}
		}
	}
	var missing []string
	if addsOrChanges && !slices.ContainsFunc(message, docsWaiver().MatchString) {
		previous := strings.TrimSpace(strings.Join(gitLines(dir, "log", "-1", "--format=%H", "--grep=^chore(release): ", "HEAD"), "\n"))
		changed, since := gitLines(dir, "ls-files", "--", "docs/"), "in this repository"
		if previous != "" {
			changed, since = gitLines(dir, "diff", "--cached", "--name-only", previous, "--", "docs/"), "since "+previous[:min(8, len(previous))]
		}
		if len(changed) == 0 {
			missing = append(missing, "no file under docs/ changed "+since+". Every Added and Changed line goes into docs/ before the release, or the body says Docs: none, <reason>.")
		}
	}
	slices.SortFunc(names, func(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) })
	for _, name := range slices.CompactFunc(names, strings.EqualFold) {
		if exec.Command("git", "-C", dir, "grep", "--cached", "-q", "-I", "-F", "-w", "-e", name, "--", "docs/").Run() != nil {
			missing = append(missing, "`"+name+"` is in the "+version+" entry and nowhere under docs/.")
		}
	}
	if len(missing) == 0 {
		return Answer{}
	}
	return refuse(1, "release refused: docs/ lags the changelog for "+version+".\n  "+strings.Join(missing, "\n  "))
}

func announces(section string) bool {
	return strings.EqualFold(section, "Added") || strings.EqualFold(section, "Changed")
}

func gitLines(dir string, args ...string) []string {
	out, _ := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	text := strings.TrimSuffix(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}
