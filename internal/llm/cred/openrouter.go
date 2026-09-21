package cred

import (
	"errors"
	"path/filepath"
	"strings"

	"tofu/internal/sys"
)

const (
	openRouterVariable = "OPENROUTER_KEY"
	openRouterFileMode = 0o600
)

func OpenRouterPath() (string, error) {
	dir, err := sys.HomeConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, sys.CredentialFileName), nil
}

func SaveOpenRouter(path, key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("cred: the openrouter key is empty")
	}
	if strings.ContainsAny(key, "\r\n") {
		return errors.New("cred: the openrouter key carries a line break")
	}
	kept, err := linesWithoutTheKey(path)
	if err != nil {
		return err
	}
	kept = append(kept, openRouterVariable+"="+key)
	return sys.WriteCredential(path, []byte(strings.Join(kept, "\n")+"\n"), openRouterFileMode)
}

func linesWithoutTheKey(path string) ([]string, error) {
	present, err := sys.Exists(path)
	if err != nil || !present {
		return nil, err
	}
	raw, err := sys.ReadCredential(path)
	if err != nil {
		return nil, err
	}
	text := strings.TrimRight(string(raw), "\n")
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	var kept []string
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimPrefix(strings.TrimSpace(line), "export "), openRouterVariable+"=") {
			continue
		}
		kept = append(kept, line)
	}
	return kept, nil
}
