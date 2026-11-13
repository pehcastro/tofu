package format

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	goformat "go/format"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"tofu/internal/models/llm/core"
	platformfs "tofu/internal/platform/fs"
	"tofu/internal/platform/outside"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/history/shadow"
)

const Name = "format"

const rustEditionUnstated = "2015"

type outcome int

const (
	wrote outcome = iota
	unchanged
	skipped
	notFormatted
)

type Format struct {
	root  contract.Root
	store shadow.Store
	run   time.Duration
}

func New(dir string, store shadow.Store, run time.Duration) (contract.Tool, error) {
	root, err := contract.NewRoot(dir)
	return Format{root: root, store: store, run: run}, err
}

func (Format) Name() string { return Name }

func (Format) Definition() core.Tool {
	return core.Tool{
		Name: Name,
		Description: "runs the project's own formatter over named files and nothing else: gofmt for go, rustfmt for rust, prettier for typescript, javascript, json, css and markdown, ruff for python. " +
			"use it instead of gofmt -w ., prettier --write . or cargo fmt through bash, which reformat files the person wrote. " +
			"paths are files under the working directory; left out, it formats the files this turn changed, read from the shadow store tofu commits at every turn's start and end. " +
			"each file is reported formatted, unchanged, skipped or not formatted with the reason; a formatter that is not installed is named with how to install it",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"paths": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			},
		},
	}
}

func (f Format) Run(ctx context.Context, raw json.RawMessage) (contract.Result, error) {
	var args struct {
		Paths []string `json:"paths"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return contract.Result{}, fmt.Errorf("format: arguments are not the expected shape: %w", err)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, f.run)
	defer cancel()
	paths, from := args.Paths, "the paths given"
	if len(paths) == 0 {
		var err error
		if paths, from, err = f.touched(ctx); err != nil {
			return contract.Result{}, fmt.Errorf("format: %w", err)
		}
	}
	if len(paths) == 0 {
		return contract.Result{Content: "format: no file to format from " + from + ". name the files in paths\n", Command: from}, nil
	}
	var said strings.Builder
	formatted, failed := 0, false
	for _, path := range paths {
		line, result := f.one(ctx, path)
		switch result {
		case wrote:
			formatted++
		case notFormatted:
			failed = true
		case unchanged, skipped:
		}
		said.WriteString(line + " " + path + "\n")
	}
	content := fmt.Sprintf("format: formatted %d of %d files from %s\n", formatted, len(paths), from) + said.String()
	if failed {
		return contract.Failure(content), nil
	}
	return contract.Result{Content: content, Command: strings.Join(paths, " ")}, nil
}

func (f Format) touched(ctx context.Context) ([]string, string, error) {
	var turn shadow.Changed
	from := "this turn"
	if started, inTurn := shadow.StartedIn(ctx); inTurn {
		live, err := started.Live(ctx)
		if err != nil {
			return nil, "", err
		}
		turn = live
	} else {
		turns, err := f.store.Turns(ctx, 1)
		if err != nil || len(turns) == 0 {
			return nil, "the shadow store at " + f.store.Dir() + ", which holds no turn yet", err
		}
		turn, from = turns[0], "turn "+turns[0].Turn.ID+" by agent "+turns[0].Turn.Agent+", the newest turn in this project whoever ran it, not necessarily yours"
	}
	var paths []string
	for _, file := range turn.Files {
		if !file.Binary {
			paths = append(paths, file.Path)
		}
	}
	return paths, from, nil
}

func (f Format) one(ctx context.Context, requested string) (string, outcome) {
	path, err := f.root.Resolve(requested)
	if err != nil {
		return "not formatted, " + err.Error() + ":", notFormatted
	}
	info, err := os.Stat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return "skipped, it does not exist:", skipped
	case err != nil:
		return "not formatted, " + err.Error() + ":", notFormatted
	case info.IsDir():
		return "skipped, a directory, and format runs only on the files it names:", skipped
	}
	before, err := os.ReadFile(path)
	if err != nil {
		return "not formatted, " + err.Error() + ":", notFormatted
	}
	tool, after, err := formatted(ctx, path, before)
	switch {
	case tool == "":
		return "skipped, no formatter for " + filepath.Ext(path) + " files:", skipped
	case err != nil:
		return "not formatted, " + err.Error() + ":", notFormatted
	case bytes.Equal(before, after):
		return "unchanged by " + tool + ":", unchanged
	}
	if err := platformfs.WriteFile(path, after, info.Mode().Perm()); err != nil {
		return "not formatted, " + err.Error() + ":", notFormatted
	}
	return "formatted by " + tool + ":", wrote
}

func formatted(ctx context.Context, path string, before []byte) (string, []byte, error) {
	dir := filepath.Dir(path)
	var tool, install string
	var candidates, args []string
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		after, err := goformat.Source(before)
		if err != nil {
			return "gofmt", nil, fmt.Errorf("gofmt did not format it, %w", err)
		}
		return "gofmt", after, nil
	case ".rs":
		tool, install, args = "rustfmt", "it is not on PATH. install it with rustup component add rustfmt", []string{"--edition", edition(dir)}
	case ".ts", ".tsx", ".mts", ".cts", ".js", ".jsx", ".mjs", ".cjs", ".json", ".css", ".scss", ".html", ".vue", ".md", ".yaml", ".yml":
		tool, install, args = "prettier", "it is not under node_modules in this directory or above it, nor on PATH. install it with npm install --save-dev prettier", []string{"--stdin-filepath", path}
		if found, ok := contract.FindUp(dir, "node_modules", "prettier", "package.json"); ok {
			candidates = append(candidates, filepath.Join(filepath.Dir(filepath.Dir(found)), ".bin", "prettier"))
		}
	case ".py", ".pyi":
		tool, install, args = "ruff", "it is not in the project's .venv nor on PATH. install it with pip install ruff, uv tool install ruff, or brew install ruff on macOS", []string{"format", "--stdin-filename", path}
		if found, ok := contract.FindUp(dir, ".venv", "pyvenv.cfg"); ok {
			candidates = append(candidates, filepath.Join(filepath.Dir(found), "Scripts", "ruff"), filepath.Join(filepath.Dir(found), "bin", "ruff"))
		}
	default:
		return "", nil, nil
	}
	program := ""
	for _, candidate := range append(candidates, tool) {
		if found, err := exec.LookPath(candidate); err == nil {
			program = found
			break
		}
	}
	if program == "" {
		return tool, nil, errors.New(tool + " is not installed: " + install + ", then call format again")
	}
	command := outside.Command(ctx, program, args...)
	var stdout, stderr bytes.Buffer
	command.Dir, command.Stdin, command.Stdout, command.Stderr = dir, bytes.NewReader(before), &stdout, &stderr
	if err := command.Run(); err != nil {
		said, _, _ := strings.Cut(strings.TrimSpace(stderr.String()), "\n")
		return tool, nil, fmt.Errorf("%s did not format it, %v: %s", tool, err, said)
	}
	return tool, stdout.Bytes(), nil
}

func edition(dir string) string {
	manifest, found := contract.FindUp(dir, "Cargo.toml")
	if !found {
		return rustEditionUnstated
	}
	text, err := os.ReadFile(manifest)
	if err != nil {
		return rustEditionUnstated
	}
	for lines := bufio.NewScanner(bytes.NewReader(text)); lines.Scan(); {
		if key, value, ok := strings.Cut(lines.Text(), "="); ok && strings.TrimSpace(key) == "edition" {
			return strings.Trim(strings.TrimSpace(value), `"'`)
		}
	}
	return rustEditionUnstated
}
