package guard

import (
	"slices"
	"strings"
	"unicode"

	"tofu/internal/boardy"
)

const (
	FlowsStamps    = boardy.FlowsStamps
	TreewideStamps = boardy.TreewideStamps
)

func StampDir(repo string) string { return boardy.StampDir(repo) }

func StampPath(repo, kind, tree string) string { return boardy.StampPath(repo, kind, tree) }

func HeadTree(repo string) (string, []string, error) { return boardy.HeadTree(repo) }

func flowsRefusal(env Env, command string) string {
	if !strings.Contains(command, "boardy") {
		return ""
	}
	source := ticketsAt(env)
	for _, id := range doneMoves(command) {
		id = strings.ToUpper(id)
		moved, _, err := source.find(id)
		if err != nil {
			return "The flows gate could not read " + id + ": " + err.Error()
		}
		if why := boardy.DoneRefusal(env.Root, moved.Owns); why != "" {
			return boardy.RefusedError{Ticket: id, Why: why}.Error()
		}
	}
	return ""
}

func releaseRefusal(dir string) string {
	tree, run, err := boardy.Unstamped(dir, FlowsStamps, TreewideStamps)
	if err != nil {
		return "release refused: HEAD's tree could not be read, so no stamp can be checked: " + err.Error()
	}
	var why []string
	if outside := slices.DeleteFunc(gitLines(dir, "diff", "--cached", "--name-only", "HEAD"), releaseFile); len(outside) > 0 {
		why = append(why, "these staged files are not release files, so the commit is not the stamped HEAD plus a version bump:\n  "+strings.Join(outside, "\n  "))
	}
	if len(run) > 0 {
		why = append(why, "HEAD's tree "+tree+" has no passing stamp; in a clean clone of HEAD, with TOFU_STAMP_DIR="+StampDir(dir)+", run:\n  "+strings.Join(run, "\n  "))
	}
	if len(why) == 0 {
		return ""
	}
	return "release refused: " + strings.Join(why, "\nand ") + "\nA release is HEAD, stamped, plus only internal/konst/konst.go, CHANGELOG.md, library/changelog/, docs/ and library/docs/."
}

func releaseFile(path string) bool {
	return path == "internal/konst/konst.go" || path == "CHANGELOG.md" ||
		slices.ContainsFunc([]string{"docs/", "library/docs/", "library/changelog/"}, func(dir string) bool { return strings.HasPrefix(path, dir) })
}

func doneMoves(command string) []string {
	var ids []string
	for _, words := range shellWords(command) {
		at := slices.Index(words, "boardy")
		if at < 0 || at+1 == len(words) || words[at+1] != "move" {
			continue
		}
		var args []string
		for i := at + 2; i < len(words); i++ {
			name, isFlag := strings.CutPrefix(words[i], "--")
			switch {
			case !isFlag:
				args = append(args, words[i])
			case name != "all" && name != "answers" && name != "thread":
				i++
			}
		}
		if len(args) == 2 && args[1] == string(boardy.Done) {
			ids = append(ids, args[0])
		}
	}
	return ids
}

func shellWords(command string) [][]string {
	var commands [][]string
	var words []string
	var word strings.Builder
	var quote rune
	inWord := false
	flush := func() {
		if inWord {
			words, inWord = append(words, word.String()), false
			word.Reset()
		}
	}
	for _, r := range command {
		switch {
		case quote != 0 && r == quote:
			quote = 0
		case quote != 0:
			word.WriteRune(r)
		case r == '"' || r == '\'':
			quote, inWord = r, true
		case strings.ContainsRune(";&|\n()", r):
			flush()
			commands, words = append(commands, words), nil
		case unicode.IsSpace(r):
			flush()
		default:
			word.WriteRune(r)
			inWord = true
		}
	}
	flush()
	return append(commands, words)
}
