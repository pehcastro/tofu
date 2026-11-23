package host

import (
	"cmp"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"tofu/internal/agent/loop"
	"tofu/internal/cli/render"
	"tofu/internal/konst"
	"tofu/internal/models/llm/core"
	"tofu/internal/platform/safe"
	"tofu/internal/store/session"
)

const (
	alivePrefix        = "host-"
	aliveSuffix        = ".alive"
	crashSuffix        = ".crash"
	crashFileVariable  = "TOFU_HOST_CRASH_FILE"
	fatalAfterVariable = "TOFU_HOST_FATAL_AFTER"
)

type aliveRecord struct {
	PID     int       `json:"pid"`
	AliveAt time.Time `json:"alive_at"`
	Running []string  `json:"running,omitempty"`
	Crash   string    `json:"crash,omitempty"`
}

type heartbeat struct {
	mu      sync.Mutex
	path    string
	stopped bool
	quit    chan struct{}
}

func processStamp() string {
	return strconv.Itoa(os.Getpid()) + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
}

func (h *Host) beat(dir string) *heartbeat {
	b := &heartbeat{path: filepath.Join(dir, alivePrefix+processStamp()+aliveSuffix), quit: make(chan struct{})}
	safe.Go(h.report, "host alive", func() {
		every := time.NewTicker(konst.HostAliveMillis * time.Millisecond)
		defer every.Stop()
		failed := false
		for {
			err := b.write(h.runningNow())
			if err != nil && !failed {
				h.trace("the liveness record "+b.path+" was not written, so if this host stops the next open cannot say why: "+err.Error(), nil)
			}
			failed = err != nil
			select {
			case <-b.quit:
				return
			case <-every.C:
			}
		}
	})
	return b
}

func (h *Host) runningNow() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	running := h.inbox.Running()
	if h.running {
		running = append([]string{"the lead"}, running...)
	}
	return running
}

func (b *heartbeat) write(running []string) error {
	data, err := json.Marshal(aliveRecord{PID: os.Getpid(), AliveAt: time.Now(), Running: running, Crash: os.Getenv(crashFileVariable)})
	if err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.stopped {
		return nil
	}
	staged := b.path + ".new"
	if err := os.WriteFile(staged, data, 0o600); err != nil {
		return err
	}
	return os.Rename(staged, b.path)
}

func (b *heartbeat) stop() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stopped = true
	close(b.quit)
	_ = os.Remove(b.path)
}

func (h *Host) traceUnseenEnds(store *session.Store, id string) error {
	if errors.As(store.Busy(id), &session.BusyError{}) {
		return nil
	}
	found, err := filepath.Glob(filepath.Join(store.Dir(id), alivePrefix+"*"+aliveSuffix))
	if err != nil {
		return err
	}
	var failed []error
	for _, path := range found {
		if h.alive != nil && path == h.alive.path {
			continue
		}
		stop, err := unseenEnd(store, id, path)
		if err == nil {
			notice := session.StopBody{NoticeBody: session.NoticeBody{Text: stop.Said(), From: loop.RecordedOrigin(core.Origin{Source: core.SourceNotice})}, Stop: &stop}
			err = appendNotice(h.dir, id, session.Event{Kind: session.EventNotice}, notice)
		}
		if err == nil {
			err = os.Remove(path)
		}
		failed = append(failed, err)
	}
	store.Unread(id)
	return errors.Join(failed...)
}

func unseenEnd(store *session.Store, id, path string) (session.Stop, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return session.Stop{}, err
	}
	var record aliveRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return session.Stop{}, errors.New("the liveness record " + path + " did not read: " + err.Error())
	}
	var last time.Time
	if err := store.EachEventHead(id, func(head session.EventHead) {
		if head.At.After(last) {
			last = head.At
		}
	}); err != nil {
		return session.Stop{}, err
	}
	why := crashWords(record.Crash)
	switch {
	case why != "":
	case len(record.Running) > 0 && record.AliveAt.Sub(last) > konst.HostStalledMillis*time.Millisecond:
		why = "was alive but made no progress since " + last.Local().Format(time.DateTime)
	default:
		why = "ended with no crash record"
	}
	seen := last
	if record.AliveAt.After(seen) {
		seen = record.AliveAt
	}
	why += "; last event " + last.Local().Format(time.DateTime) + ", last alive " + record.AliveAt.Local().Format(time.DateTime) + ", " + render.Until(time.Since(seen)) + " before this open"
	return session.Stop{Kind: loop.HostEnded, By: "process " + strconv.Itoa(record.PID), Why: why, Took: record.Running}, nil
}

func crashWords(path string) string {
	if path == "" {
		return ""
	}
	crash, err := os.ReadFile(path)
	if err != nil {
		return "its crash output " + path + " did not read: " + err.Error()
	}
	first := ""
	for line := range strings.Lines(string(crash)) {
		said := strings.TrimSpace(line)
		if goCrashLine(said) {
			return "crashed: " + strconv.Quote(said)
		}
		first = cmp.Or(first, said)
	}
	if first == "" {
		return ""
	}
	return "ended with no crash record; its error output begins " + strconv.Quote(first)
}

func goCrashLine(line string) bool {
	for _, header := range []string{"fatal error: ", "panic: ", "runtime: ", "fatal: ", "Exception 0x", "unexpected fault address", "SIG"} {
		if strings.HasPrefix(line, header) {
			return true
		}
	}
	return false
}

func keepCrashOutput(place hubPlace, say func(string)) (release func()) {
	path := strings.TrimSuffix(place.lock, hubLockSuffix) + "." + processStamp() + crashSuffix
	file, err := os.Create(path)
	restore := func() {}
	if err == nil {
		var into func()
		if into, err = errorOutputInto(file); err == nil {
			restore = into
		}
	}
	if err == nil {
		err = os.Setenv(crashFileVariable, path)
	}
	if err != nil {
		say("the error output of this host is not kept, so a fatal error would end it unexplained: " + err.Error())
	}
	return func() {
		restore()
		if file != nil {
			_ = file.Close()
			_ = os.Remove(path)
		}
	}
}

func fatalAfter(seam string) error {
	after, err := time.ParseDuration(seam)
	if err != nil {
		return errors.New(fatalAfterVariable + "=" + seam + ": " + err.Error())
	}
	time.AfterFunc(after, func() {
		var never sync.Mutex
		never.Unlock()
	})
	return nil
}
