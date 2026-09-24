package jev

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/sys"
	"tofu/internal/transport"
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

const (
	OpenRouterVariable = "OPENROUTER_KEY"
	TypeSafeVariable   = "TYPESAFE_API_KEY"
)

func AllowLiveCredential(tb testing.TB) {
	sys.AllowLiveCredential(tb)
}

func Key(envPath string) (string, error) {
	return KeyFor(envPath, OpenRouterVariable)
}

func KeyFor(envPath, variable string) (string, error) {
	value, _, err := find(envPath, variable)
	return value, err
}

func Locate(envPath string) (Located, error) {
	value, located, err := find(envPath, OpenRouterVariable)
	located.Length = len(value)
	return located, err
}

type Why int

const (
	WhyUnexplained Why = iota
	WhyNoFile
	WhyFileLacksName
	WhyUnreadable
)

type MissingKey struct {
	Why     Why
	wrapped error
}

func (m MissingKey) Error() string { return m.wrapped.Error() }

func (m MissingKey) Unwrap() error { return m.wrapped }

func noKey(name, path string, why Why, cause error, format string, args ...any) (string, Located, error) {
	failure := transport.Fail("jev.Key", transport.KindMissingCredential, cause, format, args...)
	return "", Located{Name: name, Source: SourceMissing, Path: path}, MissingKey{Why: why, wrapped: failure}
}

func find(envPath, name string) (string, Located, error) {
	hidden := sys.CredentialsHiddenFromTests()
	value := strings.TrimSpace(os.Getenv(name))
	if hidden && sys.EqualsOwnerCredential(name, value) {
		value = ""
	}
	if value != "" {
		return value, Located{Name: name, Source: SourceEnvironment}, nil
	}
	if envPath == "" {
		return noKey(name, "", WhyUnexplained, nil, "%s is not set", name)
	}
	abs, err := filepath.Abs(envPath)
	if err != nil {
		abs = envPath
	}
	if hidden && sys.IsOwnerCredential(abs) {
		return noKey(name, abs, WhyUnexplained, nil, "%s is hidden from tests: %s is one of the owner's credential files, and a test that means to spend calls jev.AllowLiveCredential first", name, abs)
	}
	present, err := sys.Exists(envPath)
	if err != nil {
		return noKey(name, abs, WhyUnreadable, err, "reading %s", envPath)
	}
	if !present {
		return noKey(name, abs, WhyNoFile, nil, "%s is not set and %s does not exist", name, envPath)
	}
	raw, err := sys.ReadCredential(envPath)
	if err != nil {
		return noKey(name, abs, WhyUnreadable, err, "reading %s", envPath)
	}
	if key := sys.CredentialAssignment(string(raw), name); key != "" {
		return key, Located{Name: name, Source: SourceDotEnv, Path: abs}, nil
	}
	return noKey(name, abs, WhyFileLacksName, nil, "%s is not set and %s does not carry it", name, envPath)
}
