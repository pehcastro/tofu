package api

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"tofu/internal/models/llm/core"
	"tofu/internal/tools/contract"
)

const Name = "api"

type Language string

const (
	Go     Language = "go"
	TS     Language = "ts"
	Rust   Language = "rust"
	Python Language = "python"
)

type Limits struct {
	SignatureBytes int
	DocBytes       int
	Names          int
	Matches        int
}

type API struct {
	root   contract.Root
	limits Limits
}

func New(dir string, limits Limits) (contract.Tool, error) {
	root, err := contract.NewRoot(dir)
	return API{root: root, limits: limits}, err
}

func (API) Name() string { return Name }

func (API) Definition() core.Tool {
	text := map[string]any{"type": "string"}
	return core.Tool{
		Name: Name,
		Description: "answers what a dependency looks like at the version this project pinned, read from disk and never the network: returns the installed version, where it was read from, and each declaration's signature and doc. " +
			"lang go reads the module go.mod requires from the module cache, as package github.com/pelletier/go-toml/v2 and name Marshal or Decoder.Decode. " +
			"lang ts reads the .d.ts files of node_modules/<package>, or its @types package, as package react and name useEffect. " +
			"lang rust reads the crate source Cargo.lock pins in the cargo registry, as package serde and name Deserialize or Error::msg. " +
			"lang python reads site-packages of the project's .venv or its python, as package requests and name Session.get. " +
			"name is one identifier or Container.member; without name it lists the package's names. lang may be left out when the directory at path holds one kind of project. a package that is not installed says so and gives the command that installs it",
		Parameters: map[string]any{"type": "object", "required": []string{"package"}, "properties": map[string]any{
			"package": text,
			"name":    text,
			"lang":    map[string]any{"type": "string", "enum": []string{string(Go), string(TS), string(Rust), string(Python)}},
			"path":    text,
		}},
	}
}

type request struct {
	Package string   `json:"package"`
	Name    string   `json:"name"`
	Lang    Language `json:"lang"`
	Path    string   `json:"path"`
}

type installed struct {
	version, source, dir string
	files                []string
}

type declaration struct {
	file           string
	line           int
	signature, doc string
}

type answer struct {
	installed
	decls []declaration
	names []string
	doc   string
}

func (a API) Run(ctx context.Context, raw json.RawMessage) (contract.Result, error) {
	var req request
	if err := json.Unmarshal(raw, &req); err != nil {
		return contract.Result{}, fmt.Errorf("api: arguments are not the expected shape: %w", err)
	}
	if strings.TrimSpace(req.Package) == "" {
		return contract.Failure("api: package is required: the dependency to read, as \"package\": \"github.com/pelletier/go-toml/v2\""), nil
	}
	segments := strings.FieldsFunc(strings.ReplaceAll(req.Name, "::", "."), func(r rune) bool { return r == '.' })
	if len(segments) > 2 {
		return contract.Failure(fmt.Sprintf("api: name %q is one identifier or Container.member", req.Name)), nil
	}
	from, err := a.root.Resolve(cmp.Or(req.Path, "."))
	if err != nil {
		return contract.Failure("api: path " + err.Error()), nil
	}
	lang, err := detect(from, req.Lang)
	if err != nil {
		return contract.Failure("api: " + err.Error()), nil
	}
	var found answer
	var inst installed
	var syn syntax
	switch lang {
	case Go:
		found, err = lookupGo(from, req.Package, segments)
	case TS:
		inst, err = tsInstalled(from, req.Package)
		syn = tsSyntax()
	case Rust:
		inst, err = rustInstalled(from, req.Package)
		syn = rustSyntax()
	case Python:
		inst, err = pythonInstalled(ctx, from, req.Package)
		syn = pythonSyntax()
	}
	if err == nil && lang != Go {
		found, err = syn.scan(inst, segments)
	}
	if err != nil {
		return contract.Failure("api: " + err.Error()), nil
	}
	return a.render(req, found), nil
}

func detect(dir string, asked Language) (Language, error) {
	switch asked {
	case Go, TS, Rust, Python:
		return asked, nil
	case "":
	default:
		return "", fmt.Errorf("lang %q is none of go, ts, rust or python", asked)
	}
	var found []Language
	for _, probe := range []struct {
		lang  Language
		files []string
	}{{Go, []string{"go.mod"}}, {TS, []string{"package.json"}}, {Rust, []string{"Cargo.toml"}}, {Python, []string{"pyproject.toml", "setup.py", "requirements*.txt"}}} {
		for _, file := range probe.files {
			if matches, _ := filepath.Glob(filepath.Join(dir, file)); len(matches) > 0 {
				found = append(found, probe.lang)
				break
			}
		}
	}
	switch len(found) {
	case 0:
		return "", fmt.Errorf("no go.mod, package.json, Cargo.toml, pyproject.toml or requirements file in %s. name the language in lang", dir)
	case 1:
		return found[0], nil
	}
	return "", fmt.Errorf("%s holds %v projects. name one in lang", dir, found)
}

func (a API) render(req request, found answer) contract.Result {
	var b strings.Builder
	fmt.Fprintf(&b, "package: %s\nversion: %s\nsource: %s, %s\n", req.Package, found.version, found.source, found.dir)
	if found.doc != "" {
		fmt.Fprintf(&b, "doc:\n%s\n", clip(found.doc, a.limits.DocBytes))
	}
	if req.Name == "" || len(found.decls) == 0 {
		shown := found.names[:min(len(found.names), a.limits.Names)]
		names := fmt.Sprintf("names (%d, the first %d shown): %s", len(found.names), len(shown), strings.Join(shown, ", "))
		if req.Name == "" {
			return contract.Result{Content: b.String() + names}
		}
		return contract.Failure(fmt.Sprintf("api: %s is not declared in %s %s\n%s%s", req.Name, req.Package, found.version, b.String(), names))
	}
	for _, decl := range found.decls[:min(len(found.decls), a.limits.Matches)] {
		fmt.Fprintf(&b, "\ndeclared: %s:%d\nsignature:\n%s\n", decl.file, decl.line, clip(decl.signature, a.limits.SignatureBytes))
		if decl.doc != "" {
			fmt.Fprintf(&b, "doc:\n%s\n", clip(decl.doc, a.limits.DocBytes))
		}
	}
	if more := len(found.decls) - a.limits.Matches; more > 0 {
		fmt.Fprintf(&b, "\n%d more declarations of %s not shown\n", more, req.Name)
	}
	return contract.Result{Content: strings.TrimRight(b.String(), "\n")}
}

func clip(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return fmt.Sprintf("%s\n... (%d bytes, the first %d shown)", text[:cut], len(text), cut)
}

func upward(from, name string) (string, bool) {
	for dir := from; ; dir = filepath.Dir(dir) {
		if matches, _ := filepath.Glob(filepath.Join(dir, name)); len(matches) > 0 {
			return dir, true
		}
		if filepath.Dir(dir) == dir {
			return "", false
		}
	}
}
