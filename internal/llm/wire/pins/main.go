package main

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"time"

	"tofu/internal/konst"
	"tofu/internal/llm/wire/anthropic"
	"tofu/internal/settings"
	"tofu/internal/transport"
)

func main() {
	if err := raisePins(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "pins:", err)
		os.Exit(1)
	}
}

func raisePins() error {
	client, err := transport.New(transport.Config{AttemptTimeout: time.Duration(konst.TurnAttemptTimeoutMillis) * time.Millisecond, Concurrency: 1})
	if err != nil {
		return err
	}
	latest, err := settings.LatestFingerprint(context.Background(), client, settings.NpmRegistry())
	if err != nil {
		return err
	}
	for _, pin := range []struct{ file, name, latest string }{
		{"fingerprint.go", "PinnedClaudeCodeVersion", latest.ClaudeCode},
		{"../codex/fingerprint.go", "PinnedCodexClientVersion", latest.Codex},
	} {
		source, err := os.ReadFile(pin.file)
		if err != nil {
			return err
		}
		pattern := regexp.MustCompile(`\b` + pin.name + `(\s*=\s*)"([^"]*)"`)
		match := pattern.FindSubmatch(source)
		if match == nil {
			return fmt.Errorf("%s holds no %s", pin.file, pin.name)
		}
		held := string(match[2])
		if anthropic.NewerVersion(held, pin.latest) == held {
			fmt.Printf("%s stays at %s, npm has %s\n", pin.name, held, pin.latest)
			continue
		}
		info, err := os.Stat(pin.file)
		if err != nil {
			return err
		}
		if err := os.WriteFile(pin.file, pattern.ReplaceAll(source, []byte(pin.name+`${1}"`+pin.latest+`"`)), info.Mode()); err != nil {
			return err
		}
		fmt.Printf("%s raised from %s to %s\n", pin.name, held, pin.latest)
	}
	return nil
}
