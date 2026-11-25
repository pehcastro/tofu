package process

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"tofu/internal/cli/render"
	"tofu/internal/konst"
	"tofu/internal/platform/fs"
)

const tofuSays = "tofu: "

var (
	failureNamed  = fs.LazyRegexp(`^(?:error(?:\[\w+\])?: |--- FAIL|FAIL(?:ED)?\b|\[FAIL\]|panic: |fatal: |\S+\.go:\d+:\d+: |test \S+ \.\.\. FAILED$|test result: FAILED|thread '.+' panicked at |.*\berror TS\d+|Traceback |E {3}|\w+Error: )`)
	failureWorded = fs.LazyRegexp(`(?i)(?:^|[\s:\[(])(?:errors?|failed|failure|fatal|panic(?:ked)?|exception|traceback)(?:[:\s\])]|$)|\w(?:Error|Exception|NotFound)\b`)
	noFailure     = fs.LazyRegexp(`(?i)\b(?:0|no) (?:errors?|failed|failures?)\b`)
)

type Failure int

const (
	FailureNone Failure = iota
	FailureWorded
	FailureNamed
)

func FailureIn(line string) Failure {
	switch {
	case failureNamed().MatchString(line):
		return FailureNamed
	case failureWorded().MatchString(line) && !noFailure().MatchString(line):
		return FailureWorded
	}
	return FailureNone
}

type Watcher struct {
	cursor  Cursor
	partial string
	from    string
}

type News struct {
	Lines   int
	Trouble []string
}

func (r *Registry) Watch(name string, w *Watcher) (News, error) {
	text, err := r.Follow(name, &w.cursor)
	if err != nil {
		return News{}, err
	}
	lines := strings.Split(w.partial+text, "\n")
	w.partial = lines[len(lines)-1]
	var news News
	for _, line := range lines[:len(lines)-1] {
		line = strings.TrimSpace(line[strings.LastIndexByte(strings.TrimRight(line, "\r"), '\r')+1:])
		if said, ours := strings.CutPrefix(line, tofuSays); ours {
			if from, header := strings.CutPrefix(said, "from "); header {
				w.from = from
			}
			continue
		}
		news.Lines++
		if FailureIn(line) != FailureNone && len(news.Trouble) < konst.TestErrorLines {
			news.Trouble = append(news.Trouble, cmp.Or(w.from, "the command")+": "+line)
		}
	}
	return news, nil
}

func (r *Registry) Tell(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	process := r.running[name]
	if process == nil || process.told {
		return false
	}
	process.told = true
	return true
}

func (s Shell) TreeCPU() (time.Duration, error) { return treeCPU(s.PID) }

func (s Shell) Redirects(now time.Time) []string {
	var said []string
	for _, path := range namedLogs(s) {
		info, err := os.Stat(path)
		switch {
		case errors.Is(err, os.ErrNotExist):
			said = append(said, "it writes to "+path+", which does not exist yet")
		case err != nil:
			said = append(said, "it writes to "+path+", which tofu cannot read: "+err.Error())
		default:
			said = append(said, fmt.Sprintf("it writes to %s, %d bytes, last written %s ago", path, info.Size(), render.Until(now.Sub(info.ModTime()))))
		}
	}
	return said
}
