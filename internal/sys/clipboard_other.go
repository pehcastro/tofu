//go:build !windows

package sys

import "errors"

func ReadClipboard() (Clipboard, error) {
	return Clipboard{}, errors.New("sys: reading the clipboard is implemented on windows only")
}

func WriteClipboardText(string) error { return ErrNoLocalClipboard }
