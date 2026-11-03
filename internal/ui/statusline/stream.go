package status

import "strings"

type Stream struct{ held string }

func (s *Stream) Take(text string) (string, []Record) {
	rest := s.held + text
	s.held = ""
	var out strings.Builder
	var records []Record
	for {
		start := strings.Index(rest, introducer)
		if start < 0 {
			kept := heldPrefix(rest)
			out.WriteString(rest[:len(rest)-kept])
			s.held = rest[len(rest)-kept:]
			return out.String(), records
		}
		out.WriteString(rest[:start])
		rest = rest[start:]
		body := rest[len(introducer):]
		end := strings.IndexAny(body, "\a\x1b")
		switch {
		case end >= 0 && body[end] == '\a':
			rest = body[end+1:]
		case end >= 0 && end+1 < len(body) && body[end+1] == '\\':
			rest = body[end+2:]
		case end >= 0 && end+1 < len(body):
			out.WriteString(rest[:len(introducer)+end])
			rest = body[end:]
			continue
		case len(rest) >= maxSequence:
			out.WriteString(rest)
			return out.String(), records
		default:
			s.held = rest
			return out.String(), records
		}
		if record, err := Parse(body[:end]); err == nil {
			records = append(records, record)
		}
	}
}

func heldPrefix(text string) int {
	for size := min(len(introducer)-1, len(text)); size > 0; size-- {
		if strings.HasSuffix(text, introducer[:size]) {
			return size
		}
	}
	return 0
}
