package paste

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestADroppedImagePathIsReadFromEveryFormATerminalGives(t *testing.T) {
	type drop struct {
		pasted string
		want   string
	}
	attaches := []drop{
		{`/home/someone/My Shots/shot.png`, `/home/someone/My Shots/shot.png`},
		{`/home/someone/My\ Shots/shot\(1\).PNG`, `/home/someone/My Shots/shot(1).PNG`},
		{`'/home/someone/My Shots/shot.webp'` + "\n", `/home/someone/My Shots/shot.webp`},
		{`file:///home/someone/My%20Shots/shot.jpeg`, `/home/someone/My Shots/shot.jpeg`},
	}
	if runtime.GOOS == "windows" {
		attaches = []drop{
			{`"C:\Users\someone\My Shots\shot.png"`, `C:\Users\someone\My Shots\shot.png`},
			{`C:\Users\someone\shot.PNG `, `C:\Users\someone\shot.PNG`},
			{`'C:\Users\someone\(1)\shot.gif'`, `C:\Users\someone\(1)\shot.gif`},
			{`\\server\share\shot.jpg`, `\\server\share\shot.jpg`},
			{`file:///C:/Users/someone/My%20Shots/shot.png`, `C:\Users\someone\My Shots\shot.png`},
		}
	}
	for _, row := range attaches {
		got, ok := droppedImage(row.pasted)
		if !ok || got != filepath.FromSlash(row.want) {
			t.Errorf("%q read as %q, %v; want %q attached", row.pasted, got, ok, row.want)
		}
	}
	for _, pasted := range []string{
		`C:\Users\someone\notes.txt`,
		`/home/someone/notes.txt`,
		`shot.png`,
		`look at C:\Users\someone\shot.png`,
		`look at /home/someone/shot.png`,
		"C:\\a.png\nC:\\b.png",
		"/home/a.png\n/home/b.png",
		`"C:\Users\someone\shot.png`,
		`file://server/share/shot.png`,
		`https://example.com/shot.png`,
		``,
	} {
		if got, ok := droppedImage(pasted); ok {
			t.Errorf("%q attached as %q; want it kept as text", pasted, got)
		}
	}
}
