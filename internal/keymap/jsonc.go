package keymap

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

type zedEntry struct {
	Context  string                     `json:"context"`
	Bindings map[string]json.RawMessage `json:"bindings"`
}

type jsoncArray struct {
	clean   []byte
	close   int
	entries []zedEntry
}

func closingQuote(b []byte, open int) int {
	for i := open + 1; i < len(b); i++ {
		switch b[i] {
		case '\\':
			i++
		case '"':
			return i
		}
	}
	return -1
}

func parseJSONC(source []byte) (jsoncArray, error) {
	clean := bytes.Clone(source)
	if bytes.HasPrefix(clean, []byte{0xef, 0xbb, 0xbf}) {
		copy(clean[:3], "   ")
	}
	rootClose := -1
	var stack []byte
	for i := 0; i < len(clean); i++ {
		char := clean[i]
		switch {
		case char == '"':
			if i = closingQuote(clean, i); i < 0 {
				return jsoncArray{}, errors.New("keymap must be a complete JSON array")
			}
		case char == '/' && i+1 < len(clean) && clean[i+1] == '/':
			for ; i < len(clean) && clean[i] != '\n'; i++ {
				clean[i] = ' '
			}
		case char == '/' && i+1 < len(clean) && clean[i+1] == '*':
			end := bytes.Index(clean[i+2:], []byte("*/"))
			if end < 0 {
				return jsoncArray{}, errors.New("unterminated block comment")
			}
			for j := i; j < i+end+4; j++ {
				if clean[j] != '\n' && clean[j] != '\r' {
					clean[j] = ' '
				}
			}
			i += end + 3
		case char == '[' || char == '{':
			stack = append(stack, char)
		case char == ']' || char == '}':
			opener := byte('[')
			if char == '}' {
				opener = '{'
			}
			if len(stack) == 0 || stack[len(stack)-1] != opener {
				return jsoncArray{}, errors.New("mismatched JSON bracket")
			}
			if char == ']' && len(stack) == 1 {
				rootClose = i
			}
			stack = stack[:len(stack)-1]
		}
	}
	if len(stack) != 0 || rootClose < 0 {
		return jsoncArray{}, errors.New("keymap must be a complete JSON array")
	}
	for i := 0; i < len(clean); i++ {
		switch clean[i] {
		case '"':
			i = closingQuote(clean, i)
		case ',':
			next := bytes.TrimLeft(clean[i+1:], " \t\r\n")
			if len(next) > 0 && (next[0] == ']' || next[0] == '}') {
				clean[i] = ' '
			}
		}
	}
	var entries []zedEntry
	if err := json.Unmarshal(clean, &entries); err != nil {
		return jsoncArray{}, fmt.Errorf("invalid keymap JSONC: %w", err)
	}
	return jsoncArray{clean, rootClose, entries}, nil
}
