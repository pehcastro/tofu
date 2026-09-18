package jev

import (
	"os"
	"path/filepath"
	"strings"

	"boji/internal/sys"
	"boji/internal/transport"
)

type Source int

const (
	SourceMissing Source = iota
	SourceEnvironment
	SourceDotEnv
)

type Located struct {
	Name   string
	Source Source
	Path   string
	Length int
}

func Key(envPath string) (string, error) {
	value, _, err := find(envPath)
	return value, err
}

func Locate(envPath string) (Located, error) {
	value, located, err := find(envPath)
	located.Length = len(value)
	return located, err
}

func find(envPath string) (string, Located, error) {
	value, name := os.Getenv("OPENROUTER_KEY"), "OPENROUTER_KEY"
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		return trimmed, Located{Name: name, Source: SourceEnvironment}, nil
	}
	if envPath == "" {
		return "", Located{Name: name, Source: SourceMissing}, transport.Fail("jev.Key", transport.KindMissingCredential, nil, "%s is not set", name)
	}
	abs, err := filepath.Abs(envPath)
	if err != nil {
		abs = envPath
	}
	missing := Located{Name: name, Source: SourceMissing, Path: abs}
	present, err := sys.Exists(envPath)
	if err != nil {
		return "", missing, transport.Fail("jev.Key", transport.KindMissingCredential, err, "reading %s", envPath)
	}
	if !present {
		return "", missing, transport.Fail("jev.Key", transport.KindMissingCredential, nil, "%s is not set and %s does not exist", name, envPath)
	}
	raw, err := sys.ReadFile(envPath)
	if err != nil {
		return "", missing, transport.Fail("jev.Key", transport.KindMissingCredential, err, "reading %s", envPath)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "export "))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, found := strings.CutPrefix(line, name+"=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		key = strings.Trim(key, `"'`)
		if key != "" {
			return key, Located{Name: name, Source: SourceDotEnv, Path: abs}, nil
		}
	}
	return "", missing, transport.Fail("jev.Key", transport.KindMissingCredential, nil, "%s is not set and %s does not carry it", name, envPath)
}
