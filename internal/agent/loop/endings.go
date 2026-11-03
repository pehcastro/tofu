package turn

import "strings"

const byteOrderMark = "\xef\xbb\xbf"

type LineEndings struct{ bom, crlf bool }

func EndingsOf(body string) (string, LineEndings) {
	text := strings.TrimPrefix(body, byteOrderMark)
	crlf := strings.Contains(text, "\r\n") && strings.Count(text, "\r\n") == strings.Count(text, "\n")
	if crlf {
		text = strings.ReplaceAll(text, "\r\n", "\n")
	}
	return text, LineEndings{bom: strings.HasPrefix(body, byteOrderMark), crlf: crlf}
}

func (l LineEndings) Restore(text string) string {
	if l.crlf {
		text = strings.ReplaceAll(text, "\n", "\r\n")
	}
	if l.bom {
		text = byteOrderMark + text
	}
	return text
}

func AsLF(text string) string {
	return strings.ReplaceAll(strings.TrimPrefix(text, byteOrderMark), "\r\n", "\n")
}
