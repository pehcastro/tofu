package nativetools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"tofu/internal/konst"
	"tofu/internal/turn"
	"tofu/internal/turn/tools"
)

type Outcome string

const (
	Right             Outcome = "right"
	RefusedWithReason Outcome = "refused, says why"
	SilentNoop        Outcome = "no change, silent"
	SilentWrong       Outcome = "wrong, silent"
	WrongSaid         Outcome = "wrong, says something"
	Guessed           Outcome = "no unique target, changed silently"
	NoTarget          Outcome = "no target"
)

var Outcomes = []Outcome{Right, RefusedWithReason, SilentNoop, SilentWrong, WrongSaid, Guessed, NoTarget}

type EditArm struct {
	Outcome        Outcome
	Returned, Sent int
}

type EditRow struct {
	Case                             EditCase
	Tofu, SedLine, SedWhole, HereDoc EditArm
}

type ReadArm struct {
	Returned             int
	Complete, SecondCall bool
}

type ReadRow struct {
	Case                 ReadCase
	FileLines, Wanted    int
	Tofu, Cat, Sed, Head ReadArm
}

type ListRow struct {
	Case                            ListCase
	Find                            string
	GlobBytes, GlobPaths, FindBytes int
	FindPaths, LsBytes              int
	Differ                          []string
}

type editRunner func(dir string, edit EditCase) (output string, failed bool, sent int, err error)

func RunEdits(corpus Corpus, scratch string) ([]EditRow, error) {
	var rows []EditRow
	for _, edit := range corpus.Edits {
		before := corpus.Files[edit.Path]
		expected := before
		var rewrite editRunner
		if edit.Occurrences == 1 {
			expected = strings.Replace(before, edit.Old, edit.New, 1)
			rewrite = shellEdit(func(edit EditCase) string { return hereDoc(edit.Path, expected) })
		}
		row := EditRow{Case: edit, HereDoc: EditArm{Outcome: NoTarget}}
		arms := []struct {
			name string
			into *EditArm
			run  editRunner
		}{
			{"tofu", &row.Tofu, tofuEdit},
			{"sedline", &row.SedLine, shellEdit(sedLine)},
			{"sedwhole", &row.SedWhole, shellEdit(sedWhole)},
			{"heredoc", &row.HereDoc, rewrite},
		}
		for _, arm := range arms {
			if arm.run == nil {
				continue
			}
			dir := filepath.Join(scratch, edit.ID, arm.name)
			if err := place(dir, edit.Path, before); err != nil {
				return nil, err
			}
			output, failed, sent, err := arm.run(dir, edit)
			if err != nil {
				return nil, fmt.Errorf("%s %s: %w", edit.ID, arm.name, err)
			}
			after, err := os.ReadFile(filepath.Join(dir, edit.Path))
			if err != nil {
				return nil, err
			}
			*arm.into = EditArm{
				Outcome:  classify(before, expected, string(after), output != "" || failed),
				Returned: len(output),
				Sent:     sent,
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func classify(before, expected, after string, said bool) Outcome {
	switch {
	case after == expected && expected != before:
		return Right
	case after == before && said:
		return RefusedWithReason
	case after == before:
		return SilentNoop
	case said:
		return WrongSaid
	case expected == before:
		return Guessed
	}
	return SilentWrong
}

func tofuEdit(dir string, edit EditCase) (string, bool, int, error) {
	tool, err := tools.NewEdit(dir)
	if err != nil {
		return "", false, 0, err
	}
	raw, err := json.Marshal(map[string]string{"path": edit.Path, "old_string": edit.Old, "new_string": edit.New})
	if err != nil {
		return "", false, 0, err
	}
	result, err := tool.Run(context.Background(), raw)
	if err != nil {
		return err.Error(), true, len(raw), nil
	}
	if strings.Contains(result.Content, "\n\ntypecheck: ") {
		return "", false, 0, errors.New("a tsconfig.json above the scratch directory started a real typecheck")
	}
	content, _, _ := strings.Cut(result.Content, "\n\ntypecheck skipped: ")
	return content, false, len(raw), nil
}

func shellEdit(command func(EditCase) string) editRunner {
	return func(dir string, edit EditCase) (string, bool, int, error) {
		script := command(edit)
		output, failed, err := shell(dir, script)
		return output, failed, len(script), err
	}
}

func sedLine(edit EditCase) string {
	old, new := changedLines(edit.Old, edit.New)
	if !oneLineChange(old) {
		return sedWhole(edit)
	}
	if len(new) == 0 {
		return "sed -i " + quote("/"+sedPattern(old[0])+"/d") + " " + quote(edit.Path)
	}
	return "sed -i " + quote("s/"+sedPattern(old[0])+"/"+sedReplacement(strings.Join(new, "\n"))+"/") + " " + quote(edit.Path)
}

func sedWhole(edit EditCase) string {
	return "sed -i -z " + quote("s/"+sedPattern(edit.Old)+"/"+sedReplacement(edit.New)+"/") + " " + quote(edit.Path)
}

func hereDoc(path, content string) string {
	end := "TOFU_EOF"
	for slices.Contains(lines(content), end) {
		end += "_"
	}
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return "cat > " + quote(path) + " <<'" + end + "'\n" + content + end + "\n"
}

func sedPattern(text string) string {
	return escape(text, `\.*[]^$/`)
}

func sedReplacement(text string) string {
	return escape(text, `\&/`)
}

func escape(text, special string) string {
	b := &strings.Builder{}
	for _, r := range text {
		switch {
		case r == '\n':
			b.WriteString(`\n`)
		case strings.ContainsRune(special, r):
			b.WriteRune('\\')
			b.WriteRune(r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func quote(text string) string {
	return "'" + strings.ReplaceAll(text, "'", `'\''`) + "'"
}

func shell(dir, script string) (string, bool, error) {
	cmd := exec.Command("sh", "-s")
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(script)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return out.String(), true, nil
	}
	return out.String(), false, err
}

func place(dir, path, body string) error {
	target := filepath.Join(dir, path)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return os.WriteFile(target, []byte(body), 0o600)
}

func RunReads(corpus Corpus, scratch string) ([]ReadRow, error) {
	for _, read := range corpus.Reads {
		if err := place(scratch, read.Path, corpus.Files[read.Path]); err != nil {
			return nil, err
		}
	}
	tool, err := turn.NewReadTool(scratch)
	if err != nil {
		return nil, err
	}
	var rows []ReadRow
	for _, read := range corpus.Reads {
		all := lines(corpus.Files[read.Path])
		end := read.End
		if end <= 0 || end > len(all) {
			end = len(all)
		}
		wanted := strings.Join(all[read.Start-1:end], "\n")
		row := ReadRow{Case: read, FileLines: len(all), Wanted: len(wanted)}
		raw, err := json.Marshal(map[string]any{"path": read.Path, "start_line": read.Start, "end_line": read.End})
		if err != nil {
			return nil, err
		}
		result, err := tool.Run(context.Background(), raw)
		if err != nil {
			return nil, fmt.Errorf("%s read: %w", read.ID, err)
		}
		row.Tofu = readArm(result.Content, wanted)
		sedEnd, headCommand := "$", fmt.Sprintf("tail -n +%d %s", read.Start, quote(read.Path))
		if read.End > 0 {
			sedEnd = fmt.Sprint(read.End)
			headCommand = fmt.Sprintf("head -n %d %s | tail -n +%d", read.End, quote(read.Path), read.Start)
		}
		native := []struct {
			into   *ReadArm
			script string
		}{
			{&row.Cat, "cat " + quote(read.Path)},
			{&row.Sed, fmt.Sprintf("sed -n '%d,%sp' %s", read.Start, sedEnd, quote(read.Path))},
			{&row.Head, headCommand},
		}
		for _, arm := range native {
			output, _, err := shell(scratch, arm.script)
			if err != nil {
				return nil, fmt.Errorf("%s %s: %w", read.ID, arm.script, err)
			}
			*arm.into = readArm(output, wanted)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func readArm(output, wanted string) ReadArm {
	complete := strings.Contains(output, wanted)
	return ReadArm{Returned: len(output), Complete: complete, SecondCall: !complete || len(output) > konst.TurnResultBytesCap}
}

func RunLists(corpus Corpus) ([]ListRow, error) {
	glob, err := tools.NewGlob(corpus.Seed)
	if err != nil {
		return nil, err
	}
	var rows []ListRow
	for _, list := range corpus.Lists {
		raw, err := json.Marshal(map[string]string{"pattern": list.Pattern, "path": list.Path})
		if err != nil {
			return nil, err
		}
		result, err := glob.Run(context.Background(), raw)
		if err != nil {
			return nil, fmt.Errorf("%s glob: %w", list.ID, err)
		}
		var globbed []string
		for _, line := range strings.Split(result.Content, "\n")[1:] {
			if info, err := os.Stat(filepath.Join(corpus.Seed, line)); line != "" && err == nil && !info.IsDir() {
				globbed = append(globbed, line)
			}
		}
		dir, find := findCommand(list.Pattern)
		found, _, err := shell(corpus.Seed, find)
		if err != nil {
			return nil, err
		}
		var finds []string
		for _, line := range strings.Split(strings.TrimSpace(found), "\n") {
			if line != "" {
				finds = append(finds, strings.TrimPrefix(line, "./"))
			}
		}
		listed, _, err := shell(corpus.Seed, "ls -R "+quote(dir))
		if err != nil {
			return nil, err
		}
		var differ []string
		for _, path := range globbed {
			if !slices.Contains(finds, path) {
				differ = append(differ, "glob only: "+path)
			}
		}
		for _, path := range finds {
			if !slices.Contains(globbed, path) {
				differ = append(differ, "find only: "+path)
			}
		}
		rows = append(rows, ListRow{
			Case: list, Find: find, Differ: differ,
			GlobBytes: len(result.Content), GlobPaths: len(globbed),
			FindBytes: len(found), FindPaths: len(finds), LsBytes: len(listed),
		})
	}
	return rows, nil
}

func findCommand(pattern string) (string, string) {
	segments := strings.Split(pattern, "/")
	fixed := 0
	for fixed < len(segments)-1 && !strings.ContainsAny(segments[fixed], "*?[{") {
		fixed++
	}
	dir := "."
	if fixed > 0 {
		dir = strings.Join(segments[:fixed], "/")
	}
	rest := segments[fixed:]
	depth, test := "", "-path "+quote(pattern)
	switch {
	case len(rest) == 1 && fixed > 0:
		depth, test = " -maxdepth 1", nameTest(rest[0])
	case len(rest) == 1:
		test = nameTest(rest[0])
	case len(rest) == 2 && rest[0] == "**":
		test = nameTest(rest[1])
	}
	return dir, "find " + quote(dir) + depth + ` \( -name node_modules -o -name .git \) -prune -o -type f ` + test + " -print"
}

func nameTest(name string) string {
	open, close := strings.Index(name, "{"), strings.Index(name, "}")
	if open < 0 || close < open {
		return "-name " + quote(name)
	}
	var names []string
	for _, choice := range strings.Split(name[open+1:close], ",") {
		names = append(names, "-name "+quote(name[:open]+choice+name[close+1:]))
	}
	return `\( ` + strings.Join(names, " -o ") + ` \)`
}
