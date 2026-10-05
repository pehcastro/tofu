package tui

import (
	"cmp"
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"tofu/interface/tui/session"
)

type editedMsg struct {
	text string
	err  error
}

type editorRun struct {
	argv   []string
	draft  string
	edited string
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
}

func (r *editorRun) SetStdin(in io.Reader)   { r.stdin = in }
func (r *editorRun) SetStdout(out io.Writer) { r.stdout = out }
func (r *editorRun) SetStderr(out io.Writer) { r.stderr = out }

func (r *editorRun) Run() error {
	file, err := os.CreateTemp("", "tofu-prompt-*.md")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(file.Name()) }()
	_, err = file.WriteString(r.draft + "\n")
	if err = errors.Join(err, file.Close()); err != nil {
		return err
	}
	editor := exec.Command(r.argv[0], append(r.argv[1:], file.Name())...)
	editor.Stdin, editor.Stdout, editor.Stderr = r.stdin, r.stdout, r.stderr
	if err := editor.Run(); err != nil {
		return err
	}
	saved, err := os.ReadFile(file.Name())
	r.edited = editedText(saved)
	return err
}

func editorCommand(getenv func(string) string) ([]string, error) {
	fallback := "vi"
	if runtime.GOOS == "windows" {
		fallback = "notepad"
	}
	line := cmp.Or(strings.TrimSpace(getenv("VISUAL")), strings.TrimSpace(getenv("EDITOR")), fallback)
	var words []string
	var word strings.Builder
	quoted, started := false, false
	for _, char := range line {
		switch {
		case char == '"':
			quoted, started = !quoted, true
		case unicode.IsSpace(char) && !quoted:
			if started {
				words = append(words, word.String())
				word.Reset()
			}
			started = false
		default:
			word.WriteRune(char)
			started = true
		}
	}
	if quoted {
		return nil, errors.New("the editor command " + line + " has an unmatched quote")
	}
	return append(words, word.String()), nil
}

func editedText(saved []byte) string {
	text := strings.ReplaceAll(strings.TrimPrefix(string(saved), "\xef\xbb\xbf"), "\r\n", "\n")
	return strings.TrimSuffix(text, "\n")
}

func (a *App) openEditor() tea.Cmd {
	argv, err := editorCommand(os.Getenv)
	if err != nil {
		a.edited(editedMsg{err: err})
		return nil
	}
	run := &editorRun{argv: argv, draft: a.view.Draft()}
	return tea.Exec(run, func(err error) tea.Msg { return editedMsg{text: run.edited, err: err} })
}

func (a *App) edited(msg editedMsg) {
	if msg.err != nil {
		a.view.Append(session.Entry{Kind: session.Note, Body: "the editor failed (" + msg.err.Error() + "), so the prompt is unchanged"})
		return
	}
	a.view.Redraft(msg.text)
}
