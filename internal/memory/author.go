package memory

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/scrypt"

	"tofu/internal/konst"
	"tofu/internal/sys"
)

const (
	UnknownAuthor = "unknown"
	saltFile      = "salt"
	toldFile      = "unknown-author-told"
	ghIDFile      = "gh-id"
)

type who struct {
	identity string
	resolved bool
	authors  map[string]string
}

func (m *Memory) author(root string, writing bool) (string, error) {
	if author, known := m.who.authors[root]; known {
		return author, nil
	}
	if !m.who.resolved {
		m.who.identity, m.who.resolved = m.identity(), true
	}
	if m.who.identity == "" && writing {
		return UnknownAuthor, m.tellUnknownOnce()
	}
	if m.who.identity == "" {
		return UnknownAuthor, nil
	}
	salt, err := saltOf(root, writing)
	if err != nil || salt == nil {
		return "", err
	}
	key, err := scrypt.Key([]byte(m.who.identity), salt, konst.MemoryAuthorScryptCost, konst.MemoryAuthorScryptBlock, konst.MemoryAuthorScryptThreads, konst.MemoryAuthorBytes)
	if err != nil {
		return "", err
	}
	m.who.authors[root] = hex.EncodeToString(key)
	return m.who.authors[root], nil
}

func (m *Memory) identity() string {
	cached := filepath.Join(m.home, dirName, ghIDFile)
	if kept, err := os.Stat(cached); err == nil && time.Since(kept.ModTime()) < konst.MemoryGhIDKeptHours*time.Hour {
		if id, err := os.ReadFile(cached); err == nil && len(bytes.TrimSpace(id)) > 0 {
			return "gh:" + string(bytes.TrimSpace(id))
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), konst.MemoryIdentityTimeoutMillis*time.Millisecond)
	defer cancel()
	if !testing.Testing() {
		id, err := exec.CommandContext(ctx, "gh", "api", "user", "--jq", ".id").Output()
		if id = bytes.TrimSpace(id); err == nil && len(id) > 0 {
			_ = sys.WriteFile(cached, id, 0o600)
			return "gh:" + string(id)
		}
	}
	email, err := exec.CommandContext(ctx, "git", "-C", m.project, "config", "user.email").Output()
	if err != nil || len(bytes.TrimSpace(email)) == 0 {
		return ""
	}
	return "email:" + strings.ToLower(string(bytes.TrimSpace(email)))
}

func saltOf(root string, writing bool) ([]byte, error) {
	file := filepath.Join(root, dirName, saltFile)
	for {
		kept, err := os.ReadFile(file)
		if err == nil {
			return hex.DecodeString(strings.TrimSpace(string(kept)))
		}
		if !errors.Is(err, os.ErrNotExist) || !writing {
			return nil, nil
		}
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			return nil, err
		}
		fresh, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		salt := make([]byte, konst.MemorySaltBytes)
		_, _ = rand.Read(salt)
		_, err = fresh.WriteString(hex.EncodeToString(salt) + "\n")
		return salt, errors.Join(err, fresh.Close())
	}
}

func (m *Memory) tellUnknownOnce() error {
	told := filepath.Join(m.home, dirName, toldFile)
	if _, err := os.Stat(told); !errors.Is(err, os.ErrNotExist) {
		return nil
	}
	m.Notices = append(m.Notices, "no gh login and no git user.email here, so memory is written by author "+UnknownAuthor+"; set git config user.email to own what you write. Said once")
	return sys.WriteFile(told, nil, 0o644)
}
