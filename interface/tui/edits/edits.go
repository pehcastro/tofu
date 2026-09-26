package edits

import (
	"cmp"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"

	"tofu/interface/tui/look"
)

const (
	Self        = "tofu"
	tabStop     = "    "
	hunkMark    = "@@"
	addedMark   = "+"
	removedMark = "-"
	contextMark = " "
	fromHeader  = "--- "
	intoHeader  = "+++ "
	oscStart    = "\x1b]8;;"
	oscEnd      = "\x1b\\"
)

type Op int

const (
	OpModified Op = iota
	OpAdded
	OpDeleted
)

func (o Op) mark() (word, sigil string, colour look.Color) {
	switch o {
	case OpAdded:
		return "added", addedMark, look.Mint
	case OpDeleted:
		return "deleted", removedMark, look.Red
	}
	return "modified", "~", look.Blue
}

type Edit struct {
	Agent   string
	Path    string
	ID      string
	When    time.Time
	op      Op
	lines   []string
	numbers []int
	added   int
	removed int
}

func Changed(agent, path, diff, created, id string, when time.Time) (Edit, bool) {
	edit := Edit{Agent: cmp.Or(agent, Self), Path: path, ID: id, When: when}
	switch {
	case diff != "":
		for _, header := range []string{fromHeader, intoHeader} {
			if strings.HasPrefix(diff, header) {
				_, diff, _ = strings.Cut(diff, "\n")
			}
		}
		edit.lines = strings.Split(expand(diff), "\n")
		edit.numbers = make([]int, len(edit.lines))
		old, next := 0, 0
		for index, line := range edit.lines {
			switch {
			case strings.HasPrefix(line, hunkMark):
				if fields := strings.Fields(line); len(fields) > 2 {
					old, next = hunkStart(fields[1]), hunkStart(fields[2])
				}
			case strings.HasPrefix(line, addedMark):
				edit.numbers[index], next, edit.added = next, next+1, edit.added+1
			case strings.HasPrefix(line, removedMark):
				edit.numbers[index], old, edit.removed = old, old+1, edit.removed+1
			case strings.HasPrefix(line, contextMark):
				edit.numbers[index], old, next = next, old+1, next+1
			}
		}
	case created != "":
		edit.op = OpAdded
		edit.lines = strings.Split(expand(created), "\n")
		edit.numbers = make([]int, len(edit.lines))
		for index, line := range edit.lines {
			edit.lines[index], edit.numbers[index] = addedMark+line, index+1
		}
		edit.added = len(edit.lines)
	default:
		return Edit{}, false
	}
	return edit, true
}

func (e Edit) within(contextLines int) Edit {
	if contextLines <= 0 {
		return e
	}
	near, far := make([]int, len(e.lines)), len(e.lines)+contextLines
	walk := func(at, distance int) int {
		switch line := e.lines[at]; {
		case strings.HasPrefix(line, addedMark), strings.HasPrefix(line, removedMark):
			return 0
		case strings.HasPrefix(line, contextMark):
			return distance + 1
		case strings.HasPrefix(line, hunkMark):
			return far
		}
		return distance
	}
	for at, distance := 0, far; at < len(e.lines); at++ {
		distance = walk(at, distance)
		near[at] = distance
	}
	for at, distance := len(e.lines)-1, far; at >= 0; at-- {
		distance = walk(at, distance)
		near[at] = min(near[at], distance)
	}
	trimmed := e
	trimmed.lines, trimmed.numbers = nil, nil
	for at, line := range e.lines {
		if strings.HasPrefix(line, contextMark) && near[at] > contextLines {
			continue
		}
		trimmed.lines, trimmed.numbers = append(trimmed.lines, line), append(trimmed.numbers, e.numbers[at])
	}
	return trimmed
}

func hunkStart(field string) int {
	start, _, _ := strings.Cut(strings.TrimLeft(field, "+-"), ",")
	number, _ := strconv.Atoi(start)
	return number
}

func expand(text string) string {
	return strings.ReplaceAll(strings.TrimRight(text, "\n"), "\t", tabStop)
}

func (m Model) lexer(path string) chroma.Lexer {
	extension := cmp.Or(filepath.Ext(path), filepath.Base(path))
	if m.cache != nil {
		if found, known := m.cache.lexers[extension]; known {
			return found
		}
	}
	found := cmp.Or(lexers.Match(filepath.Base(path)), lexers.Fallback)
	if m.cache != nil {
		if m.cache.lexers == nil {
			m.cache.lexers = map[string]chroma.Lexer{}
		}
		m.cache.lexers[extension] = found
	}
	return found
}

func (e Edit) Op() Op          { return e.op }
func (e Edit) Added() int      { return e.added }
func (e Edit) Removed() int    { return e.removed }
func (e Edit) Lines() []string { return e.lines }

func (e Edit) Tally() string {
	return addedMark + strconv.Itoa(e.added) + " " + removedMark + strconv.Itoa(e.removed)
}

func (e Edit) meta() string { return deltas(e.added, e.removed) + look.Faint("  ·  @"+e.Path) }

func deltas(added, removed int) string {
	return look.Accent(look.SignedLines(int64(added))) + "  " + look.Style(look.Red).Render(look.SignedLines(-int64(removed)))
}

func hyperlink(root, path string, plain bool) string {
	if plain {
		return path
	}
	target := path
	if root != "" && !filepath.IsAbs(path) {
		target = filepath.ToSlash(filepath.Join(root, path))
	}
	return oscStart + "file://" + target + oscEnd + path + oscStart + oscEnd
}
