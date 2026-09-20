package sys

import "errors"

const ClipboardMaxBytes = 64 << 20

var ErrNoLocalClipboard = errors.New("sys: this machine has no clipboard of its own, so the terminal has to carry the copy")

type ClipboardKind int

const (
	ClipboardEmpty ClipboardKind = iota
	ClipboardText
	ClipboardImage
	ClipboardFiles
)

type Clipboard struct {
	Kind  ClipboardKind
	Text  string
	PNG   []byte
	Files []string
}
