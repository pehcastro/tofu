package main

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

type screen struct {
	cells    [][]rune
	row, col int
	pending  []byte
}

func newScreen(size screenSize) *screen {
	s := &screen{cells: make([][]rune, size.rows)}
	for i := range s.cells {
		s.cells[i] = blankRow(int(size.columns))
	}
	return s
}

func blankRow(columns int) []rune {
	return []rune(strings.Repeat(" ", columns))
}

func (s *screen) String() string {
	lines := make([]string, len(s.cells))
	for i, cells := range s.cells {
		lines[i] = string(cells)
	}
	return strings.Join(lines, "\n")
}

func (s *screen) feed(chunk []byte) {
	text := string(append(s.pending, chunk...))
	s.pending = nil
	for i := 0; i < len(text); {
		switch text[i] {
		case '\x1b':
			used, complete := s.escape(text[i:])
			if !complete {
				s.pending = []byte(text[i:])
				return
			}
			i += used
		case '\r':
			s.col, i = 0, i+1
		case '\n':
			s.lineFeed()
			i++
		case '\b':
			s.col, i = max(0, s.col-1), i+1
		default:
			if !utf8.FullRuneInString(text[i:]) {
				s.pending = []byte(text[i:])
				return
			}
			r, size := utf8.DecodeRuneInString(text[i:])
			s.put(r)
			i += size
		}
	}
}

func (s *screen) put(r rune) {
	if r < ' ' {
		return
	}
	columns := len(s.cells[0])
	if s.col >= columns {
		s.col = 0
		s.lineFeed()
	}
	s.cells[s.row][s.col] = r
	s.col++
}

func (s *screen) lineFeed() {
	if s.row < len(s.cells)-1 {
		s.row++
		return
	}
	s.scrollUp(1)
}

func (s *screen) scrollUp(n int) {
	for range min(n, len(s.cells)) {
		s.cells = append(s.cells[1:], blankRow(len(s.cells[0])))
	}
}

func (s *screen) scrollDown(n int) {
	for range min(n, len(s.cells)) {
		s.cells = append([][]rune{blankRow(len(s.cells[0]))}, s.cells[:len(s.cells)-1]...)
	}
}

func (s *screen) escape(text string) (int, bool) {
	if len(text) < 2 {
		return 0, false
	}
	switch text[1] {
	case '[':
		for end := 2; end < len(text); end++ {
			if text[end] >= 0x40 && text[end] <= 0x7e {
				s.csi(text[2:end], text[end])
				return end + 1, true
			}
		}
		return 0, false
	case ']':
		for end := 2; end < len(text); end++ {
			if text[end] == '\a' {
				return end + 1, true
			}
			if text[end] == '\x1b' && end+1 < len(text) && text[end+1] == '\\' {
				return end + 2, true
			}
		}
		return 0, false
	case '(', ')':
		return min(3, len(text)), len(text) >= 3
	}
	return 2, true
}

func (s *screen) csi(params string, final byte) {
	if strings.HasPrefix(params, "?") || strings.HasPrefix(params, ">") {
		return
	}
	numbers := strings.Split(params, ";")
	arg := func(i, fallback int) int {
		if i >= len(numbers) {
			return fallback
		}
		n, err := strconv.Atoi(numbers[i])
		if err != nil || n == 0 {
			return fallback
		}
		return n
	}
	rows, columns := len(s.cells), len(s.cells[0])
	switch final {
	case 'H', 'f':
		s.row, s.col = min(arg(0, 1), rows)-1, min(arg(1, 1), columns)-1
	case 'A':
		s.row = max(0, s.row-arg(0, 1))
	case 'B':
		s.row = min(rows-1, s.row+arg(0, 1))
	case 'C':
		s.col = min(columns-1, s.col+arg(0, 1))
	case 'D':
		s.col = max(0, s.col-arg(0, 1))
	case 'G':
		s.col = min(arg(0, 1), columns) - 1
	case 'd':
		s.row = min(arg(0, 1), rows) - 1
	case 'K':
		s.eraseLine(arg(0, 0))
	case 'J':
		s.eraseScreen(arg(0, 0))
	case 'X':
		for c := s.col; c < min(columns, s.col+arg(0, 1)); c++ {
			s.cells[s.row][c] = ' '
		}
	case 'S':
		s.scrollUp(arg(0, 1))
	case 'T':
		s.scrollDown(arg(0, 1))
	case 'L':
		below := append([][]rune{}, s.cells[s.row:]...)
		for range min(arg(0, 1), len(below)) {
			below = append([][]rune{blankRow(columns)}, below[:len(below)-1]...)
		}
		s.cells = append(s.cells[:s.row], below...)
	case 'M':
		below := append([][]rune{}, s.cells[s.row:]...)
		for range min(arg(0, 1), len(below)) {
			below = append(below[1:], blankRow(columns))
		}
		s.cells = append(s.cells[:s.row], below...)
	}
}

func (s *screen) eraseLine(mode int) {
	from, to := s.col, len(s.cells[0])
	switch mode {
	case 1:
		from, to = 0, s.col+1
	case 2:
		from = 0
	}
	for c := from; c < min(to, len(s.cells[0])); c++ {
		s.cells[s.row][c] = ' '
	}
}

func (s *screen) eraseScreen(mode int) {
	from, to := s.row+1, len(s.cells)
	switch mode {
	case 0:
		s.eraseLine(0)
	case 1:
		from, to = 0, s.row
		s.eraseLine(1)
	default:
		from = 0
	}
	for r := from; r < to; r++ {
		s.cells[r] = blankRow(len(s.cells[0]))
	}
}
