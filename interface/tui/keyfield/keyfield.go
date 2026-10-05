package keyfield

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"tofu/internal/sys"
)

const (
	ellipsis     = "…"
	shownRunes   = 4
	nothingYet   = "nothing typed yet"
	prefixOK     = "prefix ok"
	notChecked   = "prefix not checked"
	wrongPrefix  = "prefix is not "
	saveAnyway   = ", enter again keeps it"
	shapeSpacing = "  "
	promptGlyph  = "› "
)

func expectedPrefix(variable string) string {
	if variable == sys.OpenRouterKeyName {
		return "sk-or-v1-"
	}
	return ""
}

type Field struct {
	value, expected string
	warned          bool
}

func New(variable string) Field { return Field{expected: expectedPrefix(variable)} }

func (f *Field) Type(key string) {
	switch {
	case key == "backspace":
		runes := []rune(f.value)
		f.value = string(runes[:max(len(runes)-1, 0)])
	case utf8.RuneCountInString(key) == 1 && strings.TrimSpace(key) != "":
		f.value += key
	default:
		return
	}
	f.warned = false
}

func (f *Field) Paste(text string) {
	f.value += strings.Join(strings.Fields(text), "")
	f.warned = false
}

func (f Field) Value() string { return f.value }

func (f Field) prefixWrong() bool { return f.expected != "" && !strings.HasPrefix(f.value, f.expected) }

func (f *Field) Enter() bool {
	if f.value == "" || f.prefixWrong() && !f.warned {
		f.warned = f.value != ""
		return false
	}
	return true
}

func (f Field) Warning() string {
	if !f.warned {
		return ""
	}
	return wrongPrefix + f.expected + saveAnyway
}

func (f Field) Shape() string {
	runes := []rune(f.value)
	if len(runes) == 0 {
		return nothingYet
	}
	verdict, public := prefixOK, f.expected
	switch {
	case f.expected == "":
		verdict = notChecked
	case f.prefixWrong():
		verdict, public = wrongPrefix+f.expected, ""
	}
	secret := runes[utf8.RuneCountInString(public):]
	headRunes := 0
	if public == "" {
		headRunes = shownRunes
	}
	head, tail := public, ""
	if 2*(headRunes+shownRunes) <= len(secret) {
		head, tail = public+string(secret[:headRunes]), string(secret[len(secret)-shownRunes:])
	}
	return head + ellipsis + tail + shapeSpacing + strconv.Itoa(len(runes)) + " chars" + shapeSpacing + verdict
}

func (f Field) Line() string { return promptGlyph + f.Shape() }
