package toolcard

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

const (
	cutMarker        = "...(truncated)..."
	toolRefOpen      = "[tool#"
	artifactLead     = "artifact "
	artifactHolds    = " holds "
	searchFoundNone  = 1
	failedTestPrefix = "--- FAIL: "
	pushedNote       = "pushed"
	rtkDone          = "ok "
	rtkUpToDate      = "ok (up-to-date)"
	rtkPulled        = "ok %d files +%d -%d"
)

func readable(content string) ([]string, bool) {
	lines := strings.Split(withoutEscapes(strings.ReplaceAll(content, "\r", "")), "\n")
	if first := argument(lines, 0); strings.HasPrefix(first, toolRefOpen) && strings.HasSuffix(first, "]") {
		lines = lines[1:]
	}
	if first := argument(lines, 0); strings.HasPrefix(first, artifactLead) && strings.Contains(first, artifactHolds) {
		lines = lines[1:]
	}
	trimmed := false
	for at, line := range lines {
		if line == cutMarker {
			headWhole, tailWhole := lines[:max(at-1, 0)], lines[min(at+2, len(lines)):]
			lines, trimmed = append(slices.Clip(headWhole), tailWhole...), true
			break
		}
	}
	kept := lines[:0]
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			kept = append(kept, line)
		}
	}
	return kept, trimmed
}

func withoutEscapes(text string) string {
	if !strings.Contains(text, "\x1b[") {
		return text
	}
	var plain strings.Builder
	for at := 0; at < len(text); at++ {
		if text[at] != '\x1b' || at+1 >= len(text) || text[at+1] != '[' {
			plain.WriteByte(text[at])
			continue
		}
		for at += 2; at < len(text) && !unicode.IsLetter(rune(text[at])); at++ {
		}
	}
	return plain.String()
}

func anyContains(lines []string, part string) bool {
	for _, line := range lines {
		if strings.Contains(line, part) {
			return true
		}
	}
	return false
}

func firstWord(text string) string {
	return argument(strings.Fields(text), 0)
}

func counted(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

func delta(added, removed int) string {
	var texts []string
	for _, piece := range counts(added, removed) {
		texts = append(texts, piece.Text)
	}
	return strings.Join(texts, " ")
}

func number(word string) (int, bool) {
	n, err := strconv.Atoi(word)
	return n, err == nil
}

func statLine(lines []string) (string, bool) {
	for _, line := range lines {
		words := strings.Fields(strings.NewReplacer(",", " ", "(+)", "", "(-)", "").Replace(line))
		if len(words) < 3 || !strings.HasPrefix(argument(words, 1), "file") || argument(words, 2) != "changed" {
			continue
		}
		files, isNumber := number(words[0])
		if !isNumber {
			continue
		}
		added, removed := 0, 0
		for at := 3; at+1 < len(words); at += 2 {
			n, _ := number(words[at])
			switch {
			case strings.HasPrefix(words[at+1], "insertion"):
				added = n
			case strings.HasPrefix(words[at+1], "deletion"):
				removed = n
			}
		}
		return strings.TrimSpace(counted(files, "file", "files") + " " + delta(added, removed)), true
	}
	return "", false
}

func statusCount(lines []string) string {
	if anyContains(lines, "working tree clean") {
		return "clean"
	}
	changed := 0
	for _, line := range lines {
		short := len(line) > 3 && line[2] == ' ' && strings.ContainsRune(" MADRCU?!", rune(line[0])) && strings.ContainsRune(" MADRCU?!", rune(line[1]))
		if strings.HasPrefix(line, "\t") || short && !strings.HasPrefix(line, "##") {
			changed++
		}
	}
	return counted(changed, "changed", "changed")
}

func diffCount(lines []string, trimmed bool) string {
	if stat, found := statLine(lines); found {
		return stat
	}
	if trimmed {
		return ""
	}
	changes := diffChanges(lines)
	added, removed := 0, 0
	for _, change := range changes {
		added, removed = added+change.Added, removed+change.Removed
	}
	if len(changes) == 0 {
		return "no changes"
	}
	return counted(len(changes), "file", "files") + " " + delta(added, removed)
}

func diffChanges(lines []string) []Change {
	var changes []Change
	inHunk := false
	for at, line := range lines {
		switch {
		case strings.HasPrefix(line, "--- ") && strings.HasPrefix(argument(lines, at+1), "+++ "):
			inHunk = false
		case strings.HasPrefix(line, "+++ ") && at > 0 && strings.HasPrefix(lines[at-1], "--- "):
			file := strings.TrimPrefix(strings.TrimPrefix(line, "+++ "), "b/")
			changes = append(changes, Change{Path: file})
		case strings.HasPrefix(line, "@@"):
			inHunk = len(changes) > 0
		case !inHunk:
		case strings.HasPrefix(line, "+"):
			changes[len(changes)-1].Added++
		case strings.HasPrefix(line, "-"):
			changes[len(changes)-1].Removed++
		case !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, `\`):
			inHunk = false
		}
	}
	return changes
}

func logCount(words, lines []string, trimmed bool) string {
	if trimmed {
		return ""
	}
	commits, oneline := 0, slices.Contains(words, "--oneline")
	for _, line := range lines {
		if oneline || strings.HasPrefix(line, "commit ") {
			commits++
		}
	}
	return counted(commits, "commit", "commits")
}

func committed(c Card, lines []string) Card {
	if anyContains(lines, "nothing to commit") || anyContains(lines, "nothing added to commit") {
		c.Verb, c.Tone = "nothing to commit", Warn
		return c
	}
	for _, line := range lines {
		if sha, found := strings.CutPrefix(line, rtkDone); found && hex(sha) {
			subject := commitSubject(c.words)
			c.Object, c.Meta, c.subject = sha, subject, subject
			break
		}
		inside, subject, found := strings.Cut(strings.TrimPrefix(line, "["), "] ")
		if !found || !strings.HasPrefix(line, "[") {
			continue
		}
		fields := strings.Fields(inside)
		if sha := argument(fields, len(fields)-1); hex(sha) {
			c.Object, c.Meta, c.subject = sha, subject, subject
		}
		break
	}
	if stat, found := statLine(lines); found && c.Object != "" {
		c.Meta += "  " + stat
	}
	if !c.withPush || c.Object == "" {
		return c
	}
	push := pushed(Card{Tone: Ok}, lines)
	switch {
	case push.Tone == Fail:
		c.Tone, c.pushNote = Fail, "push refused "+push.Meta
	case c.Tone == Fail:
		c.pushNote = "push failed"
	default:
		c.pushNote = pushedNote
	}
	c.failedVerb, c.Meta = "", c.Meta+", "+c.pushNote
	return c
}

func hex(word string) bool {
	return word != "" && strings.Trim(word, "0123456789abcdef") == ""
}

func commitSubject(words []string) string {
	message := ""
	for at, word := range words {
		if word == "--message" || strings.HasPrefix(word, "-") && !strings.HasPrefix(word, "--") && strings.HasSuffix(word, "m") {
			message = argument(words, at+1)
			break
		}
		if given, found := strings.CutPrefix(word, "--message="); found {
			message = given
			break
		}
	}
	subject, _, _ := strings.Cut(message, "\n")
	return subject
}

func pushed(c Card, lines []string) Card {
	for _, line := range lines {
		if strings.Contains(line, "[rejected]") || strings.Contains(line, "[remote rejected]") {
			c.Tone, c.Meta = Fail, "rejected"
			if _, reason, found := strings.Cut(line, "("); found {
				c.Meta = "(" + reason
			}
			return c
		}
	}
	switch {
	case anyContains(lines, "Everything up-to-date"):
		c.Meta = "up to date"
	case anyContains(lines, "[new branch]"):
		c.Meta = "new branch"
	}
	if c.Object == "" {
		for _, line := range lines {
			if _, into, found := strings.Cut(line, " -> "); found {
				c.Object = firstWord(into)
				break
			}
		}
	}
	return c
}

func pulled(c Card, lines []string) Card {
	conflicts := 0
	for _, line := range lines {
		if strings.HasPrefix(line, "CONFLICT") {
			conflicts++
		}
	}
	var files, added, removed int
	_, notRtk := fmt.Sscanf(argument(lines, len(lines)-1), rtkPulled, &files, &added, &removed)
	switch {
	case conflicts > 0:
		c.Tone, c.Meta = Fail, counted(conflicts, "conflict", "conflicts")
	case anyContains(lines, "up to date") || slices.Contains(lines, rtkUpToDate):
		c.Meta = "up to date"
	case anyContains(lines, "Fast-forward"):
		c.Meta = "fast-forward"
	case notRtk == nil:
		c.Meta = strings.TrimSpace(counted(files, "file", "files") + " " + delta(added, removed))
	}
	return c
}

func switched(c Card, lines []string) Card {
	leads := []struct{ lead, meta string }{
		{"Switched to a new branch '", "new branch"},
		{"Switched to branch '", ""},
		{"Already on '", "already"},
	}
	for _, line := range lines {
		for _, said := range leads {
			if rest, found := strings.CutPrefix(line, said.lead); found {
				c.Object, _, _ = strings.Cut(rest, "'")
				c.Meta = said.meta
				return c
			}
		}
	}
	return c
}

func stashed(c Card, lines []string) Card {
	switch {
	case anyContains(lines, "No local changes to save"):
		c.Meta = "nothing to stash"
	case anyContains(lines, "CONFLICT"):
		c.Tone, c.Meta = Fail, "conflict"
	}
	return c
}

func ticketID(word string) bool {
	board, digits, found := strings.Cut(word, "-")
	if !found || board == "" || !unicode.IsUpper(rune(board[0])) || strings.ContainsRune(digits, '-') {
		return false
	}
	_, isNumber := number(digits)
	return isNumber && strings.IndexFunc(board, func(r rune) bool { return !unicode.IsUpper(r) && !unicode.IsDigit(r) }) < 0
}

func receipted(c Card, lines []string) Card {
	if c.Tone == Fail {
		return c
	}
	for _, line := range lines {
		words := strings.Fields(line)
		for at, word := range words {
			id := strings.TrimRight(word, ",.;:")
			if !ticketID(id) {
				continue
			}
			c.Object = id
			rest := words[at+1:]
			switch {
			case c.kind == kindBoardMove && argument(rest, 0) == "is" && argument(rest, 1) == "already":
				c.Meta = "already " + argument(rest, 2)
			case c.kind == kindBoardMove && argument(rest, 0) == "is":
				c.Meta = movedTo(argument(rest, 1))
			case c.kind == kindBoardAssign && argument(rest, 1) == "assigned":
				c.Meta = "to " + argument(rest, 3)
			}
			return c
		}
	}
	return c
}

func checked(c Card, lines []string) Card {
	var names []string
	failed := c.Tone == Fail
	for _, line := range lines {
		text := strings.TrimSpace(line)
		name := ""
		switch {
		case strings.HasPrefix(text, failedTestPrefix):
			name, failed = firstWord(strings.TrimPrefix(text, failedTestPrefix)), true
		case strings.HasPrefix(text, "test ") && strings.HasSuffix(text, " ... FAILED"):
			name, failed = strings.TrimSuffix(strings.TrimPrefix(text, "test "), " ... FAILED"), true
		case text == "FAIL", strings.HasPrefix(text, "FAIL\t"), strings.HasPrefix(text, "FAIL "),
			strings.HasPrefix(text, "test result: FAILED"), strings.HasPrefix(text, "panic: "):
			failed = true
		}
		if name != "" && !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	if !failed {
		c.Tone, c.Meta = Ok, "pass"
		return c
	}
	c.Tone, c.Meta = Fail, "failed"
	if len(names) > 0 {
		c.Meta += " " + names[0]
		if len(names) > 1 {
			c.Meta += " +" + strconv.Itoa(len(names)-1)
		}
	} else if at := located(lines); at != "" {
		c.Meta += " at " + at
	}
	return c
}

func located(lines []string) string {
	for _, line := range lines {
		for _, field := range strings.Fields(line) {
			parts := strings.Split(field, ":")
			for at := 1; at < len(parts); at++ {
				file := base(strings.Join(parts[:at], ":"))
				if _, isNumber := number(parts[at]); isNumber && strings.Contains(file, ".") {
					return file + ":" + parts[at]
				}
			}
		}
	}
	return ""
}

func edited(c Card, lines []string, trimmed bool) Card {
	joined := strings.Join(lines, "\n")
	switch {
	case c.Tone == Fail:
		return c
	case strings.Contains(joined, "already held that text"):
		c.Meta = "unchanged"
		return c
	case strings.HasPrefix(joined, "created "):
		c.Verb = "created"
		if _, size, found := strings.Cut(joined, ": "); found {
			n, _ := number(firstWord(size))
			c.Changes[0].Added = n
		}
	case strings.HasPrefix(joined, "appended "):
		c.Verb = "appended to"
		n, _ := number(firstWord(strings.TrimPrefix(joined, "appended ")))
		c.Changes[0].Added = n
	case !trimmed:
		if changes := diffChanges(lines); len(changes) > 0 {
			c.Changes = changes
		}
	}
	added, removed := 0, 0
	for _, change := range c.Changes {
		added, removed = added+change.Added, removed+change.Removed
	}
	c.Object, c.Meta = changedNames(c.Changes), delta(added, removed)
	return c
}

func remembered(c Card, text string) Card {
	switch {
	case strings.Contains(text, " saved to memory"):
		c.Verb, c.Object = "saved", "to memory"
	case strings.HasPrefix(text, "refused"):
		c.Verb, c.Tone = "refused", Warn
	case strings.Contains(text, "nothing was kept"):
		c.Verb, c.Tone = "declined", Warn
	}
	return c
}

func answered(c Card, text string) Card {
	if strings.HasPrefix(text, "asked; the answer comes") {
		c.Meta = "waiting"
		return c
	}
	var reply struct {
		Outcome string `json:"outcome"`
	}
	if json.Unmarshal([]byte(text), &reply) != nil {
		return c
	}
	switch reply.Outcome {
	case "submitted":
		c.Meta = "answered"
	case "taken_in_auto":
		c.Meta = "took the recommended"
	case "cancelled", "timed_out", "undelivered":
		c.Meta, c.Tone = "unanswered", Warn
	}
	return c
}

func orchestratorAnswered(c Card, text string) Card {
	switch {
	case strings.HasPrefix(text, "the orchestrator answers: "):
		c.Meta = "answered"
	case strings.HasPrefix(text, "the orchestrator did not answer"):
		c.Meta, c.Tone = "assumed", Warn
	}
	return c
}
