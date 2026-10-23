package turn

import (
	"context"
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"

	"tofu/internal/konst"
)

type lineSpan struct{ first, last int }

type readMark struct {
	hash  [sha256.Size]byte
	spans []lineSpan
}

type ReadLedger struct {
	mutex   sync.Mutex
	byAgent map[string]map[string]readMark
}

func NewReadLedger() *ReadLedger {
	return &ReadLedger{byAgent: map[string]map[string]readMark{}}
}

func (l *ReadLedger) seen(ctx context.Context) map[string]readMark {
	agent := SubAgentAsking(ctx)
	if l.byAgent[agent] == nil {
		l.byAgent[agent] = map[string]readMark{}
	}
	return l.byAgent[agent]
}

func ledgerKey(path string) string {
	return filepath.ToSlash(filepath.Clean(path))
}

func readKey(path string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(ledgerKey(path))
	}
	return ledgerKey(path)
}

func lineCount(body []byte) int {
	return strings.Count(strings.TrimSuffix(string(body), "\n"), "\n") + 1
}

func (l *ReadLedger) Mark(ctx context.Context, path string, body []byte) {
	l.MarkLines(ctx, path, body, 1, lineCount(body))
}

func (l *ReadLedger) MarkLines(ctx context.Context, path string, body []byte, first, last int) {
	if l == nil {
		return
	}
	l.mutex.Lock()
	defer l.mutex.Unlock()
	seen, key, hash := l.seen(ctx), readKey(path), sha256.Sum256(body)
	mark := seen[key]
	if mark.hash != hash {
		mark = readMark{hash: hash}
	}
	mark.spans = merged(append(mark.spans, lineSpan{first, last}))
	seen[key] = mark
}

func (l *ReadLedger) Saw(ctx context.Context, path string, body []byte) bool {
	if l == nil {
		return true
	}
	l.mutex.Lock()
	defer l.mutex.Unlock()
	return l.seen(ctx)[readKey(path)].hash == sha256.Sum256(body)
}

func (l *ReadLedger) Known(ctx context.Context, path string) bool {
	if l == nil {
		return true
	}
	l.mutex.Lock()
	defer l.mutex.Unlock()
	_, known := l.seen(ctx)[readKey(path)]
	return known
}

type LineChange struct{ first, oldLast, newLast int }

func ChangeOf(before, after string) LineChange {
	old, becomes := strings.Split(before, "\n"), strings.Split(after, "\n")
	same := 0
	for same < min(len(old), len(becomes)) && old[same] == becomes[same] {
		same++
	}
	tail := 0
	for tail < min(len(old), len(becomes))-same && old[len(old)-1-tail] == becomes[len(becomes)-1-tail] {
		tail++
	}
	return LineChange{first: same + 1, oldLast: len(old) - tail, newLast: len(becomes) - tail}
}

func (c LineChange) reach(lines int) (int, int) {
	first := min(c.first, lines)
	return first, min(max(c.oldLast, first), lines)
}

func (l *ReadLedger) Unshown(ctx context.Context, path string, body []byte, change LineChange) error {
	if l == nil {
		return nil
	}
	l.mutex.Lock()
	defer l.mutex.Unlock()
	first, last := change.reach(lineCount(body))
	above, inserted := change.first-1, change.oldLast < change.first
	spans := l.seen(ctx)[readKey(path)].spans
	shown := make([]string, len(spans))
	for i, span := range spans {
		if span.first <= first && last <= span.last || inserted && span.first <= above && above <= span.last {
			return nil
		}
		shown[i] = fmt.Sprintf("%d-%d", span.first, span.last)
	}
	reach := fmt.Sprintf("this change reaches lines %d-%d, which no read showed", first, last)
	if inserted {
		reach, first = fmt.Sprintf("this insertion lands after line %d, and no read showed that line or the one below it", above), max(above, 1)
	}
	return fmt.Errorf("%s was read only at lines %s, and %s: read lines %d to %d with start_line and end_line, or read the whole file, then try again",
		path, strings.Join(shown, ", "), reach, first, last)
}

func (l *ReadLedger) Rewrote(ctx context.Context, path string, after []byte, change LineChange) {
	if l == nil {
		return
	}
	l.mutex.Lock()
	defer l.mutex.Unlock()
	seen, key, shift := l.seen(ctx), readKey(path), change.newLast-change.oldLast
	var spans []lineSpan
	for _, span := range seen[key].spans {
		if span.first < change.first {
			spans = append(spans, lineSpan{span.first, min(span.last, change.first-1)})
		}
		if span.last > change.oldLast {
			spans = append(spans, lineSpan{max(span.first, change.oldLast+1) + shift, span.last + shift})
		}
	}
	if change.newLast >= change.first {
		spans = append(spans, lineSpan{change.first, change.newLast})
	}
	seen[key] = readMark{hash: sha256.Sum256(after), spans: merged(spans)}
}

func merged(spans []lineSpan) []lineSpan {
	slices.SortFunc(spans, func(a, b lineSpan) int { return a.first - b.first })
	var joined []lineSpan
	for _, span := range spans {
		if n := len(joined); n > 0 && span.first <= joined[n-1].last+1 {
			joined[n-1].last = max(joined[n-1].last, span.last)
			continue
		}
		joined = append(joined, span)
	}
	return joined
}

func (l *ReadLedger) Refuse(ctx context.Context, path string, body []byte, change LineChange) string {
	if len(body) <= konst.RefusalCarriesWholeUpToBytes {
		l.Mark(ctx, path, body)
		return "its current content follows, so try again against the text that is there.\n" + string(body)
	}
	lines := lineCount(body)
	if change == (LineChange{}) {
		return fmt.Sprintf("it has %d lines, too many to carry here: read it whole, or the lines the change is meant for with start_line and end_line, then try again", lines)
	}
	first, last := change.reach(lines)
	return fmt.Sprintf("it has %d lines, and this change reaches lines %d-%d: read them with read {\"path\": %q, \"start_line\": %d, \"end_line\": %d}, or read the whole file, then try again",
		lines, first, last, path, first, last)
}
