package shell

import (
	"bytes"
	"io"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

const (
	consoleColumns = 1024
	consoleRows    = 50
)

type screenText struct {
	out    io.Writer
	held   []byte
	column int
}

func (s *screenText) Write(p []byte) (int, error) {
	data := slices.Concat(s.held, p)
	var text bytes.Buffer
	at := 0
	for at < len(data) {
		size := s.step(data[at:], &text)
		if size == 0 {
			break
		}
		at += size
	}
	s.held = slices.Clone(data[at:])
	if _, err := s.out.Write(text.Bytes()); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (s *screenText) step(rest []byte, text *bytes.Buffer) int {
	switch rest[0] {
	case '\x1b':
		return s.escape(rest, text)
	case '\a':
		return 1
	case '\n':
		text.WriteByte('\n')
		s.column = 0
		return 1
	case '\r':
		if len(rest) < 2 {
			return 0
		}
		wrapped := s.column == consoleColumns
		s.column = 0
		if rest[1] != '\n' {
			text.WriteByte('\r')
			return 1
		}
		if !wrapped {
			text.WriteByte('\n')
		}
		return 2
	}
	if !utf8.FullRune(rest) {
		return 0
	}
	_, size := utf8.DecodeRune(rest)
	text.Write(rest[:size])
	s.column += max(1, ansi.StringWidth(string(rest[:size])))
	return size
}

func (s *screenText) escape(rest []byte, text *bytes.Buffer) int {
	if len(rest) < 2 {
		return 0
	}
	switch rest[1] {
	case '[':
		end := bytes.IndexFunc(rest[2:], func(r rune) bool { return r >= '@' && r <= '~' })
		if end < 0 {
			return 0
		}
		end += 2
		if rest[end] == 'C' {
			forward, err := strconv.Atoi(string(rest[2:end]))
			if err != nil {
				forward = 1
			}
			text.WriteString(strings.Repeat(" ", forward))
			s.column += forward
		}
		return end + 1
	case ']':
		bell, terminator := bytes.IndexByte(rest, '\a'), bytes.Index(rest, []byte("\x1b\\"))
		switch {
		case bell >= 0 && (terminator < 0 || bell < terminator):
			return bell + 1
		case terminator >= 0:
			return terminator + 2
		}
		return 0
	}
	return 2
}

func pipelines(command string) [][][]string {
	shielded := regexp.MustCompile(`"[^"]*"|'[^']*'`).ReplaceAllStringFunc(command, func(quoted string) string {
		return strings.NewReplacer("|", " ", ";", " ", "&", " ", ">", " ").Replace(quoted)
	})
	var all [][][]string
	for _, run := range regexp.MustCompile(`&&|\|\||;|\n`).Split(shielded, -1) {
		var stages [][]string
		for _, stage := range strings.Split(run, "|") {
			stages = append(stages, strings.Fields(stage))
		}
		all = append(all, stages)
	}
	return all
}

func program(fields []string) string {
	return strings.TrimSuffix(filepath.Base(fields[0]), ".exe")
}

func blocksOnAPipe(fields []string) bool {
	lineFlags := map[string][]string{"grep": {"--line-buffered"}, "sed": {"-u", "--unbuffered"}, "awk": nil, "cut": nil, "tr": nil, "uniq": nil}
	flags, filter := lineFlags[program(fields)]
	return filter && !slices.ContainsFunc(fields, func(field string) bool { return slices.Contains(flags, field) })
}

func endsInFilter(command string) bool {
	return slices.ContainsFunc(pipelines(command), func(stages [][]string) bool {
		last := stages[len(stages)-1]
		return len(stages) > 1 && len(last) > 0 && blocksOnAPipe(last) && !strings.Contains(strings.Join(last, " "), ">")
	})
}

func heldBy(command string, terminal bool) string {
	for _, stages := range pipelines(command) {
		for at, fields := range stages {
			if at == 0 || len(fields) == 0 {
				continue
			}
			piped := "tofu: piped into " + strings.Join(fields[:min(2, len(fields))], " ")
			switch program(fields) {
			case "tail":
				if !slices.ContainsFunc(fields, func(flag string) bool { return slices.Contains([]string{"-f", "-F", "--follow"}, flag) }) {
					return piped + ", which prints when the command ends"
				}
			case "sort", "wc", "tac":
				return piped + ", which prints when the command ends"
			case "head":
				return piped + ", which prints when it has its lines"
			}
			if blocksOnAPipe(fields) && (!terminal || at < len(stages)-1) {
				return piped + ", which prints in blocks when not on a terminal"
			}
		}
	}
	return ""
}
