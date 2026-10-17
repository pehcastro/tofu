package host

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"tofu/internal/konst"
	"tofu/internal/session"
)

const (
	HolderVerb     = "holder"
	HolderVariable = "TOFU_HOLDER"
	holderLog      = "holder.log"
)

var (
	errNoHolder     = errors.New("the holder answered and holds no lock on the session")
	errNotHolding   = errors.New("this tofu does not hold the session, another one does")
	errHolderSilent = errors.New("the tofu holding the session did not answer, so nothing changed")
)

func startHolder(dir string, store *session.Store, id string) error {
	path := store.Channel(id)
	if answers(path) {
		return nil
	}
	if store.Busy(id) == nil {
		_ = os.Remove(path)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(store.Dir(id), 0o755); err != nil {
		return err
	}
	logPath := filepath.Join(store.Dir(id), holderLog)
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = log.Close() }()
	if err := startDetached(func() *exec.Cmd {
		cmd := exec.Command(exe, HolderVerb, "--dir", dir, "--session", id)
		cmd.Dir, cmd.Stdout, cmd.Stderr = dir, log, log
		return cmd
	}); err != nil {
		return err
	}
	wait := konst.HolderReadyMillis * time.Millisecond
	for deadline := time.Now().Add(wait); time.Now().Before(deadline); time.Sleep(followRetry) {
		if answers(path) {
			return nil
		}
	}
	return fmt.Errorf("the holder of session %s did not answer within %s, its log is %s", id, wait, logPath)
}

func answers(path string) bool {
	conn, err := net.DialTimeout(followNetwork, path, konst.HolderProbeMillis*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func (h *Host) HoldWhileAttached(grace time.Duration) error {
	a := h.audience.Load()
	if a == nil {
		return errNotHolding
	}
	for {
		var alone <-chan time.Time
		if a.attachedNow() == 0 {
			alone = time.After(grace)
		}
		select {
		case <-a.changed:
		case <-alone:
			if a.attachedNow() == 0 {
				return nil
			}
		}
	}
}
