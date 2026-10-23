package toolcard

import (
	"path"
	"slices"
	"strings"
	"unicode"
)

type rank int

const (
	rankNone rank = iota
	rankRan
	rankExplore
	rankGitRead
	rankGitStage
	rankCheck
	rankTofu
	rankBoardy
	rankGitWrite
)

func ofCommand(command string) Card {
	best, bestRank := Card{Family: Ran, Doing: "running", Verb: "ran", Tone: Ok, Fold: true}, rankNone
	dir, pushes := "", false
	for _, words := range segments(command) {
		if program(words[0]) == "cd" {
			dir = path.Join(dir, argument(words, 1))
		}
		if words = meaningful(words); words == nil {
			continue
		}
		card, at := ofSegment(words)
		if card.kind == kindCheck && program(words[0]) == "go" {
			card.Object = resolved(dir, card.Object)
		}
		pushes = pushes || card.kind == kindPush
		if at > bestRank {
			best, bestRank = card, at
		}
	}
	best.withPush = best.kind == kindCommit && pushes
	return best
}

func resolved(dir, pkg string) string {
	joined := path.Join(dir, pkg)
	if joined == "" || strings.HasPrefix(joined, ".") && joined != "..." || strings.HasPrefix(joined, "/") {
		return joined
	}
	return "./" + joined
}

func segments(command string) [][]string {
	var all [][]string
	var words []string
	var word strings.Builder
	quote, inWord := rune(0), false
	endWord := func() {
		if inWord {
			words, inWord = append(words, word.String()), false
			word.Reset()
		}
	}
	endSegment := func() {
		if endWord(); len(words) > 0 {
			all, words = append(all, words), nil
		}
	}
	runes := []rune(command)
	for at := 0; at < len(runes); at++ {
		switch r := runes[at]; {
		case quote != 0 && r == quote:
			quote = 0
		case quote != 0:
			word.WriteRune(r)
		case r == '"' || r == '\'' || r == '`':
			quote, inWord = r, true
		case r == ';' || r == '\n' || r == '|':
			endSegment()
		case r == '&' && at+1 < len(runes) && runes[at+1] == '&':
			endSegment()
			at++
		case unicode.IsSpace(r):
			endWord()
		default:
			word.WriteRune(r)
			inWord = true
		}
	}
	endSegment()
	return all
}

func meaningful(words []string) []string {
	wrapper := ""
	for ; len(words) > 0; words = words[1:] {
		word, named := words[0], program(words[0])
		if assignment(word) || wrapper != "" && strings.HasPrefix(word, "-") || wrapper == "rtk" && word == "proxy" {
			continue
		}
		switch named {
		case "rtk", "env", "sudo", "time", "nohup", "exec":
			wrapper = named
			continue
		case "cd", "pushd", "popd", "set", "export", "source", ".", "set-location":
			return nil
		case "bash", "sh", "zsh", "pwsh", "powershell", "cmd":
			for at, flag := range words[:len(words)-1] {
				switch strings.ToLower(flag) {
				case "-c", "-command", "/c":
					return firstMeaningful(words[at+1])
				}
			}
		}
		return words
	}
	return nil
}

func firstMeaningful(command string) []string {
	for _, words := range segments(command) {
		if words = meaningful(words); words != nil {
			return words
		}
	}
	return nil
}

func ofSegment(words []string) (Card, rank) {
	card := func(family Family, kind kind, doing, verb, object string) Card {
		return Card{Family: family, kind: kind, Doing: doing, Verb: verb, Object: object, Tone: Ok, Fold: true, words: words}
	}
	named, sub := program(words[0]), argument(words, 1)
	switch named {
	case "git":
		return ofGit(words, card)
	case "tofu":
		if sub == "boardy" {
			return ofBoardy(words[2:], card), rankBoardy
		}
		return card(Ran, kindRan, "running", "ran", spoken(named, sub)), rankTofu
	case "go", "cargo":
		switch sub {
		case "test", "vet", "build", "check", "clippy":
			return card(Tests, kindCheck, "running "+named+" "+sub, named+" "+sub, packageOf(words[2:])), rankCheck
		}
	case "cat", "head", "tail", "less", "type", "get-content":
		return card(Explored, kindRead, "reading", "read", base(lastOperand(words[1:]))), rankExplore
	case "grep", "rg", "findstr", "select-string":
		return card(Explored, kindSearch, "searching", "searched", ""), rankExplore
	case "ls", "dir", "tree", "find", "get-childitem":
		return card(Explored, kindList, "listing", "listed", base(lastOperand(words[1:]))), rankExplore
	}
	return card(Ran, kindRan, "running", "ran", spoken(named, sub)), rankRan
}

func ofGit(words []string, card func(Family, kind, string, string, string) Card) (Card, rank) {
	at := 1
	for at < len(words) && strings.HasPrefix(words[at], "-") {
		if words[at] == "-C" || words[at] == "-c" {
			at++
		}
		at++
	}
	sub, rest := argument(words, at), words[min(at+1, len(words)):]
	write := func(kind kind, doing, verb string, object string) (Card, rank) {
		written := card(Git, kind, doing, verb, object)
		written.failedVerb = "git " + sub
		return written, rankGitWrite
	}
	switch sub {
	case "status":
		return card(Explored, kindGitStatus, "checking", "checked", "git status"), rankGitRead
	case "diff":
		return card(Explored, kindGitDiff, "checking", "checked", "git diff"), rankGitRead
	case "log":
		return card(Explored, kindGitLog, "checking", "checked", "git log"), rankGitRead
	case "show", "blame":
		return card(Explored, kindRan, "checking", "checked", "git "+sub), rankGitRead
	case "add":
		return card(Git, kindGitAdd, "staging", "staged", staged(rest)), rankGitStage
	case "commit":
		return write(kindCommit, "committing", "committed", "")
	case "push":
		return write(kindPush, "pushing", "pushed", strings.Join(operands(rest), " "))
	case "pull":
		return write(kindPull, "pulling", "pulled", strings.Join(operands(rest), " "))
	case "rebase":
		return write(kindPull, "rebasing", "rebased onto", firstOperand(rest))
	case "merge":
		return write(kindPull, "merging", "merged", firstOperand(rest))
	case "switch", "checkout":
		return write(kindSwitch, "switching to", "switched to", firstOperand(rest))
	case "stash":
		return write(kindStash, "stashing", "stashed", firstOperand(rest))
	case "tag":
		return write(kindRan, "tagging", "tagged", firstOperand(rest))
	case "reset":
		return write(kindReset, "resetting", "reset to", firstOperand(rest))
	}
	return card(Ran, kindRan, "running", "ran", spoken("git", sub)), rankGitRead
}

func ofBoardy(words []string, card func(Family, kind, string, string, string) Card) Card {
	at := slices.IndexFunc(words, ticketID)
	id, next := argument(words, at), ""
	if at >= 0 {
		next = argument(words, at+1)
	}
	switch verb := argument(words, 0); verb {
	case "move":
		moved := card(Board, kindBoardMove, "moving", "moved", id)
		moved.Meta = movedTo(next)
		return moved
	case "new":
		opened := card(Board, kindBoardNew, "opening", "opened", "")
		opened.Meta = flagValue(words, "--title")
		return opened
	case "log":
		logged := card(Board, kindBoardLog, "logging on", "logged on", id)
		logged.Meta = "say"
		if slices.ContainsFunc(words, func(word string) bool { return strings.HasPrefix(word, "--evidence") }) {
			logged.Meta = "evidence"
		}
		return logged
	case "assign":
		assigned := card(Board, kindBoardAssign, "assigning", "assigned", id)
		assigned.Meta = "to " + next
		return assigned
	case "ask":
		return card(Board, kindBoardAsk, "asking on", "asked on", id)
	default:
		return card(Board, kindRan, "reading", "read", strings.TrimSpace(spoken("boardy", verb)+" "+id))
	}
}

func program(word string) string {
	named := strings.ToLower(base(word))
	return strings.TrimSuffix(named, ".exe")
}

func assignment(word string) bool {
	key, _, found := strings.Cut(word, "=")
	return found && key != "" && strings.IndexFunc(key, func(r rune) bool {
		return r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) < 0
}

func argument(words []string, at int) string {
	if at < 0 || at >= len(words) {
		return ""
	}
	return words[at]
}

func spoken(named, sub string) string {
	if plainWord(sub) {
		return named + " " + sub
	}
	return named
}

func plainWord(word string) bool {
	return word != "" && unicode.IsLetter(rune(word[0])) && strings.IndexFunc(word, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune("_.:-", r)
	}) < 0
}

func operands(words []string) []string {
	var found []string
	for _, word := range words {
		if !strings.HasPrefix(word, "-") && !strings.ContainsAny(word, "<>") {
			found = append(found, word)
		}
	}
	return found
}

func firstOperand(words []string) string {
	return argument(operands(words), 0)
}

func lastOperand(words []string) string {
	found := operands(words)
	for at := len(found) - 1; at >= 0; at-- {
		if strings.IndexFunc(found[at], func(r rune) bool { return !unicode.IsDigit(r) }) >= 0 {
			return found[at]
		}
	}
	return ""
}

func packageOf(words []string) string {
	for _, word := range operands(words) {
		if strings.HasPrefix(word, ".") || strings.Contains(word, "/") {
			return word
		}
	}
	return flagValue(words, "-p")
}

func staged(words []string) string {
	paths := operands(words)
	if slices.Contains(words, "-A") || slices.Contains(words, "--all") || slices.Contains(paths, ".") {
		return "all"
	}
	return counted(len(paths), "file", "files")
}

func flagValue(words []string, flag string) string {
	for at, word := range words {
		if word == flag {
			return argument(words, at+1)
		}
		if value, found := strings.CutPrefix(word, flag+"="); found {
			return value
		}
	}
	return ""
}
