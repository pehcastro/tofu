package shell

import (
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"tofu/internal/konst"
	"tofu/internal/widget"
)

var (
	commandStep = regexp.MustCompile(`&&|\|\||;`)
	changedDir  = regexp.MustCompile(`(?i)^\s*(?:cd|pushd|set-location)\s+(?:/d\s+)?["']?([^"']+?)["']?\s*$`)
	logTarget   = regexp.MustCompile(`(?i)(?:(?:^|\s)--?log(?:-?file)?[=\s]+|(?:^|[^<>=-])[12&*]?>>?\s*|\btee\s+(?:-a\s+)?|\bout-file\s+(?:-filepath\s+)?|-RedirectStandard(?:Output|Error)\s+)["']?([^\s"'|;&<>()]+)`)
	msysDrive   = regexp.MustCompile(`^/([a-zA-Z])(?:/|$)`)
)

type Tails struct {
	mu     sync.Mutex
	shells map[string]*tailed
}

type tailed struct {
	key  tailKey
	logs []logEnd
	text string
}

type tailKey struct {
	command, dir      string
	terminal          bool
	started, deadline int64
	lines             int
}

type logEnd struct {
	path   string
	since  time.Time
	limit  int64
	size   int64
	at     time.Time
	window []byte
}

func (r *Registry) Tail(name string, lines int) (string, error) {
	return new(Tails).Tail(r, name, lines)
}

func (t *Tails) Tail(r *Registry, name string, lines int) (string, error) {
	entry, err := r.Read(name)
	if err != nil {
		return "", err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.shells == nil {
		t.shells = map[string]*tailed{}
	}
	key := tailKey{command: entry.Command, dir: entry.Dir, terminal: entry.Terminal, started: entry.Started.UnixNano(), deadline: entry.Deadline, lines: lines}
	held := t.shells[name]
	moved := held == nil || held.key != key
	if moved {
		held = &tailed{key: key, logs: []logEnd{{path: r.logPath(name), limit: konst.ShellTailBytes}}}
		for _, path := range namedLogs(entry.Dir, entry.Command) {
			held.logs = append(held.logs, logEnd{path: path, since: entry.Started, limit: konst.ShellNamedLogBytes})
		}
		t.shells[name] = held
	}
	for at := range held.logs {
		moved = held.logs[at].refresh() || moved
	}
	if !moved {
		return held.text, nil
	}
	parts := []string{heldBy(entry.Command, entry.Terminal), lastLines(held.logs[0].tail(), lines)}
	for _, log := range held.logs[1:] {
		if named := lastLines(log.tail(), lines); named != "" {
			parts = append(parts, log.path+", which the command writes to, ends:\n"+named)
		}
	}
	if entry.Deadline > 0 {
		parts = append(parts, "tofu: "+HitDeadline(time.Duration(entry.Deadline)*time.Millisecond)+" and was killed")
	}
	held.text = strings.TrimLeft(strings.Join(slices.DeleteFunc(parts, func(part string) bool { return part == "" }), "\n\n"), "\n")
	return held.text, nil
}

func (t *Tails) Forget(listed []Shell) {
	t.mu.Lock()
	defer t.mu.Unlock()
	maps.DeleteFunc(t.shells, func(name string, _ *tailed) bool {
		return !slices.ContainsFunc(listed, func(one Shell) bool { return one.Name == name })
	})
}

func (l *logEnd) refresh() bool {
	info, err := os.Stat(l.path)
	if err != nil || !written(info, l.since) {
		moved := l.size > 0
		l.size, l.at, l.window = 0, time.Time{}, nil
		return moved
	}
	if info.Size() == l.size && info.ModTime().Equal(l.at) {
		return false
	}
	file, err := os.Open(l.path)
	if err != nil {
		return false
	}
	defer func() { _ = file.Close() }()
	overlap := int64(min(len(l.window), konst.ShellRewriteHeadBytes))
	from := l.size - overlap
	if l.size == 0 || info.Size() < l.size || info.Size()-l.size > l.limit {
		from, overlap, l.window = max(0, info.Size()-l.limit), 0, nil
	}
	raw := make([]byte, info.Size()-from)
	n, _ := file.ReadAt(raw, from)
	if int64(n) < overlap || !bytes.Equal(raw[:overlap], l.window[int64(len(l.window))-overlap:]) {
		l.size, l.window = 0, nil
		return l.refresh()
	}
	l.window = append(l.window, raw[overlap:n]...)
	l.window = l.window[max(0, int64(len(l.window))-l.limit):]
	l.size, l.at = from+int64(n), info.ModTime()
	return true
}

func (l *logEnd) tail() []byte {
	if int64(len(l.window)) < l.size {
		return l.window[bytes.IndexByte(l.window, '\n')+1:]
	}
	return l.window
}

func written(info os.FileInfo, since time.Time) bool {
	return !info.IsDir() && info.Size() > 0 && !info.ModTime().Before(since.Add(-konst.ShellLogClockSlackMillis*time.Millisecond))
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
		if info, err := os.Stat(path); err == nil && written(info, entry.Started) && info.ModTime().After(timing.Last) {
			timing.Last = info.ModTime()
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

func namedLogs(dir, command string) []string {
	var paths []string
	for _, step := range commandStep.Split(command, -1) {
		if moved := changedDir.FindStringSubmatch(step); moved != nil {
			dir = within(dir, moved[1])
			continue
		}
		for _, match := range logTarget.FindAllStringSubmatch(step, -1) {
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
	if drive := msysDrive.FindStringSubmatch(path); drive != nil && runtime.GOOS == "windows" {
		path = strings.ToUpper(drive[1]) + ":/" + path[len(drive[0]):]
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(dir, path)
}
