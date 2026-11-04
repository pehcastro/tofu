package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"tofu/internal/sys"
)

const (
	childProcs = "4"
	panicMark  = "FLOWS-PANIC"
)

type seam struct {
	pattern, replacement string
}

var seams = []seam{
	{`SubAgentWallClockSeconds\s*=\s*\d+`, "SubAgentWallClockSeconds = 5"},
	{`SubAgentAskMillis\s*=\s*\d+`, "SubAgentAskMillis = 8000"},
	{`CronMinIntervalSeconds\s*=\s*\d+`, "CronMinIntervalSeconds = 10"},
	{`-konst\.SettledSubAgentFoldMinutes \* time\.Minute`, "-20 * time.Second"},
	{`(\treply, err := c\.take\(request\)\r?\n\tif err != nil \{\r?\n\t\treturn core\.Decision\{\}, err\r?\n\t\}\r?\n)`,
		"${1}\tif strings.HasPrefix(reply.decision.Content, \"" + panicMark + "\") {\n\t\tpanic(reply.decision.Content)\n\t}\n"},
}

func build(src, work string, seamed bool) (string, error) {
	name := "product"
	if seamed {
		name = "seamed"
	}
	exe := filepath.Join(work, name, "tofu")
	if os.PathSeparator == '\\' {
		exe += ".exe"
	}
	files, err := goSources(src)
	if err != nil {
		return "", err
	}
	product, err := locate(files, "(?m)"+sys.ProductSymbol)
	if err != nil {
		return "", err
	}
	target, err := filepath.Rel(src, filepath.Dir(product))
	if err != nil {
		return "", err
	}
	target = "./" + filepath.ToSlash(target)
	args := []string{"build", "-trimpath", "-p", childProcs, "-o", exe}
	if seamed {
		compiled, err := productSources(src, target)
		if err != nil {
			return "", err
		}
		overlay, err := writeOverlay(compiled, filepath.Join(work, name))
		if err != nil {
			return "", err
		}
		args = append(args, "-overlay", overlay)
	}
	cmd := exec.Command("go", append(args, target)...)
	cmd.Dir, cmd.Env = src, append(os.Environ(), "GOMAXPROCS="+childProcs)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("go build %s in %s: %v: %s", name, src, err, out)
	}
	return exe, nil
}

func productSources(src, target string) ([]string, error) {
	cmd := exec.Command("go", "list", "-deps", "-f", `{{if .Module}}{{if .Module.Main}}{{$dir := .Dir}}{{range .GoFiles}}{{$dir}}{{"\t"}}{{.}}{{"\n"}}{{end}}{{end}}{{end}}`, target)
	cmd.Dir, cmd.Env = src, append(os.Environ(), "GOMAXPROCS="+childProcs)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list -deps %s in %s: %w", target, src, err)
	}
	var files []string
	for line := range strings.Lines(string(out)) {
		dir, file, _ := strings.Cut(strings.TrimSpace(line), "\t")
		files = append(files, filepath.Join(dir, file))
	}
	return files, nil
}

func goSources(src string) ([]string, error) {
	var files []string
	for _, root := range sys.GoRoots() {
		dir := filepath.Join(src, filepath.FromSlash(root))
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			continue
		}
		err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
			switch {
			case err != nil:
				return err
			case entry.IsDir() && entry.Name() == "testdata":
				return filepath.SkipDir
			case !entry.IsDir() && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go"):
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return files, nil
}

func locate(files []string, pattern string) (string, error) {
	expression := regexp.MustCompile(pattern)
	var found []string
	for _, path := range files {
		body, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		if expression.Match(body) {
			found = append(found, path)
		}
	}
	if len(found) != 1 {
		return "", fmt.Errorf("symbol %s is in %d Go files, not one, so the build cannot be made: %v", pattern, len(found), found)
	}
	return found[0], nil
}

func writeOverlay(files []string, dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	patched := map[string]string{}
	for _, each := range seams {
		from, err := locate(files, each.pattern)
		if err != nil {
			return "", err
		}
		body, err := readPatched(patched, from)
		if err != nil {
			return "", err
		}
		patched[from] = regexp.MustCompile(each.pattern).ReplaceAllString(body, each.replacement)
	}
	replace := map[string]string{}
	for from, body := range patched {
		to := filepath.Join(dir, filepath.Base(from))
		if err := os.WriteFile(to, []byte(body), 0o644); err != nil {
			return "", err
		}
		replace[from] = to
	}
	overlay := filepath.Join(dir, "overlay.json")
	written, err := json.Marshal(map[string]any{"Replace": replace})
	if err != nil {
		return "", err
	}
	return overlay, os.WriteFile(overlay, written, 0o644)
}

func readPatched(patched map[string]string, path string) (string, error) {
	if body, held := patched[path]; held {
		return body, nil
	}
	body, err := os.ReadFile(path)
	return string(body), err
}
