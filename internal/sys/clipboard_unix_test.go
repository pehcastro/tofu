//go:build linux

package sys

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

const pngSignature = "\x89PNG\r\n\x1a\n"

func onlyTools(t *testing.T, display string, tools map[string]string) {
	t.Helper()
	dir := t.TempDir()
	for name, body := range tools {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("DISPLAY", "")
	if display != "" {
		t.Setenv(display, "fake")
	}
}

func wlPaste(types string, bodies map[string]string) string {
	script := "if [ \"$1\" = --list-types ]; then printf '" + types + "'; exit 0; fi\nfor last; do :; done\ncase \"$last\" in\n"
	for kind, body := range bodies {
		script += "'" + kind + "') printf '" + body + "' ;;\n"
	}
	return script + "*) echo \"no $last\" >&2; exit 1 ;;\nesac"
}

func TestReadClipboardOffWindowsHandlesEveryWayTheHelperCanAnswer(t *testing.T) {
	cases := []struct {
		name    string
		display string
		tools   map[string]string
		want    Clipboard
		failure string
	}{
		{name: "no display at all", failure: ErrNoLocalClipboard.Error()},
		{name: "wayland with no wl-paste names the package", display: "WAYLAND_DISPLAY", failure: "wl-clipboard"},
		{name: "x11 with neither xclip nor xsel names xclip", display: "DISPLAY", failure: "xclip"},
		{
			name: "nothing copied is empty, not a failure", display: "WAYLAND_DISPLAY",
			tools: map[string]string{"wl-paste": "echo 'Nothing is copied' >&2; exit 1"},
			want:  Clipboard{Kind: ClipboardEmpty},
		},
		{
			name: "a dead compositor is a failure carrying what the tool said", display: "WAYLAND_DISPLAY",
			tools:   map[string]string{"wl-paste": "echo 'Failed to connect to a Wayland server' >&2; exit 1"},
			failure: "Failed to connect to a Wayland server",
		},
		{
			name: "png wins over the text a browser offers beside it", display: "WAYLAND_DISPLAY",
			tools: map[string]string{"wl-paste": wlPaste(`text/html\ntext/plain\nimage/png\n`, map[string]string{
				"image/png": `\211PNG\r\n\032\n`, "text/plain": "alt text",
			})},
			want: Clipboard{Kind: ClipboardImage, PNG: []byte(pngSignature)},
		},
		{
			name: "a jpeg is refused rather than handed over as png", display: "WAYLAND_DISPLAY",
			tools:   map[string]string{"wl-paste": wlPaste(`image/jpeg\n`, map[string]string{"image/jpeg": "jpeg"})},
			failure: "image/jpeg",
		},
		{
			name: "a uri list keeps only decoded file entries", display: "WAYLAND_DISPLAY",
			tools: map[string]string{"wl-paste": wlPaste(`text/uri-list\ntext/plain\n`, map[string]string{
				"text/uri-list": `# copied\r\nhttps://example.com/a.png\r\nfile:///tmp/shot%%20one.png\r\nfile:///tmp/b.png\r\n`,
			})},
			want: Clipboard{Kind: ClipboardFiles, Files: []string{"/tmp/shot one.png", "/tmp/b.png"}},
		},
		{
			name: "a uri list naming no file falls through to text", display: "WAYLAND_DISPLAY",
			tools: map[string]string{"wl-paste": wlPaste(`text/uri-list\ntext/plain;charset=utf-8\n`, map[string]string{
				"text/uri-list": "https://example.com/", "text/plain;charset=utf-8": "https://example.com/",
			})},
			want: Clipboard{Kind: ClipboardText, Text: "https://example.com/"},
		},
		{
			name: "x11 reads the targets xclip lists", display: "DISPLAY",
			tools: map[string]string{"xclip": `case "$4" in TARGETS) printf 'TARGETS\nUTF8_STRING\n' ;; UTF8_STRING) printf 'from x' ;; *) exit 1 ;; esac`},
			want:  Clipboard{Kind: ClipboardText, Text: "from x"},
		},
		{
			name: "x11 with only xsel still reads text", display: "DISPLAY",
			tools: map[string]string{"xsel": `printf 'from xsel'`},
			want:  Clipboard{Kind: ClipboardText, Text: "from xsel"},
		},
		{
			name: "past the paste ceiling stops reading", display: "WAYLAND_DISPLAY",
			tools: map[string]string{"wl-paste": "if [ \"$1\" = --list-types ]; then echo image/png; exit 0; fi\n/usr/bin/head -c " +
				strconv.Itoa(ClipboardMaxBytes+1) + " /dev/zero"},
			failure: "paste ceiling",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			onlyTools(t, c.display, c.tools)
			got, err := ReadClipboard()
			if c.failure != "" {
				if err == nil || !strings.Contains(err.Error(), c.failure) {
					t.Fatalf("want a failure naming %q, got %v and %+v", c.failure, err, got.Kind)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Kind != c.want.Kind || got.Text != c.want.Text || !bytes.Equal(got.PNG, c.want.PNG) || !slices.Equal(got.Files, c.want.Files) {
				t.Fatalf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestReadClipboardGivesUpOnAHelperThatNeverAnswers(t *testing.T) {
	onlyTools(t, "WAYLAND_DISPLAY", map[string]string{"wl-paste": "/bin/sleep 60"})
	started := time.Now()
	_, err := ReadClipboard()
	if err == nil {
		t.Fatal("a hung helper read as a clipboard")
	}
	if waited := time.Since(started); waited > 6*time.Second {
		t.Fatalf("waited %v on a hung helper", waited)
	}
}

func TestWriteClipboardTextDoesNotWaitForTheDaemonTheToolLeavesBehind(t *testing.T) {
	onlyTools(t, "WAYLAND_DISPLAY", map[string]string{"wl-copy": "/bin/cat > \"$0.got\"; /bin/sleep 30 &"})
	started := time.Now()
	if err := WriteClipboardText("héllo"); err != nil {
		t.Fatal(err)
	}
	if waited := time.Since(started); waited > time.Second {
		t.Fatalf("the write waited %v for the daemon", waited)
	}
	got, err := os.ReadFile(filepath.Join(os.Getenv("PATH"), "wl-copy.got"))
	if err != nil || string(got) != "héllo" {
		t.Fatalf("wl-copy was handed %q, %v", got, err)
	}
}

func TestWriteClipboardTextFallsBackToTheTerminalWithoutADisplayOrATool(t *testing.T) {
	for _, display := range []string{"", "WAYLAND_DISPLAY", "DISPLAY"} {
		onlyTools(t, display, nil)
		if err := WriteClipboardText("x"); !errors.Is(err, ErrNoLocalClipboard) {
			t.Errorf("display %q: want ErrNoLocalClipboard, got %v", display, err)
		}
	}
}
