package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"tofu/internal/browser"
)

type unreadStdin struct{ t *testing.T }

func (u unreadStdin) Read([]byte) (int, error) {
	u.t.Error("host mode read stdin before checking the origin")
	return 0, io.EOF
}

func TestBrowserWithNoHostSaysNotConnected(t *testing.T) {
	home, err := os.MkdirTemp("", "tb")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, args := range [][]string{{"browser"}, {"browser", "open", "https://example.com/"}, {"browser", "close", "12"}} {
		var out, errOut bytes.Buffer
		if code := run(args, strings.NewReader(""), &out, &errOut); code != exitUsage {
			t.Fatalf("tofu %v exited %d, want %d; stderr %q", args, code, exitUsage, errOut.String())
		}
		if !strings.Contains(errOut.String(), browser.ErrNotConnected.Error()) || out.Len() != 0 {
			t.Fatalf("tofu %v printed %q and %q", args, out.String(), errOut.String())
		}
	}
	for _, args := range [][]string{{"browser", "open"}, {"browser", "close"}, {"browser", "close", "twelve"}, {"browser", "install", "x"}, {"browser", "open", "a", "b"}} {
		var out, errOut bytes.Buffer
		if code := run(args, strings.NewReader(""), &out, &errOut); code != exitUsage || !strings.HasPrefix(errOut.String(), "usage: tofu browser") || out.Len() != 0 {
			t.Fatalf("tofu %v exited %d and printed %q and %q", args, code, out.String(), errOut.String())
		}
	}
}

func TestHostModeRefusesAWrongOriginBeforeStdin(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	var out, errOut bytes.Buffer
	code := run([]string{"chrome-extension://ponmlkjihgfedcbaponmlkjihgfedcba/", "--parent-window=0"}, unreadStdin{t}, &out, &errOut)
	if code != exitVerdict || out.Len() != 0 || !strings.HasPrefix(errOut.String(), "tofu host: ") || !strings.Contains(errOut.String(), "chrome-extension://ponmlkjihgfedcbaponmlkjihgfedcba/") {
		t.Fatalf("host mode with a wrong origin exited %d, wrote %q to Chrome and %q to stderr", code, out.String(), errOut.String())
	}
}
