package shell

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"tofu/internal/konst"
	"tofu/internal/widget"
)

func (r *Registry) Tail(name string, lines int) (string, error) {
	entry, err := r.Read(name)
	if err != nil {
		return "", err
	}
	own, _ := readEnd(r.logPath(name), time.Time{}, konst.ShellTailBytes)
	parts := []string{heldBy(entry.Command, entry.Terminal), lastLines(own, lines)}
	for _, path := range namedLogs(entry.Dir, entry.Command) {
		written, _ := readEnd(path, entry.Started, konst.ShellNamedLogBytes)
		if named := lastLines(written, lines); named != "" {
			parts = append(parts, path+", which the command writes to, ends:\n"+named)
		}
	}
	if entry.Deadline > 0 {
		parts = append(parts, "tofu: "+HitDeadline(time.Duration(entry.Deadline)*time.Millisecond)+" and was killed")
	}
	return strings.TrimLeft(strings.Join(slices.DeleteFunc(parts, func(part string) bool { return part == "" }), "\n\n"), "\n"), nil
}

type Timing struct {
	Ran   time.Duration
	Last  time.Time
	Ended time.Time
}

func (r *Registry) Timing(entry Shell, now time.Time) Timing {
	var timing Timing
	if entry.Ended != nil {
		now, timing.Ended = *entry.Ended, *entry.Ended
	}
	timing.Ran = now.Sub(entry.Started)
	for _, path := range append(namedLogs(entry.Dir, entry.Command), r.logPath(entry.Name)) {
		if _, at := readEnd(path, entry.Started, 0); at.After(timing.Last) {
			timing.Last = at
		}
	}
	return timing
}

func (t Timing) Words(now time.Time) string {
	ran := "ran " + widget.Until(t.Ran)
	switch {
	case t.Last.IsZero():
		return ran + ", no output"
	case !t.Ended.IsZero():
		return ran + ", last output " + widget.Until(t.Ended.Sub(t.Last)) + " before it ended"
	}
	return ran + ", last output " + widget.Until(now.Sub(t.Last)) + " ago"
}

type Cursor struct {
	logs    map[string]*followed
	from    string
	noted   bool
	midLine bool
}

type followed struct {
	offset int64
	head   []byte
}

func (r *Registry) Follow(name string, cursor *Cursor) (string, error) {
	entry, err := r.Read(name)
	if err != nil {
		return "", err
	}
	if cursor.logs == nil {
		cursor.logs = map[string]*followed{}
	}
	var out strings.Builder
	if note := heldBy(entry.Command, entry.Terminal); !cursor.noted && note != "" {
		out.WriteString(note + "\n")
	}
	cursor.noted = true
	own := r.logPath(name)
	for _, path := range append([]string{own}, namedLogs(entry.Dir, entry.Command)...) {
		at := cursor.logs[path]
		if at == nil {
			at = &followed{}
			cursor.logs[path] = at
		}
		raw := at.next(path, entry.Started, entry.State == Running)
		if len(raw) == 0 {
			continue
		}
		if path != cursor.from && (cursor.from != "" || path != own) {
			if cursor.midLine {
				out.WriteString("\n")
			}
			label, err := filepath.Rel(entry.Dir, path)
			switch {
			case path == own:
				label = "the command"
			case err != nil || strings.HasPrefix(label, ".."):
				label = path
			}
			out.WriteString("tofu: from " + label + "\n")
		}
		text := Decode(raw)
		out.WriteString(text)
		cursor.from, cursor.midLine = path, !strings.HasSuffix(text, "\n")
	}
	return out.String(), nil
}

func (f *followed) next(path string, since time.Time, running bool) []byte {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil || info.IsDir() || info.ModTime().Before(since.Add(-konst.ShellLogClockSlackMillis*time.Millisecond)) {
		return nil
	}
	head := make([]byte, len(f.head))
	n, _ := file.ReadAt(head, 0)
	if info.Size() < f.offset || !bytes.Equal(head[:n], f.head) {
		f.offset, f.head = 0, nil
	}
	raw := make([]byte, info.Size()-f.offset)
	n, _ = file.ReadAt(raw, f.offset)
	raw = raw[:n]
	if running {
		raw = raw[:wholeRunes(raw)]
	}
	f.offset += int64(len(raw))
	f.head = append(f.head, raw[:min(len(raw), konst.ShellRewriteHeadBytes-len(f.head))]...)
	return raw
}

func wholeRunes(raw []byte) int {
	for at := len(raw) - 1; at >= max(0, len(raw)-utf8.UTFMax); at-- {
		if utf8.RuneStart(raw[at]) {
			if utf8.FullRune(raw[at:]) {
				return len(raw)
			}
			return at
		}
	}
	return len(raw)
}

func lastLines(raw []byte, lines int) string {
	if strings.TrimSpace(string(raw)) == "" {
		return ""
	}
	all := strings.Split(strings.TrimRight(Decode(raw), "\r\n"), "\n")
	return strings.Join(all[max(0, len(all)-lines):], "\n")
}

func readEnd(path string, since time.Time, limit int64) ([]byte, time.Time) {
	file, err := os.Open(path)
	if err != nil {
		return nil, time.Time{}
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil || info.IsDir() || info.Size() == 0 || info.ModTime().Before(since.Add(-konst.ShellLogClockSlackMillis*time.Millisecond)) {
		return nil, time.Time{}
	}
	raw := make([]byte, min(info.Size(), limit))
	n, _ := file.ReadAt(raw, info.Size()-int64(len(raw)))
	raw = raw[:n]
	if int64(n) < info.Size() {
		raw = raw[bytes.IndexByte(raw, '\n')+1:]
	}
	return raw, info.ModTime()
}

func namedLogs(dir, command string) []string {
	var paths []string
	for _, step := range regexp.MustCompile(`&&|\|\||;`).Split(command, -1) {
		if moved := regexp.MustCompile(`(?i)^\s*(?:cd|pushd|set-location)\s+(?:/d\s+)?["']?([^"']+?)["']?\s*$`).FindStringSubmatch(step); moved != nil {
			dir = within(dir, moved[1])
			continue
		}
		for _, match := range regexp.MustCompile(`(?i)(?:(?:^|\s)--?log(?:-?file)?[=\s]+|(?:^|[^<>=-])[12&*]?>>?\s*|\btee\s+(?:-a\s+)?|\bout-file\s+(?:-filepath\s+)?|-RedirectStandard(?:Output|Error)\s+)["']?([^\s"'|;&<>()]+)`).FindAllStringSubmatch(step, -1) {
			if slices.Contains([]string{"/dev/null", "$null", "nul"}, strings.ToLower(match[1])) {
				continue
			}
			if path := within(dir, match[1]); !slices.Contains(paths, path) {
				paths = append(paths, path)
			}
		}
	}
	return paths
}

func within(dir, path string) string {
	if drive := regexp.MustCompile(`^/([a-zA-Z])(?:/|$)`).FindStringSubmatch(path); drive != nil && runtime.GOOS == "windows" {
		path = strings.ToUpper(drive[1]) + ":/" + path[len(drive[0]):]
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(dir, path)
}
