package palette

import (
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/filepicker"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"tofu/interface/tui/look"
)

const (
	outsideWorkspace = "File picker stays inside this workspace"
	emptyDirectory   = "no files here"
	filesMaxRows     = 15
	filesMinRows     = 5
	filesChrome      = 10
	filesWidthInset  = 6
	filesMaxWidth    = 62
	filesMaxHeight   = 23
)

type FilesChoice struct {
	Path, Notice    string
	Done, Cancelled bool
}

type Files struct {
	root   string
	picker filepicker.Model
}

func NewFiles(root string) (Files, tea.Cmd) {
	picker := filepicker.New()
	picker.ShowPermissions, picker.ShowSize, picker.AutoHeight = false, false, false
	picker.Styles.Cursor = look.Style(look.Mint)
	picker.Styles.Selected = look.Style(look.Text).Background(lipgloss.Color(string(look.PanelLight)))
	picker.Styles.Directory = look.Style(look.Amber)
	picker.Styles.File = look.Style(look.MutedColor)
	picker.Styles.Symlink = look.Style(look.Blue)
	picker.Styles.EmptyDirectory = picker.Styles.EmptyDirectory.Foreground(lipgloss.Color(string(look.FaintColor))).SetString(emptyDirectory)
	picker.CurrentDirectory = root
	picker.SetHeight(filesMaxRows)
	return Files{root: root, picker: picker}, picker.Init()
}

func (f *Files) Update(msg tea.Msg) (FilesChoice, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		f.picker.SetHeight(min(filesMaxRows, max(filesMinRows, msg.Height-filesChrome)))
	case tea.KeyPressMsg:
		if msg.String() == "esc" {
			return FilesChoice{Cancelled: true}, nil
		}
	}
	var cmd tea.Cmd
	f.picker, cmd = f.picker.Update(msg)
	if _, inside := f.relative(f.picker.CurrentDirectory); !inside {
		f.picker.CurrentDirectory = f.root
		return FilesChoice{Notice: outsideWorkspace}, f.picker.Init()
	}
	if selected, path := f.picker.DidSelectFile(msg); selected {
		if rel, inside := f.relative(path); inside {
			return FilesChoice{Path: rel, Done: true}, nil
		}
	}
	return FilesChoice{}, cmd
}

func (f Files) Over(base string, width, height int) string {
	rel, _ := f.relative(f.picker.CurrentDirectory)
	modalWidth := min(filesMaxWidth, width-filesWidthInset)
	content := heading("Reference a file", look.Faint("@"+rel), modalWidth-2*dialogPadding) + f.picker.View() + "\n" + look.Faint("↑↓ browse  ·  enter select  ·  ← parent")
	modal := look.ModalPane(modalWidth, min(filesMaxHeight, height-paneHeightInset), look.Panel, dialogPadding, content)
	x, y := place(modal, width, height, paneMargin)
	return compose(base, modal, x, y)
}

func (f Files) relative(path string) (string, bool) {
	rel, err := filepath.Rel(f.root, path)
	return filepath.ToSlash(rel), err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
