package method

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var citedReportPattern = regexp.MustCompile(`bench/[\w./-]+/report-\d{4}-\d{2}-\d{2}(?:-[a-z]+)?\.md`)

var reportFilePattern = regexp.MustCompile(`^report-(\d{4}-\d{2}-\d{2})(?:-[a-z]+)?\.md$`)

var figurePattern = regexp.MustCompile(`\$\d+(?:\.\d+)?|\d+(?:\.\d+)?\s*(?:percent|%)`)

type CitationFormatError struct {
	File     string
	Point    string
	Line     int
	Measured string
}

func (e CitationFormatError) Error() string {
	return fmt.Sprintf("%s:%d %s cites %q, and no report path can be found in it", e.File, e.Line, e.Point, e.Measured)
}

type MeasuredPathError struct {
	File  string
	Point string
	Line  int
	Path  string
}

func (e MeasuredPathError) Error() string {
	return fmt.Sprintf("%s:%d %s cites %s, and no such file is on disk", e.File, e.Line, e.Point, e.Path)
}

type MeasuredFigureError struct {
	File   string
	Point  string
	Line   int
	Figure string
}

func (e MeasuredFigureError) Error() string {
	return fmt.Sprintf("%s:%d %s's measured line quotes %s, and it does not appear in the report it names", e.File, e.Line, e.Point, e.Figure)
}

type StaleAbsenceError struct {
	File   string
	Point  string
	Line   int
	Bench  string
	Report string
}

func (e StaleAbsenceError) Error() string {
	return fmt.Sprintf("%s:%d %s says measured: none, and bench/%s holds %s", e.File, e.Line, e.Point, e.Bench, e.Report)
}

type UnacknowledgedReportError struct {
	File   string
	Point  string
	Line   int
	Dir    string
	Report string
}

func (e UnacknowledgedReportError) Error() string {
	return fmt.Sprintf("%s:%d %s cites a report under %s and %s is as new or newer and named nowhere in the row", e.File, e.Line, e.Point, e.Dir, e.Report)
}

func CheckCitations(table Table, root string) []error {
	var problems []error
	for _, choice := range table.Choices {
		problems = append(problems, checkChoiceCitations(choice, table.File, root)...)
	}
	return problems
}

func checkChoiceCitations(c Choice, tableFile, root string) []error {
	measured := strings.TrimSpace(c.Measured)
	if measured == "" || measured == "none" {
		return checkStaleAbsence(c, tableFile, root)
	}
	cited := citedReportPattern.FindAllString(measured, -1)
	if len(cited) == 0 {
		return []error{CitationFormatError{File: tableFile, Point: c.Point, Line: c.Line, Measured: measured}}
	}
	var problems []error
	bodies := make(map[string]string, len(cited))
	for _, path := range cited {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			problems = append(problems, MeasuredPathError{File: tableFile, Point: c.Point, Line: c.Line, Path: path})
			continue
		}
		bodies[path] = string(data)
	}
	for _, figure := range figuresIn(measured) {
		found := false
		for _, body := range bodies {
			found = found || strings.Contains(body, figure)
		}
		if !found {
			problems = append(problems, MeasuredFigureError{File: tableFile, Point: c.Point, Line: c.Line, Figure: figure})
		}
	}
	problems = append(problems, unacknowledgedSiblings(c, tableFile, root, cited)...)
	return problems
}

func checkStaleAbsence(c Choice, tableFile, root string) []error {
	if c.Bench == "" {
		return nil
	}
	dir := filepath.Join(root, "bench", filepath.FromSlash(c.Bench))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	newest := ""
	newestDate := ""
	for _, entry := range entries {
		m := reportFilePattern.FindStringSubmatch(entry.Name())
		if m == nil || m[1] < newestDate {
			continue
		}
		newest = entry.Name()
		newestDate = m[1]
	}
	if newest == "" {
		return nil
	}
	return []error{StaleAbsenceError{File: tableFile, Point: c.Point, Line: c.Line, Bench: c.Bench, Report: newest}}
}

func figuresIn(text string) []string {
	matches := figurePattern.FindAllString(text, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, normalizeFigure(m))
	}
	return out
}

func normalizeFigure(figure string) string {
	if strings.HasPrefix(figure, "$") {
		return figure
	}
	figure = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(figure), "percent"))
	if !strings.HasSuffix(figure, "%") {
		figure += "%"
	}
	return figure
}

func unacknowledgedSiblings(c Choice, tableFile, root string, cited []string) []error {
	byDir := make(map[string][]string)
	for _, path := range cited {
		dir := filepath.ToSlash(filepath.Dir(path))
		byDir[dir] = append(byDir[dir], filepath.Base(path))
	}
	text := c.Why + " " + c.Measured + " " + c.Cost
	var problems []error
	for dir, names := range byDir {
		maxDate := ""
		citedNames := make(map[string]bool, len(names))
		for _, name := range names {
			citedNames[name] = true
			if m := reportFilePattern.FindStringSubmatch(name); m != nil && m[1] > maxDate {
				maxDate = m[1]
			}
		}
		entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(dir)))
		if err != nil {
			continue
		}
		for _, entry := range entries {
			name := entry.Name()
			m := reportFilePattern.FindStringSubmatch(name)
			if m == nil || citedNames[name] || m[1] < maxDate || strings.Contains(text, name) {
				continue
			}
			problems = append(problems, UnacknowledgedReportError{File: tableFile, Point: c.Point, Line: c.Line, Dir: dir, Report: name})
		}
	}
	return problems
}
