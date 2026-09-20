//go:build windows

package sys

import (
	"bytes"
	"image/png"
	"os"
	"strings"
	"testing"
)

const liveClipboardVar = "TOFU_LIVE_CLIPBOARD"

func TestWriteClipboardTextRefusesTextWindowsCannotCarryAndNeverOpensTheClipboard(t *testing.T) {
	err := WriteClipboardText("the answer\x00and a zero byte")
	if err == nil {
		t.Fatal("text holding a zero byte was reported as copied")
	}
	if !strings.Contains(err.Error(), "zero byte") {
		t.Errorf("the refusal does not say what is wrong with the text: %v", err)
	}
}

func TestReadClipboardReadsTheClipboardThisMachineHolds(t *testing.T) {
	if os.Getenv(liveClipboardVar) == "" {
		t.Skip("set " + liveClipboardVar + " to read the clipboard this machine is actually holding")
	}
	clip, err := ReadClipboard()
	if err != nil {
		t.Fatalf("the windows clipboard did not read: %v", err)
	}
	t.Logf("kind %d, %d text bytes, %d image bytes, files %v", clip.Kind, len(clip.Text), len(clip.PNG), clip.Files)
	if clip.Kind == ClipboardEmpty {
		t.Fatal("the clipboard read as empty, and this test is only meaningful with something on it")
	}
	if clip.Kind != ClipboardImage {
		return
	}
	picture, err := png.Decode(bytes.NewReader(clip.PNG))
	if err != nil {
		t.Fatalf("what came off the clipboard is not a png: %v", err)
	}
	t.Logf("the image is %v", picture.Bounds())
}
