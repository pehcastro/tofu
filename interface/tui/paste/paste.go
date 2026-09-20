package paste

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"tofu/interface/tui/theme"
	"tofu/internal/sys"
	"tofu/internal/widget"
)

type State int

const (
	Working State = iota
	Ready
	Failed
	Textual
)

type Outcome struct {
	Index int
	State State
	Name  string
	Bytes int
	Text  string
	Cause string
}

type Board struct {
	Read     func() (sys.Clipboard, error)
	Dir      func() (string, error)
	Write    func(path string, body []byte) error
	Recorded func(name string, bytes int, format string)
}

const (
	pastedImageFileMode = 0o644
	marker              = "▣ "
	imageInfix          = "-image-"
	pngSuffix           = ".png"
	orderFormat         = "%02d"
	gap                 = "  "
)

func Default(board Board) Board {
	if board.Read == nil {
		board.Read = sys.ReadClipboard
	}
	if board.Dir == nil {
		board.Dir = func() (string, error) {
			return "", errors.New("paste: nothing decides where a pasted image belongs")
		}
	}
	if board.Write == nil {
		board.Write = func(path string, body []byte) error {
			return sys.WriteFile(path, body, pastedImageFileMode)
		}
	}
	return board
}

func (b Board) Attach(index int) tea.Cmd {
	return func() tea.Msg {
		outcome, err := b.read(index)
		if err != nil {
			return Outcome{Index: index, State: Failed, Cause: err.Error()}
		}
		return outcome
	}
}

func (b Board) read(index int) (Outcome, error) {
	clip, err := b.Read()
	if err != nil {
		return Outcome{}, err
	}
	switch clip.Kind {
	case sys.ClipboardEmpty:
		return Outcome{}, errors.New("the clipboard holds nothing to paste")
	case sys.ClipboardText:
		return Outcome{Index: index, State: Textual, Text: clip.Text}, nil
	case sys.ClipboardImage:
		return b.store(index, clip.PNG, pngSuffix)
	case sys.ClipboardFiles:
		return b.copyFile(index, clip.Files)
	}
	panic("paste: unknown clipboard kind")
}

func (b Board) copyFile(index int, files []string) (Outcome, error) {
	if len(files) == 0 {
		return Outcome{}, errors.New("the clipboard names no file")
	}
	suffix := strings.ToLower(filepath.Ext(files[0]))
	if !slices.Contains([]string{pngSuffix, ".jpg", ".jpeg", ".gif", ".webp", ".bmp"}, suffix) {
		return Outcome{}, errors.New(filepath.Base(files[0]) + " is not an image tofu can attach")
	}
	body, err := os.ReadFile(files[0])
	if err != nil {
		return Outcome{}, err
	}
	if len(body) > sys.ClipboardMaxBytes {
		return Outcome{}, errors.New(filepath.Base(files[0]) + " is " + widget.Size(len(body)) + ", past the paste ceiling")
	}
	return b.store(index, body, suffix)
}

func (b Board) store(index int, body []byte, suffix string) (Outcome, error) {
	dir, err := b.Dir()
	if err != nil {
		return Outcome{}, err
	}
	prefix := filepath.Base(dir) + imageInfix
	name := prefix + fmt.Sprintf(orderFormat, imagesUnder(dir, prefix)+1) + suffix
	if err := b.Write(filepath.Join(dir, name), body); err != nil {
		return Outcome{}, err
	}
	outcome := Outcome{Index: index, State: Ready, Name: name, Bytes: len(body)}
	if b.Recorded != nil {
		b.Recorded(outcome.Name, outcome.Bytes, outcome.Format())
	}
	return outcome, nil
}

func imagesUnder(dir, prefix string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), prefix) {
			count++
		}
	}
	return count
}

func (o Outcome) Format() string {
	return strings.ToUpper(strings.TrimPrefix(filepath.Ext(o.Name), "."))
}

func (o Outcome) Render(width int) string {
	head := marker + "image " + strconv.Itoa(o.Index) + gap
	switch o.State {
	case Working:
		return theme.Faint().Render(widget.Fit(head+"pasting", width))
	case Ready:
		return theme.Tool().Render(widget.Fit(head+o.Format()+gap+widget.Size(o.Bytes)+gap+o.Name, width))
	case Failed:
		return theme.Fail().Render(widget.Fit(head+"could not be pasted: "+o.Cause, width))
	case Textual:
		return theme.Faint().Render(widget.Fit(head+"pasted as text", width))
	}
	panic("paste: unknown paste state")
}
