package host

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"tofu/internal/konst"
	"tofu/internal/safe"
	"tofu/internal/sys"
)

const (
	HubVerb       = "host"
	HubRunVerb    = "run"
	hubNetwork    = "unix"
	hubSocketDir  = "h"
	hubHostsDir   = "hosts"
	hubShortKey   = 8
	hubAskStatus  = "status"
	hubAskJoin    = "join"
	hubAskStop    = "stop"
	hubAnswerWait = konst.HostAnswerMillis * time.Millisecond
	hubRetry      = konst.HostRetryMillis * time.Millisecond
)

var (
	ErrHubRunning = errors.New("a tofu host already serves this project")
	errNoHub      = errors.New("no tofu host serves this project")
)

type HubStatus struct {
	PID     int       `json:"pid"`
	Socket  string    `json:"socket"`
	Project string    `json:"project"`
	Version string    `json:"version"`
	Members int64     `json:"members"`
	Started time.Time `json:"started"`
}

type hubPlace struct {
	project, socket, lock, log, registry string
}

type hubAsk struct {
	Ask string `json:"ask"`
}

type hub struct {
	status  HubStatus
	members atomic.Int64
	changed chan struct{}
	stopped chan struct{}
	stop    sync.Once
}

func placeOf(project string) (hubPlace, error) {
	abs, err := filepath.Abs(project)
	if err != nil {
		return hubPlace{}, err
	}
	if final, err := filepath.EvalSymlinks(abs); err == nil {
		abs = final
	}
	home, err := sys.HomeConfigDir()
	if err != nil {
		return hubPlace{}, err
	}
	keyed := abs
	if sys.OS() == "windows" || sys.OS() == "darwin" {
		keyed = strings.ToLower(abs)
	}
	sum := sha256.Sum256([]byte(keyed))
	key := hex.EncodeToString(sum[:])
	short := filepath.Join(home, hubSocketDir, key[:hubShortKey])
	return hubPlace{project: abs, socket: short + ".sock", lock: short + ".lock", log: short + ".log", registry: filepath.Join(home, hubHostsDir, key+".json")}, nil
}

func askHub(place hubPlace, ask string) (HubStatus, error) {
	conn, err := net.DialTimeout(hubNetwork, place.socket, hubAnswerWait)
	if err != nil {
		return HubStatus{}, errNoHub
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(hubAnswerWait))
	var status HubStatus
	if err := json.NewEncoder(conn).Encode(hubAsk{Ask: ask}); err != nil {
		return status, err
	}
	return status, json.NewDecoder(conn).Decode(&status)
}

func HubStatusOf(project string) (HubStatus, error) {
	place, err := placeOf(project)
	if err != nil {
		return HubStatus{}, err
	}
	return askHub(place, hubAskStatus)
}

func StopHub(project string) (HubStatus, error) {
	place, err := placeOf(project)
	if err != nil {
		return HubStatus{}, err
	}
	stopped, err := askHub(place, hubAskStop)
	if err != nil {
		return stopped, err
	}
	for deadline := time.Now().Add(konst.HostReadyMillis * time.Millisecond); time.Now().Before(deadline); time.Sleep(hubRetry) {
		if _, err := os.Stat(place.registry); errors.Is(err, os.ErrNotExist) {
			return stopped, nil
		}
	}
	return stopped, fmt.Errorf("the tofu host %d was asked to stop and its registry %s is still there", stopped.PID, place.registry)
}

func EnsureHub(project string) (HubStatus, error) {
	place, err := placeOf(project)
	if err != nil {
		return HubStatus{}, err
	}
	if status, err := askHub(place, hubAskStatus); err == nil {
		return status, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return HubStatus{}, err
	}
	if err := os.MkdirAll(filepath.Dir(place.log), 0o700); err != nil {
		return HubStatus{}, err
	}
	log, err := os.OpenFile(place.log, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return HubStatus{}, err
	}
	defer func() { _ = log.Close() }()
	if err := startDetached(func() *exec.Cmd {
		cmd := exec.Command(exe, HubVerb, HubRunVerb, "--dir", place.project)
		cmd.Dir, cmd.Stdout, cmd.Stderr = place.project, log, log
		return cmd
	}); err != nil {
		return HubStatus{}, err
	}
	ready := konst.HostReadyMillis * time.Millisecond
	for deadline := time.Now().Add(ready); time.Now().Before(deadline); time.Sleep(hubRetry) {
		if status, err := askHub(place, hubAskStatus); err == nil {
			return status, nil
		}
	}
	return HubStatus{}, fmt.Errorf("the tofu host did not answer on %s within %s, its log is %s", place.socket, ready, place.log)
}

func takeHubLock(place hubPlace) (*os.File, error) {
	for deadline := time.Now().Add(konst.HostReadyMillis * time.Millisecond); ; time.Sleep(hubRetry) {
		if file, held, err := lockHub(place.lock); err != nil || !held {
			return file, err
		}
		if _, err := askHub(place, hubAskStatus); err == nil {
			return nil, ErrHubRunning
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("the host lock %s stays held and nothing answers on %s", place.lock, place.socket)
		}
	}
}

func RunHub(project string, report safe.Report, say func(string)) error {
	place, err := placeOf(project)
	if err != nil {
		return err
	}
	for _, dir := range []string{filepath.Dir(place.socket), filepath.Dir(place.registry)} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	if err := os.Chdir(place.project); err != nil {
		return err
	}
	lock, err := takeHubLock(place)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	_ = os.Remove(place.socket)
	listener, err := net.Listen(hubNetwork, place.socket)
	if err != nil {
		return fmt.Errorf("the host socket %s does not open: %w", place.socket, err)
	}
	defer func() {
		_ = listener.Close()
		_ = os.Remove(place.socket)
		_ = os.Remove(place.registry)
	}()
	h := &hub{
		status:  HubStatus{PID: os.Getpid(), Socket: place.socket, Project: place.project, Version: konst.Version, Started: time.Now()},
		changed: make(chan struct{}, 1),
		stopped: make(chan struct{}),
	}
	if err := writeRegistry(place.registry, h.status); err != nil {
		return fmt.Errorf("the host registry %s: %w", place.registry, err)
	}
	safe.Go(report, "host accept", func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			safe.Go(report, "host member", func() { h.serve(conn) })
		}
	})
	say("process " + strconv.Itoa(h.status.PID) + " serves " + place.project + " on " + place.socket)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	say(h.wait(ctx, konst.HostLingerMillis*time.Millisecond))
	return nil
}

func writeRegistry(path string, status HubStatus) error {
	data, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return err
	}
	staged := path + "." + strconv.Itoa(status.PID)
	if err := os.WriteFile(staged, data, 0o600); err != nil {
		return err
	}
	return os.Rename(staged, path)
}

func (h *hub) wait(ctx context.Context, linger time.Duration) string {
	for {
		var alone <-chan time.Time
		if h.members.Load() == 0 {
			alone = time.After(linger)
		}
		select {
		case <-h.changed:
		case <-alone:
			if h.members.Load() == 0 {
				return "no member for " + linger.String() + ", so the host exits"
			}
		case <-h.stopped:
			return "asked to stop, so the host exits"
		case <-ctx.Done():
			return "interrupted, so the host exits"
		}
	}
}

func (h *hub) moved(by int64) {
	h.members.Add(by)
	select {
	case h.changed <- struct{}{}:
	default:
	}
}

func (h *hub) answer(conn net.Conn) error {
	status := h.status
	status.Members = h.members.Load()
	return json.NewEncoder(conn).Encode(status)
}

func (h *hub) serve(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(hubAnswerWait))
	var asked hubAsk
	if json.NewDecoder(conn).Decode(&asked) != nil {
		return
	}
	switch asked.Ask {
	case hubAskStatus:
		_ = h.answer(conn)
	case hubAskStop:
		_ = h.answer(conn)
		h.stop.Do(func() { close(h.stopped) })
	case hubAskJoin:
		h.moved(1)
		defer h.moved(-1)
		if h.answer(conn) != nil {
			return
		}
		_ = conn.SetDeadline(time.Time{})
		_, _ = io.Copy(io.Discard, conn)
	}
}
