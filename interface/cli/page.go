package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/term"

	"tofu/interface/tui/look"
	"tofu/internal/konst"
	"tofu/internal/sys"
	"tofu/internal/widget"
)

const pageMaxCells = 100

type Page struct {
	Profile colorprofile.Profile
	ASCII   bool
	Width   int
	Home    string
}

func Detect(out io.Writer, environ []string) Page {
	env := func(key string) string {
		value := ""
		for _, pair := range environ {
			if name, found, ok := strings.Cut(pair, "="); ok && strings.EqualFold(name, key) {
				value = found
			}
		}
		return value
	}
	if force := env("FORCE_COLOR"); force != "" && force != "0" && force != "false" {
		environ = append(environ, "CLICOLOR_FORCE=1")
	}
	page := Page{Profile: colorprofile.Detect(out, environ), ASCII: env("TERM") == "dumb", Width: konst.ProseWidthChars, Home: env("HOME")}
	if page.Home == "" {
		page.Home = env("USERPROFILE")
	}
	if env("NO_COLOR") != "" {
		page.Profile = colorprofile.NoTTY
	}
	if file, ok := out.(*os.File); ok {
		forced, _ := strconv.ParseBool(env("CLICOLOR_FORCE"))
		if page.Profile > colorprofile.NoTTY && sys.EnableVT(file.Fd()) != nil && !forced {
			page.Profile = colorprofile.NoTTY
		}
		if columns, _, err := term.GetSize(file.Fd()); err == nil && columns > 0 {
			page.Width = min(columns, pageMaxCells)
		}
	}
	return page
}

func (p Page) Colour() bool { return p.Profile > colorprofile.ASCII }

func (p Page) Print(out io.Writer, lines []string) error {
	var text strings.Builder
	for _, line := range lines {
		if widget.Cells(line) > p.Width && !strings.Contains(line, hintArrow) {
			line = p.wrap(line)
		}
		text.WriteString(line)
		text.WriteByte('\n')
	}
	written := text.String()
	if p.ASCII {
		written = look.ASCII(written)
	}
	_, err := (&colorprofile.Writer{Forward: out, Profile: p.Profile}).Write([]byte(written))
	return err
}

func (p Page) wrap(line string) string {
	body := strings.TrimLeft(line, " ")
	lead := line[:len(line)-len(body)]
	room := p.Width - len(lead) - len(Gap)
	rows := []string{""}
	for _, word := range strings.Split(strings.TrimRight(body, " "), " ") {
		last := &rows[len(rows)-1]
		switch {
		case *last == "":
			*last = word
		case widget.Cells(*last+" "+word) <= room:
			*last += " " + word
		default:
			*last = strings.TrimRight(*last, " ")
			rows = append(rows, word)
		}
	}
	return lead + strings.Join(rows, "\n"+lead+Gap)
}

func (p Page) Path(path string) string {
	home, slashed := strings.TrimRight(filepath.ToSlash(p.Home), "/"), filepath.ToSlash(path)
	if home == "" || len(slashed) < len(home) || !strings.EqualFold(slashed[:len(home)], home) {
		return path
	}
	if rest := slashed[len(home):]; rest == "" || rest[0] == '/' {
		return "~" + rest
	}
	return path
}

func (p Page) paint(style lipgloss.Style, text string) string {
	if !p.Colour() || text == "" {
		return text
	}
	return style.Render(text)
}

func (p Page) Subject(text string) string { return p.paint(lipgloss.NewStyle().Bold(true), text) }

func (p Page) Label(text string) string { return p.paint(look.Style(look.MutedColor), text) }

type Problem struct {
	What string `json:"what"`
	Hint string `json:"hint,omitempty"`
}

type Envelope struct {
	Verb     string
	OK       bool
	At       time.Time
	Data     any
	Problems []Problem
}

func (e Envelope) MarshalJSON() ([]byte, error) {
	var document bytes.Buffer
	encoder := json.NewEncoder(&document)
	encoder.SetEscapeHTML(false)
	err := encoder.Encode(struct {
		Tofu     string    `json:"tofu"`
		Verb     string    `json:"verb"`
		OK       bool      `json:"ok"`
		At       string    `json:"at"`
		Data     any       `json:"data"`
		Problems []Problem `json:"problems"`
	}{konst.Version, e.Verb, e.OK, e.At.UTC().Format(time.RFC3339), e.Data, append([]Problem{}, e.Problems...)})
	return document.Bytes(), err
}
