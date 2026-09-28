package markdown

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/look"
)

const (
	tableGutter   = " │ "
	tableRule     = "─"
	tableJunction = "─┼─"
)

func opensTable(lines []string, at int) bool {
	if at+1 >= len(lines) || !strings.Contains(lines[at], "|") {
		return false
	}
	delimiter := tableCells(lines[at+1])
	for _, cell := range delimiter {
		if dashes := strings.Trim(cell, ":"); dashes == "" || strings.Trim(dashes, "-") != "" {
			return false
		}
	}
	return len(delimiter) == len(tableCells(lines[at]))
}

func tableCells(line string) []string {
	row := strings.TrimPrefix(strings.TrimSpace(line), "|")
	if strings.HasSuffix(row, "|") && !strings.HasSuffix(row, `\|`) {
		row = row[:len(row)-1]
	}
	var cells []string
	start := 0
	for at := 0; at < len(row); at++ {
		switch row[at] {
		case '\\':
			at++
		case '|':
			cells, start = append(cells, strings.TrimSpace(row[start:at])), at+1
		}
	}
	return append(cells, strings.TrimSpace(row[start:]))
}

func (r *Renderer) table(text string, width int) ([]string, error) {
	source := strings.Split(text, "\n")
	columns := len(tableCells(source[0]))
	available := width - ansi.StringWidth(tableGutter)*(columns-1)
	if available < columns {
		return r.render(text)
	}
	var cells [][]string
	widths := make([]int, columns)
	for index, line := range source {
		if index == 1 {
			continue
		}
		row, values := make([]string, columns), tableCells(line)
		for column := range min(columns, len(values)) {
			rendered, err := r.render(inlineOnly(values[column]))
			if err != nil {
				return nil, err
			}
			for at := range rendered {
				rendered[at] = trimRight(rendered[at])
			}
			row[column] = strings.Join(rendered, " ")
			if index == 0 {
				row[column] = ansi.Strip(row[column])
			}
			widths[column] = max(widths[column], ansi.StringWidth(row[column]))
		}
		cells = append(cells, row)
	}
	total := 0
	for _, cellWidth := range widths {
		total += cellWidth
	}
	for ; total > available; total-- {
		widest := 0
		for column, cellWidth := range widths {
			if cellWidth > widths[widest] {
				widest = column
			}
		}
		widths[widest]--
	}
	faint, header := look.Style(look.FaintColor), look.Style(look.Text).Bold(true)
	separator := faint.Render(tableGutter)
	var lines []string
	for index, row := range cells {
		wrapped, height := make([][]string, columns), 1
		for column, cell := range row {
			wrapped[column] = strings.Split(ansi.Wrap(cell, widths[column], ""), "\n")
			height = max(height, len(wrapped[column]))
		}
		for at := range height {
			parts := make([]string, columns)
			for column, cellLines := range wrapped {
				line := ""
				if at < len(cellLines) {
					line = cellLines[at]
				}
				padding := strings.Repeat(" ", widths[column]-ansi.StringWidth(line))
				if index == 0 {
					line = header.Render(line)
				}
				parts[column] = line + ansi.ResetStyle + padding
			}
			lines = append(lines, strings.Join(parts, separator))
		}
		if index == 0 {
			rules := make([]string, columns)
			for column, cellWidth := range widths {
				rules[column] = strings.Repeat(tableRule, cellWidth)
			}
			lines = append(lines, faint.Render(strings.Join(rules, tableJunction)))
		}
	}
	return lines, nil
}

func inlineOnly(cell string) string {
	if cell == "-" || cell == "+" || cell == "*" || isHeading(cell) || isSetextUnderline(cell) || strings.HasPrefix(cell, ">") || cell != "" && isListMarker(cell) {
		digits := len(cell) - len(strings.TrimLeft(cell, "0123456789"))
		return cell[:digits] + `\` + cell[digits:]
	}
	return cell
}
