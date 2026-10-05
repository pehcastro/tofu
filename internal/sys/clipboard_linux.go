package sys

import (
	"errors"
	"net/url"
	"os"
	"slices"
	"strings"
)

const (
	pngType     = "image/png"
	uriListType = "text/uri-list"
)

type selectionTool struct {
	pkg, name     string
	list          []string
	typed         func(kind string) []string
	nothingCopied []string
}

func ReadClipboard() (Clipboard, error) {
	switch {
	case os.Getenv("WAYLAND_DISPLAY") != "":
		return readSelection(selectionTool{
			pkg: "wl-clipboard", name: "wl-paste", list: []string{"--list-types"},
			typed:         func(kind string) []string { return []string{"--no-newline", "--type", kind} },
			nothingCopied: []string{"Nothing is copied", "No selection"},
		})
	case os.Getenv("DISPLAY") != "":
		clip, err := readSelection(selectionTool{
			pkg: "xclip", name: "xclip", list: xclipTyped("TARGETS"), typed: xclipTyped,
			nothingCopied: []string{"target TARGETS not available"},
		})
		if !errors.Is(err, ErrNoLocalClipboard) {
			return clip, err
		}
		text, xselErr := readClipboardTool(ClipboardMaxBytes, "xsel", "xsel", "--clipboard", "--output")
		if errors.Is(xselErr, ErrNoLocalClipboard) {
			return Clipboard{}, err
		}
		return textClipboard(text), xselErr
	}
	return Clipboard{}, ErrNoLocalClipboard
}

func WriteClipboardText(text string) error {
	switch {
	case os.Getenv("WAYLAND_DISPLAY") != "":
		return runClipboardTool("wl-clipboard", strings.NewReader(text), nil, "wl-copy")
	case os.Getenv("DISPLAY") != "":
		err := runClipboardTool("xclip", strings.NewReader(text), nil, "xclip", "-selection", "clipboard")
		if !errors.Is(err, ErrNoLocalClipboard) {
			return err
		}
		if xselErr := runClipboardTool("xsel", strings.NewReader(text), nil, "xsel", "--clipboard", "--input"); !errors.Is(xselErr, ErrNoLocalClipboard) {
			return xselErr
		}
		return err
	}
	return ErrNoLocalClipboard
}

func xclipTyped(kind string) []string {
	return []string{"-selection", "clipboard", "-t", kind, "-o"}
}

func readSelection(tool selectionTool) (Clipboard, error) {
	listed, err := readClipboardTool(ClipboardMaxBytes, tool.pkg, tool.name, tool.list...)
	if err != nil {
		if slices.ContainsFunc(tool.nothingCopied, func(said string) bool { return strings.Contains(err.Error(), said) }) {
			return Clipboard{Kind: ClipboardEmpty}, nil
		}
		return Clipboard{}, err
	}
	offered := strings.Fields(string(listed))
	read := func(kind string) ([]byte, error) {
		return readClipboardTool(ClipboardMaxBytes, tool.pkg, tool.name, tool.typed(kind)...)
	}
	if slices.Contains(offered, pngType) {
		body, err := read(pngType)
		return Clipboard{Kind: ClipboardImage, PNG: body}, err
	}
	if slices.Contains(offered, uriListType) {
		list, err := read(uriListType)
		if err != nil {
			return Clipboard{}, err
		}
		if files := filePaths(string(list)); len(files) > 0 {
			return Clipboard{Kind: ClipboardFiles, Files: files}, nil
		}
	}
	for _, kind := range []string{"text/plain;charset=utf-8", "UTF8_STRING", "text/plain", "STRING", "TEXT"} {
		if slices.Contains(offered, kind) {
			text, err := read(kind)
			return textClipboard(text), err
		}
	}
	if image := slices.IndexFunc(offered, func(kind string) bool { return strings.HasPrefix(kind, "image/") }); image >= 0 {
		return Clipboard{}, errors.New("sys: the clipboard holds " + offered[image] + ", and only image/png can be attached")
	}
	return Clipboard{Kind: ClipboardEmpty}, nil
}

func filePaths(uriList string) []string {
	var files []string
	for line := range strings.Lines(uriList) {
		parsed, err := url.Parse(strings.TrimSpace(line))
		if err == nil && parsed.Scheme == "file" && parsed.Path != "" {
			files = append(files, parsed.Path)
		}
	}
	return files
}
