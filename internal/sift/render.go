package sift

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const fenceLine = "--- sift ---"

var (
	markerPattern = regexp.MustCompile(`^\[sift:(\d+) [^\]]*\]$`)
	summaryLine   = regexp.MustCompile(`^kept \d+/\d+ ends ([01])$`)
)

func isFence(line string) bool { return strings.TrimRight(line, "\r\n") == fenceLine }

func isMarkerLine(line string) bool {
	return markerPattern.MatchString(strings.TrimRight(line, "\r\n"))
}

func Render(parts []Part, marks []Mark) string {
	var body strings.Builder
	var records strings.Builder
	kept := 0
	for i, p := range parts {
		if marks[i].Keep {
			kept++
			body.WriteString(p.Text)
			body.WriteString(p.Sep)
			continue
		}
		fmt.Fprintf(&body, "[sift:%d %s]\n", i, marks[i].Reason)
		fmt.Fprintf(&records, "%d\t%s\n", i, strconv.Quote(p.Text+p.Sep))
	}
	text := body.String()
	ends := "1"
	if !strings.HasSuffix(text, "\n") {
		ends = "0"
		text += "\n"
	}
	return fmt.Sprintf("%s%s\nkept %d/%d ends %s\n%s", text, fenceLine, kept, len(parts), ends, records.String())
}

func Restore(rendered string) (string, error) {
	all := lines(rendered)
	fence := -1
	for i, line := range all {
		if isFence(line) {
			fence = i
		}
	}
	if fence < 0 {
		return "", fmt.Errorf("sift: the text carries no %q fence, so there is nothing to restore", fenceLine)
	}
	if fence+1 >= len(all) {
		return "", fmt.Errorf("sift: the %q fence is not followed by a summary", fenceLine)
	}
	summary := summaryLine.FindStringSubmatch(strings.TrimRight(all[fence+1], "\r\n"))
	if summary == nil {
		return "", fmt.Errorf("sift: %q is not a sift summary", strings.TrimRight(all[fence+1], "\r\n"))
	}

	records := map[int]string{}
	for _, line := range all[fence+2:] {
		index, text, err := parseRecord(line)
		if err != nil {
			return "", err
		}
		records[index] = text
	}

	var out strings.Builder
	for _, line := range all[:fence] {
		marker := markerPattern.FindStringSubmatch(strings.TrimRight(line, "\r\n"))
		if marker == nil {
			out.WriteString(line)
			continue
		}
		index, _ := strconv.Atoi(marker[1])
		text, ok := records[index]
		if !ok {
			return "", fmt.Errorf("sift: marker %s has no record, so the original is not recoverable", marker[1])
		}
		out.WriteString(text)
	}
	text := out.String()
	if summary[1] == "0" {
		text = strings.TrimSuffix(text, "\n")
	}
	return text, nil
}

func parseRecord(line string) (int, string, error) {
	field, quoted, ok := strings.Cut(strings.TrimRight(line, "\r\n"), "\t")
	if !ok {
		return 0, "", fmt.Errorf("sift: %q is not an index and a quoted paragraph", line)
	}
	index, err := strconv.Atoi(field)
	if err != nil {
		return 0, "", fmt.Errorf("sift: %q does not start with a paragraph index: %w", line, err)
	}
	text, err := strconv.Unquote(quoted)
	if err != nil {
		return 0, "", fmt.Errorf("sift: the record for paragraph %d is not a quoted string: %w", index, err)
	}
	return index, text, nil
}
