package paste

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"tofu/internal/sys"
)

const shellEscapables = " '\"()[]{}&;!$#*?<>|~`\\"

func (b Board) Dropped(text string) (Board, bool) {
	path, ok := droppedImage(text)
	if !ok {
		return b, false
	}
	b.Read = func() (sys.Clipboard, error) {
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			return sys.Clipboard{Kind: sys.ClipboardText, Text: text}, nil
		}
		return sys.Clipboard{Kind: sys.ClipboardFiles, Files: []string{path}}, nil
	}
	return b, true
}

func droppedImage(text string) (string, bool) {
	path := strings.TrimSpace(text)
	if len(path) >= 2 && (path[0] == '"' || path[0] == '\'') && path[len(path)-1] == path[0] {
		path = path[1 : len(path)-1]
	}
	windowsDrive := len(path) >= 3 && path[1] == ':' && (path[2] == '\\' || path[2] == '/')
	switch {
	case strings.ContainsAny(path, "\r\n"):
		return "", false
	case strings.HasPrefix(path, "file://"):
		parsed, err := url.Parse(path)
		if err != nil || (parsed.Host != "" && parsed.Host != "localhost") {
			return "", false
		}
		path = parsed.Path
		if len(path) > 2 && path[0] == '/' && path[2] == ':' {
			path = path[1:]
		}
		path = filepath.FromSlash(path)
	case !windowsDrive && !strings.HasPrefix(path, `\\`):
		path = unescaped(path)
	}
	_, image := imageSuffix(path)
	return path, image && filepath.IsAbs(path)
}

func unescaped(path string) string {
	var out strings.Builder
	for at := 0; at < len(path); at++ {
		if path[at] == '\\' && at+1 < len(path) && strings.IndexByte(shellEscapables, path[at+1]) >= 0 {
			at++
		}
		out.WriteByte(path[at])
	}
	return out.String()
}
