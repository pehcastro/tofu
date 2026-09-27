package turn

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"tofu/internal/konst"
	"tofu/internal/shell"
)

const (
	tsconfigName        = "tsconfig.json"
	erasableSyntaxError = "error TS1294:"
	typeStrippingNote   = " node runs this project's .ts files with type stripping, which refuses enum, parameter properties and namespaces that hold values"
)

type packageManifest struct {
	Scripts        map[string]string `json:"scripts"`
	PackageManager string            `json:"packageManager"`
}

func Typechecked(ctx context.Context, resolved, result string) string {
	if ext := filepath.Ext(resolved); ext != ".ts" && ext != ".tsx" {
		return result
	}
	return result + "\n\n" + typecheck(ctx, resolved)
}

func typecheck(ctx context.Context, resolved string) string {
	tsconfig, found := findUp(filepath.Dir(resolved), tsconfigName)
	if !found {
		return "typecheck skipped: no tsconfig.json in " + filepath.Dir(resolved) + " or above it"
	}
	dir := filepath.Dir(tsconfig)
	if _, installed := findUp(dir, "node_modules", "typescript", "package.json"); !installed {
		return "typecheck skipped: typescript is not installed under node_modules, and the check never downloads it"
	}
	var manifest packageManifest
	if at, found := findUp(dir, "package.json"); found {
		if body, err := os.ReadFile(at); err == nil {
			_ = json.Unmarshal(body, &manifest)
		}
	}
	manager := lockfileManager(dir, manifest)
	var argv []string
	switch manager {
	case "bun":
		argv = []string{"bun", "x", "tsc"}
	case "pnpm":
		argv = []string{"pnpm", "exec", "tsc"}
	case "yarn":
		argv = []string{"yarn", "tsc"}
	case "npm":
		argv = []string{"npm", "exec", "--", "tsc"}
	default:
		return "typecheck skipped: packageManager names " + manager + ", and the check runs tsc only through bun, pnpm, yarn or npm"
	}
	argv = append(argv, "--noEmit", "--pretty", "false", "-p", tsconfigName)
	if runsTypeScript(manifest) {
		argv = append(argv, "--erasableSyntaxOnly")
	}
	command := strings.Join(argv, " ")

	ctx, cancel := context.WithTimeout(ctx, konst.TypecheckDeadlineMillis*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir, cmd.WaitDelay = dir, konst.BashWaitDelayMillis*time.Millisecond
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	started := time.Now()
	tracked, err := shell.StartTracked(cmd)
	if err != nil {
		return fmt.Sprintf("typecheck skipped: %s did not start: %v", command, err)
	}
	defer tracked.Release()
	failed := cmd.Wait() != nil
	took := time.Since(started).Milliseconds()
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return fmt.Sprintf("typecheck skipped: %s ran past its %d ms limit", command, konst.TypecheckDeadlineMillis)
	case ctx.Err() != nil:
		return "typecheck skipped: the turn was cancelled while " + command + " ran"
	}
	relative, _ := filepath.Rel(dir, resolved)
	return tscReport(output.String(), filepath.ToSlash(relative), fmt.Sprintf("typecheck: %s, %d ms", command, took), failed)
}

func lockfileManager(dir string, manifest packageManifest) string {
	for _, lock := range [][2]string{{"bun.lock", "bun"}, {"bun.lockb", "bun"}, {"pnpm-lock.yaml", "pnpm"}, {"yarn.lock", "yarn"}, {"package-lock.json", "npm"}} {
		if _, found := findUp(dir, lock[0]); found {
			return lock[1]
		}
	}
	if manifest.PackageManager != "" {
		name, _, _ := strings.Cut(manifest.PackageManager, "@")
		return name
	}
	return "npm"
}

func runsTypeScript(manifest packageManifest) bool {
	for _, script := range manifest.Scripts {
		for _, command := range strings.FieldsFunc(script, func(r rune) bool { return r == '&' || r == '|' || r == ';' }) {
			words := strings.Fields(command)
			if len(words) == 0 || words[0] != "node" {
				continue
			}
			for _, word := range words[1:] {
				if ext := filepath.Ext(word); ext == ".ts" || ext == ".mts" || ext == ".cts" {
					return true
				}
			}
		}
	}
	return false
}

func tscReport(output, file, head string, failed bool) string {
	var mine, global, unread []string
	errorsHere, elsewhere, inMine := 0, 0, false
	for _, line := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		continued := strings.HasPrefix(line, " ")
		switch {
		case strings.TrimSpace(line) == "":
		case continued && inMine:
			mine = append(mine, line)
		case strings.HasPrefix(line, file+"("):
			if strings.Contains(line, erasableSyntaxError) {
				line += typeStrippingNote
			}
			mine, errorsHere = append(mine, line), errorsHere+1
		case strings.HasPrefix(line, "error TS"):
			global = append(global, line)
		case strings.Contains(line, "): error TS"):
			elsewhere++
		case !continued:
			unread = append(unread, line)
		}
		inMine = strings.HasPrefix(line, file+"(") || continued && inMine
	}
	lines := slices.Concat(global, mine)
	summary := fmt.Sprintf("%s, errors in %s: %d, in other files: %d", head, file, errorsHere, elsewhere)
	if failed && len(lines) == 0 && elsewhere == 0 {
		lines, summary = unread, head+", tsc failed and named no file"
	}
	if len(lines) == 0 {
		return summary
	}
	shown := lines[:min(len(lines), konst.TypecheckLinesCap)]
	if cut := len(lines) - len(shown); cut > 0 {
		shown = append(shown, fmt.Sprintf("(%d more lines not shown)", cut))
	}
	return summary + ":\n" + strings.Join(shown, "\n")
}
