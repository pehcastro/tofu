package search

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"tofu/internal/sys"
)

type Claim string

const (
	ClaimResolved Claim = "resolved"
	ClaimNoFile   Claim = "no_file"
	ClaimNoLine   Claim = "no_line"
	ClaimNoSymbol Claim = "no_symbol"
)

type Citation struct {
	Text      string
	Path      string
	FirstLine int
	LastLine  int
	Symbol    string
	Claim     Claim
	Detail    string
}

type Cited struct {
	Found    []Citation
	Resolved int
	Refused  int
	NotFound int
	Text     string
}

var citationPattern = sys.LazyRegexp(
	`(?:^|[^A-Za-z0-9_./\\-])((?:\.\.?[/\\])*\.?[A-Za-z0-9_][A-Za-z0-9_./\\-]*\.[A-Za-z][A-Za-z0-9]{0,9})` +
		`:([1-9][0-9]{0,8})(?:-([1-9][0-9]{0,8}))?` +
		`(?::([A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)?))?`)

type fileBody struct {
	lines   []string
	present bool
}

func Cite(root, text string, moved ...string) Cited {
	roots := append(slices.Clip(moved), root)
	places := searchedPlaces(root, moved)
	read := map[string]fileBody{}
	var cited Cited
	for _, match := range citationPattern().FindAllStringSubmatch(text, -1) {
		one := Citation{Path: filepath.ToSlash(match[1]), Symbol: match[4]}
		one.FirstLine, _ = strconv.Atoi(match[2])
		one.LastLine = one.FirstLine
		if match[3] != "" {
			one.LastLine, _ = strconv.Atoi(match[3])
		}
		one.Text = spell(one)
		if _, known := read[one.Path]; !known {
			read[one.Path] = firstPresent(roots, one.Path)
		}
		one.Claim, one.Detail = resolveCitation(one, read[one.Path], places)
		switch one.Claim {
		case ClaimResolved:
			cited.Resolved++
		case ClaimNoFile:
			cited.NotFound++
		}
		cited.Found = append(cited.Found, one)
	}
	cited.Refused = len(cited.Found) - cited.Resolved - cited.NotFound
	cited.Text = renderCitations(cited)
	return cited
}

func spell(one Citation) string {
	text := one.Path + ":" + strconv.Itoa(one.FirstLine)
	if one.LastLine != one.FirstLine {
		text += "-" + strconv.Itoa(one.LastLine)
	}
	if one.Symbol != "" {
		text += ":" + one.Symbol
	}
	return text
}

func searchedPlaces(root string, moved []string) string {
	places := []string{"the working directory"}
	for _, dir := range moved {
		if rel, err := filepath.Rel(root, dir); err == nil && !strings.HasPrefix(rel, "..") {
			dir = rel
		}
		places = append(places, filepath.ToSlash(dir))
	}
	return strings.Join(places, " or ")
}

func firstPresent(roots []string, rel string) fileBody {
	for _, root := range roots {
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		switch {
		case err != nil:
			continue
		case len(body) == 0:
			return fileBody{present: true}
		}
		unwound := strings.TrimSuffix(strings.ReplaceAll(string(body), "\r\n", "\n"), "\n")
		return fileBody{lines: strings.Split(unwound, "\n"), present: true}
	}
	return fileBody{}
}

func resolveCitation(one Citation, file fileBody, places string) (Claim, string) {
	switch {
	case !file.present:
		return ClaimNoFile, "no file is at that path under " + places + ", so it was not checked"
	case one.LastLine < one.FirstLine:
		return ClaimNoLine, "the range ends before it begins"
	case one.LastLine > len(file.lines):
		return ClaimNoLine, fmt.Sprintf("the file has %d lines", len(file.lines))
	case one.Symbol == "":
		return ClaimResolved, ""
	}
	for _, line := range file.lines[one.FirstLine-1 : one.LastLine] {
		if namesSymbol(line, one.Symbol) {
			return ClaimResolved, ""
		}
	}
	return ClaimNoSymbol, "the lines it names do not hold " + one.Symbol
}

func namesSymbol(line, symbol string) bool {
	for at := 0; ; {
		index := strings.Index(line[at:], symbol)
		if index < 0 {
			return false
		}
		start := at + index
		if !identifierByte(line, start-1) && !identifierByte(line, start+len(symbol)) {
			return true
		}
		at = start + len(symbol)
	}
}

func identifierByte(line string, at int) bool {
	if at < 0 || at >= len(line) {
		return false
	}
	character := line[at]
	return character == '_' ||
		('0' <= character && character <= '9') ||
		('a' <= character && character <= 'z') ||
		('A' <= character && character <= 'Z')
}

func renderCitations(cited Cited) string {
	if len(cited.Found) == 0 {
		return ""
	}
	lines := []string{fmt.Sprintf("citations: %d found, %d resolved", len(cited.Found), cited.Resolved)}
	if cited.NotFound > 0 {
		lines[0] += fmt.Sprintf(", %d not found", cited.NotFound)
	}
	if cited.Refused > 0 {
		lines[0] += fmt.Sprintf(", %d refused. a refused citation is not evidence and must not be repeated as one", cited.Refused)
	}
	for _, one := range cited.Found {
		switch one.Claim {
		case ClaimResolved:
		case ClaimNoFile:
			lines = append(lines, "not found "+one.Text+": "+one.Detail)
		default:
			lines = append(lines, fmt.Sprintf("refused %s %s: %s", one.Text, one.Claim, one.Detail))
		}
	}
	return strings.Join(lines, "\n")
}
