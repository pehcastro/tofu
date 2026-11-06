package lsp

import (
	"cmp"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf16"

	platformproject "tofu/internal/platform/project"
)

type Location struct {
	Path      string
	Line      int
	Character int
}

type Call struct {
	Location
	Within string
}

type Usages struct {
	Calls  []Call
	Others []Location
}

type position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type span struct {
	Start position `json:"start"`
	End   position `json:"end"`
}

type scope struct {
	name        string
	first, last int
}

const (
	symbolKindMethod      = 6
	symbolKindConstructor = 9
	symbolKindFunction    = 12
)

func (l Location) compare(other Location) int {
	return cmp.Or(strings.Compare(l.Path, other.Path), cmp.Compare(l.Line, other.Line), cmp.Compare(l.Character, other.Character))
}

func Units(before string) int { return len(utf16.Encode([]rune(before))) }

func byteOffset(line string, units int) int {
	for at, r := range line {
		if units <= 0 {
			return at
		}
		units -= utf16.RuneLen(r)
	}
	return len(line)
}

func (c *Client) Usages(ctx context.Context, at Location) (Usages, error) {
	alias := regexp.MustCompile(`^\s+as\s+([A-Za-z_$][\w$]*)`)
	found, err := c.references(ctx, at)
	if err != nil {
		return Usages{}, err
	}
	read := map[string][]string{}
	scopes := map[string][]scope{}
	seen := map[Location]bool{}
	var used Usages
	for next := 0; next < len(found); next++ {
		reference := found[next].Location
		if seen[reference] {
			continue
		}
		seen[reference] = true
		if read[reference.Path] == nil {
			read[reference.Path] = Lines(reference.Path)
		}
		text := ""
		if reference.Line <= len(read[reference.Path]) {
			text = read[reference.Path][reference.Line-1]
		}
		after := text[byteOffset(text, found[next].end):]
		if renamed := alias.FindStringSubmatch(after); renamed != nil {
			aliased, err := c.aliased(ctx, at, reference.Path, read[reference.Path], renamed[1])
			if err != nil {
				return Usages{}, err
			}
			found = append(found, aliased...)
			used.Others = append(used.Others, reference)
			continue
		}
		if !called(after) {
			used.Others = append(used.Others, reference)
			continue
		}
		if _, listed := scopes[reference.Path]; !listed {
			if scopes[reference.Path], err = c.scopes(ctx, reference.Path); err != nil {
				return Usages{}, err
			}
		}
		used.Calls = append(used.Calls, Call{Location: reference, Within: innermost(scopes[reference.Path], reference.Line)})
	}
	slices.SortFunc(used.Calls, func(a, b Call) int { return a.compare(b.Location) })
	used.Calls = slices.CompactFunc(used.Calls, func(a, b Call) bool { return a.Location == b.Location })
	slices.SortFunc(used.Others, Location.compare)
	used.Others = slices.CompactFunc(used.Others, func(a, b Location) bool { return a.Path == b.Path && a.Line == b.Line })
	return used, nil
}

func Lines(path string) []string {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return strings.Split(strings.ReplaceAll(string(body), "\r\n", "\n"), "\n")
}

func WordAt(line, word string) int {
	identifier := func(at int) bool {
		return at >= 0 && at < len(line) && (line[at] == '_' || line[at] == '$' || unicode.IsLetter(rune(line[at])) || unicode.IsDigit(rune(line[at])))
	}
	for from := 0; ; {
		found := strings.Index(line[from:], word)
		if found < 0 {
			return -1
		}
		if start := from + found; !identifier(start-1) && !identifier(start+len(word)) {
			return start
		}
		from += found + len(word)
	}
}

func (c *Client) aliased(ctx context.Context, target Location, path string, text []string, word string) ([]named, error) {
	var found []named
	for number, line := range text {
		for from := 0; ; {
			column := WordAt(line[from:], word)
			if column < 0 {
				break
			}
			start := from + column
			from = start + len(word)
			candidate := Location{Path: path, Line: number + 1, Character: Units(line[:start])}
			resolves, err := c.resolvesTo(ctx, candidate, target)
			if err != nil {
				return nil, err
			}
			if resolves {
				found = append(found, named{Location: candidate, end: Units(line[:from])})
			}
		}
	}
	return found, nil
}

func (c *Client) resolvesTo(ctx context.Context, from, target Location) (bool, error) {
	var raw json.RawMessage
	if err := c.call(ctx, "textDocument/definition", from.params(), &raw); err != nil {
		return false, err
	}
	type landing struct {
		URI          string `json:"uri"`
		Range        span   `json:"range"`
		TargetURI    string `json:"targetUri"`
		TargetChosen span   `json:"targetSelectionRange"`
	}
	var targets []landing
	if json.Unmarshal(raw, &targets) != nil {
		var one landing
		_ = json.Unmarshal(raw, &one)
		targets = []landing{one}
	}
	for _, landed := range targets {
		uri, at := cmp.Or(landed.TargetURI, landed.URI), landed.Range
		if landed.TargetURI != "" {
			at = landed.TargetChosen
		}
		if samePath(uriPath(uri), target.Path) && at.Start.Line+1 == target.Line && at.Start.Character == target.Character {
			return true, nil
		}
	}
	return false, nil
}

func samePath(a, b string) bool {
	if platformproject.FoldsCase() {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

func called(after string) bool {
	rest := strings.TrimLeft(after, " \t")
	if generic := strings.TrimPrefix(rest, "::"); strings.HasPrefix(generic, "<") {
		depth := 0
		for at, r := range generic {
			if r == '<' {
				depth++
			}
			if r == '>' {
				if depth--; depth == 0 {
					rest = strings.TrimLeft(generic[at+1:], " \t")
					break
				}
			}
		}
	}
	return strings.HasPrefix(strings.TrimPrefix(rest, "?."), "(")
}

func innermost(scopes []scope, line int) string {
	var inside *scope
	for at, one := range scopes {
		if one.first <= line && line <= one.last && (inside == nil || one.last-one.first < inside.last-inside.first) {
			inside = &scopes[at]
		}
	}
	if inside == nil {
		return ""
	}
	return inside.name
}

func (l Location) params() map[string]any {
	return map[string]any{"textDocument": map[string]string{"uri": fileURI(l.Path)}, "position": position{Line: l.Line - 1, Character: l.Character}}
}

type named struct {
	Location
	end int
}

func (c *Client) references(ctx context.Context, at Location) ([]named, error) {
	params := at.params()
	params["context"] = map[string]bool{"includeDeclaration": false}
	var found []struct {
		URI   string `json:"uri"`
		Range span   `json:"range"`
	}
	if err := c.call(ctx, "textDocument/references", params, &found); err != nil {
		return nil, err
	}
	locations := make([]named, len(found))
	for i, one := range found {
		locations[i] = named{Location: Location{Path: uriPath(one.URI), Line: one.Range.Start.Line + 1, Character: one.Range.Start.Character}, end: one.Range.End.Character}
	}
	return locations, nil
}

type symbolNode struct {
	Name     string               `json:"name"`
	Kind     int                  `json:"kind"`
	Range    *span                `json:"range"`
	Location struct{ Range span } `json:"location"`
	Children []symbolNode         `json:"children"`
}

func (c *Client) scopes(ctx context.Context, path string) ([]scope, error) {
	if err := c.Open(path); err != nil {
		return nil, err
	}
	var nodes []symbolNode
	if err := c.call(ctx, "textDocument/documentSymbol", map[string]any{"textDocument": map[string]string{"uri": fileURI(path)}}, &nodes); err != nil {
		return nil, err
	}
	var found []scope
	for len(nodes) > 0 {
		node := nodes[0]
		nodes = append(nodes[1:], node.Children...)
		reach := cmp.Or(node.Range, &node.Location.Range)
		if slices.Contains([]int{symbolKindMethod, symbolKindConstructor, symbolKindFunction}, node.Kind) {
			found = append(found, scope{name: node.Name, first: reach.Start.Line + 1, last: reach.End.Line + 1})
		}
	}
	return found, nil
}
