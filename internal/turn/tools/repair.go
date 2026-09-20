package tools

import (
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"tofu/internal/turn"
)

func lookalikes(root turn.Root, requested string, withDirectories bool) ([]string, error) {
	listed, err := filesUnder(root, ".", false)
	if err != nil {
		return nil, err
	}
	names := listed.files
	if withDirectories {
		names = slices.Concat(listed.files, directoriesOf(listed.files))
	}
	wanted := strings.ToLower(path.Clean(filepath.ToSlash(requested)))
	base := path.Base(wanted)
	var found []string
	for _, name := range names {
		lower := strings.ToLower(name)
		if lower == wanted || path.Base(lower) == base {
			found = append(found, name)
		}
	}
	slices.Sort(found)
	return slices.Compact(found), nil
}

func directoriesOf(files []string) []string {
	seen := map[string]bool{}
	var dirs []string
	for _, file := range files {
		for dir := path.Dir(file); dir != "." && dir != "/"; dir = path.Dir(dir) {
			if !seen[dir] {
				seen[dir] = true
				dirs = append(dirs, dir)
			}
		}
	}
	return dirs
}

func repairPath(root turn.Root, requested string, withDirectories bool) (string, string, error) {
	noun := "file"
	if withDirectories {
		noun = "path"
	}
	found, err := lookalikes(root, requested, withDirectories)
	if err != nil {
		return "", "", err
	}
	return turn.RepairPath(requested, noun, found)
}

func exactOccurrences(body, wanted string) []int {
	var lines []int
	for at := 0; ; {
		found := strings.Index(body[at:], wanted)
		if found < 0 {
			return lines
		}
		lines = append(lines, 1+strings.Count(body[:at+found], "\n"))
		at += found + len(wanted)
	}
}

func trimmedLines(lines []string) []string {
	trimmed := make([]string, len(lines))
	for i, line := range lines {
		trimmed[i] = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
	}
	return trimmed
}

func whitespaceOccurrences(lines []string, wanted string) []int {
	block := trimmedLines(strings.Split(wanted, "\n"))
	if len(block) == 1 && block[0] == "" {
		return nil
	}
	var starts []int
	for at := 0; at+len(block) <= len(lines); at++ {
		if slices.Equal(trimmedLines(lines[at:at+len(block)]), block) {
			starts = append(starts, at+1)
		}
	}
	return starts
}

func listLines(lines []string, at []int) string {
	parts := make([]string, len(at))
	for i, number := range at {
		text := ""
		if number-1 < len(lines) {
			text = strings.TrimSpace(lines[number-1])
		}
		parts[i] = fmt.Sprintf("line %d: %s", number, text)
	}
	return strings.Join(parts, "; ")
}

func replaceOnce(before, shown, wanted, becomes string) (string, string, error) {
	lines := strings.Split(before, "\n")
	exact := exactOccurrences(before, wanted)
	if len(exact) == 1 {
		return strings.Replace(before, wanted, becomes, 1), "", nil
	}
	if len(exact) > 1 {
		return "", "", fmt.Errorf("edit: old_string appears %d times in %s and an edit names exactly one. every occurrence: %s. add the lines above or below it until it is unique: nothing was changed",
			len(exact), shown, listLines(lines, exact))
	}

	block := len(strings.Split(wanted, "\n"))
	at := whitespaceOccurrences(lines, wanted)
	switch len(at) {
	case 1:
		first := at[0]
		last := first + block - 1
		repaired := slices.Concat(lines[:first-1], strings.Split(becomes, "\n"), lines[last:])
		return strings.Join(repaired, "\n"), fmt.Sprintf("repaired: old_string is not in %s character for character, and lines %d to %d are the only run there that differs from it in whitespace alone, so that run was replaced. copy the text from the file next time",
			shown, first, last), nil
	case 0:
		return "", "", fmt.Errorf("edit: no text in %s matches old_string, and no run of %d lines there differs from it in whitespace alone: read the file and copy the text exactly. nothing was changed",
			shown, block)
	}
	return "", "", fmt.Errorf("edit: old_string is not in %s character for character, and %d runs of lines there differ from it in whitespace alone: %s. a repair is only made when it is the only candidate, so nothing was changed: copy the text from the file",
		shown, len(at), listLines(lines, at))
}
