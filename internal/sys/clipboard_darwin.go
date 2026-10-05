package sys

import (
	"bytes"
	"encoding/hex"
	"errors"
	"os"
	"strings"
)

const printedPNGFrame = "«data PNGf»\n"

func ReadClipboard() (Clipboard, error) {
	if os.Getenv("SSH_CONNECTION") != "" {
		return Clipboard{}, ErrNoLocalClipboard
	}
	info, err := readClipboardTool(ClipboardMaxBytes, "osascript", "osascript", "-e", "clipboard info")
	if err != nil {
		return Clipboard{}, err
	}
	switch {
	case bytes.Contains(info, []byte("PNGf")):
		printed, err := readClipboardTool(2*ClipboardMaxBytes+len(printedPNGFrame), "osascript", "osascript", "-e", "the clipboard as «class PNGf»")
		if err != nil {
			return Clipboard{}, err
		}
		_, digits, found := strings.Cut(strings.TrimSpace(string(printed)), "PNGf")
		if !found {
			return Clipboard{}, errors.New("sys: osascript printed no PNGf data for the image on the clipboard")
		}
		body, err := hex.DecodeString(strings.TrimSuffix(digits, "»"))
		return Clipboard{Kind: ClipboardImage, PNG: body}, err
	case bytes.Contains(info, []byte("furl")):
		path, err := readClipboardTool(ClipboardMaxBytes, "osascript", "osascript", "-e", "POSIX path of (the clipboard as «class furl»)")
		return Clipboard{Kind: ClipboardFiles, Files: []string{strings.TrimSuffix(string(path), "\n")}}, err
	}
	text, err := readClipboardTool(ClipboardMaxBytes, "pbpaste", "pbpaste")
	return textClipboard(text), err
}

func WriteClipboardText(text string) error {
	if os.Getenv("SSH_CONNECTION") != "" {
		return ErrNoLocalClipboard
	}
	return runClipboardTool("pbcopy", strings.NewReader(text), nil, "pbcopy")
}
