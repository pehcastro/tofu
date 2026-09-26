package keymap

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

type State string

const (
	Missing           State = "keymap missing"
	NotConfigured     State = "not configured"
	Managed           State = "managed"
	NeedsUpdate       State = "needs update"
	LegacyOnly        State = "legacy Ctrl+K only; update recommended"
	Conflicting       State = "conflict"
	AlreadyConfigured State = "already configured"
)

const (
	fence          = "// tofu-keybindings-v2:"
	editedRefusal  = "tofu-managed keybindings were edited; refusing automatic change"
	nothingManaged = "nothing managed by tofu to remove"
)

func leadingComma(comma bool) string {
	if comma {
		return ","
	}
	return ""
}

func managedBlock(tag, body string, comma bool) []byte {
	return fmt.Appendf(nil, "%s\n  %sbegin:%s:%x\n%s\n  %send:%s\n", leadingComma(comma), fence, tag, sha256.Sum256([]byte(body)), body, fence, tag)
}

func findManaged(source []byte, tag string) (start, end int, err error) {
	begin, finish := fence+"begin:"+tag+":", fence+"end:"+tag
	if !bytes.Contains(source, []byte(begin)) && !bytes.Contains(source, []byte(finish)) {
		return -1, -1, nil
	}
	pattern := regexp.MustCompile(`(?s),?\n  ` + regexp.QuoteMeta(begin) + `([0-9a-f]{64})\n(.*?)\n  ` + regexp.QuoteMeta(finish) + `\n`)
	match := pattern.FindSubmatchIndex(source)
	if match == nil || string(source[match[2]:match[3]]) != fmt.Sprintf("%x", sha256.Sum256(source[match[4]:match[5]])) {
		return -1, -1, errors.New(editedRefusal)
	}
	return match[0], match[1], nil
}

func inspectManaged(path, tag string, elements []string) (State, error) {
	source, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Missing, nil
	}
	if err != nil {
		return "", err
	}
	if _, err := parseJSONC(source); err != nil {
		return "", err
	}
	start, _, err := findManaged(source, tag)
	if err != nil {
		return "", err
	}
	if start < 0 {
		return NotConfigured, nil
	}
	body := strings.Join(elements, ",\n")
	if bytes.Contains(source, managedBlock(tag, body, true)) || bytes.Contains(source, managedBlock(tag, body, false)) {
		return Managed, nil
	}
	return NeedsUpdate, nil
}

func applyManaged(path, tag string, elements []string) (status, backup string, err error) {
	if len(elements) == 0 {
		return "", "", errors.New("no supported tofu shortcuts to apply")
	}
	source, err := os.ReadFile(path)
	created := errors.Is(err, os.ErrNotExist)
	if created {
		source = []byte("[]\n")
	} else if err != nil {
		return "", "", err
	}
	start, end, err := findManaged(source, tag)
	if err != nil {
		return "", "", err
	}
	base := source
	if start >= 0 {
		base = slices.Concat(source[:start], source[end:])
	}
	array, err := parseJSONC(base)
	if err != nil {
		return "", "", err
	}
	commas := []bool{false, true}
	if len(array.entries) > 0 {
		commas = []bool{true, false}
	}
	body := strings.Join(elements, ",\n")
	for _, comma := range commas {
		result := slices.Concat(base[:array.close], managedBlock(tag, body, comma), base[array.close:])
		if _, err := parseJSONC(result); err != nil {
			continue
		}
		if bytes.Equal(source, result) {
			return "managed; no change", "", nil
		}
		backup, err := writeManaged(path, source, result, created)
		if err != nil {
			return "", backup, err
		}
		return "installed terminal-only tofu shortcuts", backup, nil
	}
	return "", "", errors.New("could not append valid keybindings to host JSONC array")
}

func undoManaged(path, tag string) (status, backup string, err error) {
	source, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nothingManaged, "", nil
	}
	if err != nil {
		return "", "", err
	}
	start, end, err := findManaged(source, tag)
	if err != nil {
		return "", "", err
	}
	if start < 0 {
		return nothingManaged, "", nil
	}
	result := slices.Concat(source[:start], source[end:])
	if _, err := parseJSONC(result); err != nil {
		return "", "", err
	}
	backup, err = writeManaged(path, source, result, false)
	if err != nil {
		return "", backup, err
	}
	return "removed tofu terminal shortcuts", backup, nil
}

func writeTemp(dir, pattern string, data []byte, mode os.FileMode) (string, error) {
	file, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", err
	}
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Chmod(file.Name(), mode)
	}
	return file.Name(), err
}

func writeManaged(path string, source, result []byte, created bool) (backup string, err error) {
	dir, name := filepath.Dir(path), filepath.Base(path)
	mode := os.FileMode(0o600)
	if created {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", err
		}
	} else {
		info, err := os.Stat(path)
		if err != nil {
			return "", err
		}
		mode = info.Mode().Perm()
		if backup, err = writeTemp(dir, name+".tofu-backup-*", source, mode); err != nil {
			return backup, err
		}
	}
	temporary, err := writeTemp(dir, name+".tofu-write-*", result, mode)
	defer func() { _ = os.Remove(temporary) }()
	if err != nil {
		return backup, err
	}
	current, err := os.ReadFile(path)
	switch {
	case created && err == nil:
		return backup, errors.New("host keymap appeared while applying; original preserved")
	case created && !errors.Is(err, os.ErrNotExist), !created && err != nil:
		return backup, err
	case !created && !bytes.Equal(current, source):
		return backup, errors.New("host keymap changed while applying; original preserved")
	}
	if err := os.Rename(temporary, path); err != nil {
		return backup, fmt.Errorf("atomic keymap replacement failed (original preserved): %w", err)
	}
	return backup, nil
}
