package lsp

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type Server struct {
	Language   string
	Program    string
	Args       []string
	Install    string
	Markers    []string
	Extensions map[string]string
	Options    map[string]any
	Quiesces   bool
}

func Servers() []Server {
	return []Server{
		{Language: "rust", Program: "rust-analyzer", Install: "rustup component add rust-analyzer",
			Markers: []string{"Cargo.toml"}, Extensions: map[string]string{".rs": "rust"}, Quiesces: true,
			Options: map[string]any{"checkOnSave": false, "cachePriming": map[string]bool{"enable": false}}},
		{Language: "typescript", Program: "typescript-language-server", Args: []string{"--stdio"}, Install: "npm install -g typescript-language-server typescript",
			Markers:    []string{"tsconfig.json", "jsconfig.json", "package.json"},
			Extensions: map[string]string{".ts": "typescript", ".tsx": "typescriptreact", ".mts": "typescript", ".cts": "typescript", ".js": "javascript", ".jsx": "javascriptreact"}},
		{Language: "python", Program: "pyright-langserver", Args: []string{"--stdio"}, Install: "npm install -g pyright",
			Markers: []string{"pyrightconfig.json", "pyproject.toml", "setup.py", "setup.cfg"}, Extensions: map[string]string{".py": "python"}},
		{Language: "go", Program: "gopls", Install: "go install golang.org/x/tools/gopls@latest",
			Markers: []string{"go.mod"}, Extensions: map[string]string{".go": "go"}},
	}
}

func ServerFor(path string) (Server, bool) {
	for _, server := range Servers() {
		if _, serves := server.Extensions[filepath.Ext(path)]; serves {
			return server, true
		}
	}
	return Server{}, false
}

func (s Server) RootOf(project, file string) string {
	for dir := filepath.Dir(file); ; dir = filepath.Dir(dir) {
		for _, marker := range s.Markers {
			if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
				return dir
			}
		}
		if rel, err := filepath.Rel(project, dir); err != nil || rel == "." || strings.HasPrefix(rel, "..") || dir == filepath.Dir(dir) {
			return project
		}
	}
}

func fileURI(path string) string {
	slashed := filepath.ToSlash(path)
	if !strings.HasPrefix(slashed, "/") {
		slashed = "/" + slashed
	}
	return (&url.URL{Scheme: "file", Path: slashed}).String()
}

func uriPath(uri string) string {
	parsed, err := url.Parse(uri)
	if err != nil {
		return uri
	}
	path := parsed.Path
	if len(path) > 2 && path[0] == '/' && path[2] == ':' {
		path = path[1:]
	}
	return filepath.FromSlash(path)
}
