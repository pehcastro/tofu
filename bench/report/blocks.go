package report

import "strings"

type BlockKind string

const (
	BlockHeading   BlockKind = "heading"
	BlockParagraph BlockKind = "paragraph"
	BlockList      BlockKind = "list"
	BlockQuote     BlockKind = "quote"
	BlockCode      BlockKind = "code"
	BlockTable     BlockKind = "table"
	BlockChart     BlockKind = "chart"
)

type Block struct {
	Kind  BlockKind  `json:"kind"`
	Level int        `json:"level,omitempty"`
	Text  string     `json:"text,omitempty"`
	Items []string   `json:"items,omitempty"`
	Head  []string   `json:"head,omitempty"`
	Rows  [][]string `json:"rows,omitempty"`
	Chart *Chart     `json:"chart,omitempty"`
}

func blocksOf(body string) []Block {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	for _, line := range lines {
		if markdownHeading.MatchString(strings.TrimRight(line, " \t")) {
			return markdownBlocks(lines)
		}
	}
	return plainBlocks(lines)
}

func plainBlocks(lines []string) []Block {
	var blocks []Block
	var held []string
	flush := func() {
		text := strings.Trim(strings.Join(held, "\n"), "\n")
		held = nil
		if text != "" {
			blocks = append(blocks, Block{Kind: BlockCode, Text: text})
		}
	}
	for i, line := range lines {
		if i == 0 {
			continue
		}
		if shoutedHeading.MatchString(strings.TrimRight(line, " \t")) {
			flush()
			blocks = append(blocks, Block{Kind: BlockHeading, Level: 2, Text: strings.TrimSpace(line)})
			continue
		}
		held = append(held, line)
	}
	flush()
	return blocks
}

func markdownBlocks(lines []string) []Block {
	var blocks []Block
	for i := 0; i < len(lines); {
		line := strings.TrimRight(lines[i], " \t")
		switch {
		case strings.TrimSpace(line) == "":
			i++
		case strings.HasPrefix(line, "```"):
			start := i + 1
			for i++; i < len(lines) && !strings.HasPrefix(lines[i], "```"); i++ {
			}
			blocks = append(blocks, Block{Kind: BlockCode, Text: strings.Join(lines[start:i], "\n")})
			i++
		case markdownHeading.MatchString(line):
			match := markdownHeading.FindStringSubmatch(line)
			blocks = append(blocks, Block{Kind: BlockHeading, Level: len(line) - len(strings.TrimLeft(line, "#")), Text: plain(match[1])})
			i++
		case strings.HasPrefix(line, "|"):
			start := i
			for ; i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), "|"); i++ {
			}
			blocks = append(blocks, tableBlocks(lines[start:i])...)
		case strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* "):
			var items []string
			for i < len(lines) {
				text := strings.TrimRight(lines[i], " \t")
				if strings.HasPrefix(text, "- ") || strings.HasPrefix(text, "* ") {
					items = append(items, plain(text[2:]))
					i++
					continue
				}
				if strings.HasPrefix(text, "  ") && strings.TrimSpace(text) != "" && len(items) > 0 {
					items[len(items)-1] += " " + plain(strings.TrimSpace(text))
					i++
					continue
				}
				break
			}
			blocks = append(blocks, Block{Kind: BlockList, Items: items})
		case strings.HasPrefix(line, ">"):
			var held []string
			for ; i < len(lines) && strings.HasPrefix(strings.TrimRight(lines[i], " \t"), ">"); i++ {
				held = append(held, plain(strings.TrimPrefix(strings.TrimSpace(lines[i]), ">")))
			}
			blocks = append(blocks, Block{Kind: BlockQuote, Text: strings.TrimSpace(strings.Join(held, " "))})
		default:
			var held []string
			for ; i < len(lines) && !startsABlock(lines[i]); i++ {
				held = append(held, strings.TrimSpace(lines[i]))
			}
			blocks = append(blocks, Block{Kind: BlockParagraph, Text: plain(strings.Join(held, " "))})
		}
	}
	return blocks
}

func startsABlock(line string) bool {
	text := strings.TrimRight(line, " \t")
	return strings.TrimSpace(text) == "" ||
		strings.HasPrefix(text, "```") ||
		strings.HasPrefix(text, "|") ||
		strings.HasPrefix(text, ">") ||
		strings.HasPrefix(text, "- ") ||
		strings.HasPrefix(text, "* ") ||
		markdownHeading.MatchString(text)
}

func tableBlocks(lines []string) []Block {
	table := Block{Kind: BlockTable}
	for _, line := range lines {
		cells := splitRow(line)
		if isRule(cells) {
			continue
		}
		if table.Head == nil {
			table.Head = cells
			continue
		}
		table.Rows = append(table.Rows, cells)
	}
	if chart, ok := chartFor(table); ok {
		return []Block{table, {Kind: BlockChart, Chart: &chart}}
	}
	return []Block{table}
}

func splitRow(line string) []string {
	cells := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
	for i, cell := range cells {
		cells[i] = plain(cell)
	}
	return cells
}

func isRule(cells []string) bool {
	for _, cell := range cells {
		if strings.Trim(cell, "-: ") != "" {
			return false
		}
	}
	return true
}
