package main

import (
	"cmp"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const byteSlack = 64

type masker struct {
	literals [][2]string
	seen     map[string]string
	counts   map[string]int
}

type ordinalClass struct {
	label   string
	pattern *regexp.Regexp
}

type fixedMask struct {
	pattern *regexp.Regexp
	into    string
}

type normalized struct {
	text  string
	bytes []int
	named bool
}

const (
	nameQualities = "amber|brisk|calm|clear|crisp|deft|eager|fair|fond|glad|keen|lucid|mild|plain|quick|quiet|rapid|ready|solid|spry|stout|sunny|tidy|tough|vivid|warm|wise|young|zesty"
	nameHues      = "ashen|azure|birch|brass|cedar|coral|dusty|flint|frost|grey|inky|ivory|jade|lilac|linen|maple|moss|olive|onyx|opal|pearl|rust|sable|sage|sandy|sepia|slate|tawny|teal|umber|wheat"
	nameCreatures = "crane|crow|dove|eagle|egret|finch|gecko|hare|hawk|heron|ibis|kite|lark|loon|lynx|mink|mole|moth|newt|otter|pike|quail|raven|robin|seal|shrew|snipe|stoat|stork|swan|swift|tern|toad|vole|wren"
)

var (
	ordinalClasses = []ordinalClass{
		{"UUID", regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)},
		{"SESSION", regexp.MustCompile(`turn-[0-9a-f]{16}`)},
		{"NAME", regexp.MustCompile(`\b(?:` + nameQualities + `)-(?:` + nameHues + `)-(?:` + nameCreatures + `)(?:[-#][0-9a-z]{5})?\b`)},
		{"TOOLREF", regexp.MustCompile(`tool#[0-9a-f]{6}`)},
		{"QUOTE", regexp.MustCompile(`quote#[0-9a-f]{6}`)},
		{"REF", regexp.MustCompile(`\b[a-z]+#([0-9a-f]{6})\b`)},
		{"TAG", regexp.MustCompile(`"session": ?"([0-9a-z]{5})"`)},
		{"REQ", regexp.MustCompile(`req-[0-9a-f]{16}`)},
		{"MEMORY", regexp.MustCompile(`\bm\d+-[0-9a-f]{4}\b`)},
		{"TAG", regexp.MustCompile(`"tag": ?"([0-9a-z]{5})"`)},
		{"TAG", regexp.MustCompile(`sessions(?:/|\\{1,2})([0-9a-z]{5})\b`)},
		{"TAG", regexp.MustCompile(`\bsession ([0-9a-z]{5})  `)},
		{"BLOB", regexp.MustCompile(`"blob": ?"([0-9a-f]{32})"`)},
		{"FAMILY", regexp.MustCompile(`#([0-9a-z]{5})\.`)},
		{"HASH", regexp.MustCompile(`(?:lead|✓|✗|○|●|·) +#([0-9a-f]{6})\b`)},
		{"STAMP", regexp.MustCompile(`\b20\d{6}-\d{6}(?:\.\d+-\d+)?\b`)},
	}
	fixedMasks = []fixedMask{
		{regexp.MustCompile(`\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}(?::\d{2}(?:\.\d+)?)?(?:Z|[+-]\d{2}:?\d{2})?`), "TIME"},
		{regexp.MustCompile(`(?i)(\bpid"?:?\s*|"process": ?|member process )\d+`), `${1}PID`},
		{regexp.MustCompile(`(?m)^ *"\w*(?:_ms|Ms)": ?\d+,?\n|,?"\w*(?:_ms|Ms)": ?\d+`), ""},
		{regexp.MustCompile(`\b(\w+_ms)[ =]\d+`), `$1 MS`},
		{regexp.MustCompile(`\b\d+(?:\.\d+)? ?ms\b|\b0s\b`), `MS`},
		{regexp.MustCompile(`,(\n *[}\]])`), "$1"},
		{regexp.MustCompile(` · MS · `), " · "},
		{regexp.MustCompile(`  \|  MS\b`), ""},
		{regexp.MustCompile(`(?m)\{"[^\n]*…$`), "{…"},
		{regexp.MustCompile(`(?m)^(.*NAME\d+)[^\n]*….*$`), "$1 …"},
		{regexp.MustCompile(`\b[0-9a-f]{8}-[0-9a-f-]*…`), "UUID…"},
		{regexp.MustCompile(`[\x{2800}-\x{28FF}]`), "SPIN"},
		{regexp.MustCompile(`\b(?:(?:Mon|Tue|Wed|Thu|Fri|Sat|Sun) )?\d{2}:\d{2}(?::\d{2})?\b`), `CLOCK`},
		{regexp.MustCompile(` {2,}(?:\d+[smhd] ago|just now)\b`), `  AGO`},
		{regexp.MustCompile(`\b\d+[smhd] ago\b`), `AGO`},
		{regexp.MustCompile(` {2,}(NAME\d+)`), `  $1`},
		{regexp.MustCompile(`("commit": ?")[^"\n]*"`), `${1}COMMIT"`},
		{regexp.MustCompile(`(?m)^(commit +)\S+$`), `${1}COMMIT`},
		{regexp.MustCompile(`\| [0-9dhms ]+ \[settings\]`), `| AGE [settings]`},
		{regexp.MustCompile(`\b\d+(?:\.\d+)?\s*[KMG]?B\b`), `SIZE`},
	}
	bytesCount = regexp.MustCompile(`\b(\d+) bytes\b|"(\w*bytes)": ?(\d+)`)
)

func newMasker(literals [][2]string) *masker {
	return &masker{literals: literals, seen: map[string]string{}, counts: map[string]int{}}
}

func (m *masker) apply(raw string) normalized {
	text := strings.ReplaceAll(raw, "\r\n", "\n")
	for _, literal := range m.literals {
		text = strings.ReplaceAll(text, literal[0], literal[1])
	}
	text = ordinals(text, func(label, found string) string {
		if named, ok := m.seen[found]; ok {
			return named
		}
		m.counts[label]++
		named := label + strconv.Itoa(m.counts[label])
		m.seen[found] = named
		return named
	})
	for _, mask := range fixedMasks {
		text = mask.pattern.ReplaceAllString(text, mask.into)
	}
	var counts []int
	text = bytesCount.ReplaceAllStringFunc(text, func(found string) string {
		parts := bytesCount.FindStringSubmatch(found)
		n, _ := strconv.Atoi(parts[1] + parts[3])
		counts = append(counts, n)
		if parts[2] != "" {
			return `"` + parts[2] + `":N`
		}
		return "N bytes"
	})
	return normalized{text: notificationsSorted(text), bytes: counts, named: m.counts["NAME"] > 0}
}

func (m *masker) unseen(raw string) string {
	text := raw
	for _, literal := range m.literals {
		text = strings.ReplaceAll(text, literal[0], literal[1])
	}
	return ordinals(text, func(label, found string) string { return cmp.Or(m.seen[found], label) })
}

func ordinals(text string, name func(label, found string) string) string {
	for _, class := range ordinalClasses {
		var out strings.Builder
		last := 0
		for _, at := range class.pattern.FindAllStringSubmatchIndex(text, -1) {
			start, end := at[0], at[1]
			if len(at) > 2 {
				start, end = at[2], at[3]
			}
			out.WriteString(text[last:start] + name(class.label, text[start:end]))
			last = end
		}
		text = out.String() + text[last:]
	}
	return text
}

func notificationsSorted(text string) string {
	lines := strings.Split(text, "\n")
	var slots []int
	var notes []string
	for i, line := range lines {
		if strings.HasPrefix(line, notificationMark) {
			slots, notes = append(slots, i), append(notes, line)
		}
	}
	slices.Sort(notes)
	for i, slot := range slots {
		lines[slot] = notes[i]
	}
	return strings.Join(lines, "\n")
}

func withoutCat(screen string) string {
	var kept []string
	inArt := false
	for _, line := range strings.Split(screen, "\n") {
		bare := strings.TrimSpace(line)
		art := bare != "" && (strings.Trim(bare, "#o+ ") == "" && strings.Contains(bare, "##") || strings.Trim(bare, "▀▄█ ") == "")
		switch {
		case art && !inArt:
			for len(kept) > 0 && strings.TrimSpace(kept[len(kept)-1]) == "" {
				kept = kept[:len(kept)-1]
			}
			kept, inArt = append(kept, "CAT"), true
		case art, inArt && bare == "":
		default:
			kept, inArt = append(kept, line), false
		}
	}
	return strings.Join(kept, "\n")
}

func pathForms(path, into string) [][2]string {
	escaped := strings.ReplaceAll(path, `\`, `\\`)
	slashed := strings.ReplaceAll(path, `\`, `/`)
	return [][2]string{{escaped, into}, {path, into}, {slashed, into}, {strings.ToLower(path), into}}
}

func (a normalized) agrees(b normalized) error {
	reflowed := a.named && b.named && slices.Equal(strings.Fields(a.text), strings.Fields(b.text))
	if a.text != b.text && !reflowed {
		return fmt.Errorf("text differs")
	}
	if len(a.bytes) != len(b.bytes) {
		return fmt.Errorf("%d byte counts against %d", len(a.bytes), len(b.bytes))
	}
	for i := range a.bytes {
		if gap := a.bytes[i] - b.bytes[i]; gap > byteSlack || gap < -byteSlack {
			return fmt.Errorf("byte count %d is %d against %d, past the %d byte slack", i+1, a.bytes[i], b.bytes[i], byteSlack)
		}
	}
	return nil
}
