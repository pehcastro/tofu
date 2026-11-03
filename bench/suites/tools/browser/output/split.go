package main

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
)

type part int

const (
	partHeader part = iota
	partRefs
	partText
	partStructure
	partCount
)

type shape int

const (
	shapeNoTree shape = iota
	shapeFullText
	shapeFullInteractive
	shapeDelta
	shapeUnchanged
	shapeCount
)

var shapeNames = [shapeCount]string{"no_tree", "full_text", "full_inter", "delta", "unchanged"}

var (
	beginMark = regexp.MustCompile(`^<<<([0-9a-f]{16}) begins>>>$`)
	refMark   = regexp.MustCompile(`ref=e\d+[,\]]`)
	urlMark   = regexp.MustCompile(`, url=[^\]]*`)
	cutMark   = regexp.MustCompile(`^\[cut: lines \d+ to \d+, (\d+) bytes`)
)

var headerStarts = []string{"[cut: ", "changed since the last snapshot", "nothing changed since the last snapshot", "behind dialog "}

type tree struct {
	url, tabLine string
	body         []string
	texts        []string
	textBytes    int
	cutBytes     int
	shape        shape
}

type splitResult struct {
	bytes    [partCount]int
	urlBytes int
	roles    map[string]int
	last     tree
}

func split(content string) splitResult {
	result := splitResult{roles: map[string]int{}}
	lines := strings.Split(content, "\n")
	var blocks [][]string
	for i := 0; i < len(lines); i++ {
		found := beginMark.FindStringSubmatch(lines[i])
		result.bytes[partHeader] += len(lines[i]) + 1
		if found == nil {
			continue
		}
		end := "<<<" + found[1] + " ends>>>"
		start := i + 1
		for i++; i < len(lines) && lines[i] != end; i++ {
		}
		if i < len(lines) {
			result.bytes[partHeader] += len(lines[i]) + 1
		}
		blocks = append(blocks, lines[start:min(i, len(lines))])
	}
	for _, block := range blocks {
		for n, line := range block {
			kind, role := classify(line, n == 0)
			result.bytes[kind] += len(line) + 1
			switch kind {
			case partStructure:
				result.roles[role] += len(line) + 1
			case partRefs:
				result.urlBytes += len(urlMark.FindString(line))
			}
		}
	}
	if len(blocks) > 0 {
		result.last = readTree(blocks[len(blocks)-1])
	}
	return result
}

func classify(line string, first bool) (part, string) {
	trimmed := strings.TrimLeft(line, " ")
	if trimmed == "" || first && strings.HasPrefix(trimmed, "tab ") || slices.ContainsFunc(headerStarts, func(start string) bool { return strings.HasPrefix(trimmed, start) }) {
		return partHeader, ""
	}
	for _, prefix := range []string{"x gone: ", "+ ", "~ "} {
		trimmed = strings.TrimPrefix(trimmed, prefix)
	}
	node, bulleted := strings.CutPrefix(trimmed, "- ")
	if !bulleted {
		node, bulleted = strings.CutPrefix(trimmed, "* ")
	}
	role, _, _ := strings.Cut(node, " ")
	switch {
	case !bulleted:
		return partText, "(plain)"
	case role == "StaticText":
		return partText, role
	case refMark.MatchString(node):
		return partRefs, role
	}
	return partStructure, role
}

func readTree(block []string) tree {
	if len(block) == 0 || !strings.HasPrefix(block[0], "tab ") {
		return tree{shape: shapeNoTree}
	}
	fields := strings.Fields(block[0])
	found := tree{tabLine: block[0], body: block[1:], shape: shapeFullInteractive}
	if len(fields) > 2 {
		found.url = fields[2]
	}
	if len(block) > 1 {
		switch {
		case strings.HasPrefix(block[1], "changed since the last snapshot"):
			found.shape = shapeDelta
			return found
		case strings.HasPrefix(block[1], "nothing changed since the last snapshot"):
			found.shape = shapeUnchanged
			return found
		}
	}
	var other strings.Builder
	for _, line := range found.body {
		if cut := cutMark.FindStringSubmatch(line); cut != nil {
			found.cutBytes, _ = strconv.Atoi(cut[1])
		}
		if kind, _ := classify(line, false); kind != partText {
			other.WriteString(line + "\n")
			continue
		}
		found.textBytes += len(line) + 1
		name := strings.TrimSpace(line)
		if at := strings.IndexByte(line, '"'); at >= 0 {
			if quoted, err := strconv.QuotedPrefix(line[at:]); err == nil {
				name, _ = strconv.Unquote(quoted)
			}
		}
		found.texts = append(found.texts, name)
	}
	if found.textBytes > 0 {
		found.shape = shapeFullText
	}
	found.texts = slices.DeleteFunc(found.texts, func(text string) bool {
		return len(text) < quoteMinBytes || strings.Contains(other.String(), text)
	})
	return found
}
