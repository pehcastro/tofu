package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
)

const (
	childProcs = "4"
	panicMark  = "FLOWS-PANIC"
)

type seam struct {
	file, pattern, replacement string
}

var seams = []seam{
	{"internal/konst/konst.go", `SubAgentWallClockSeconds\s*=\s*\d+`, "SubAgentWallClockSeconds = 5"},
	{"internal/konst/konst.go", `SubAgentAskMillis\s*=\s*\d+`, "SubAgentAskMillis = 8000"},
	{"interface/tui/turn.go", `-konst\.SettledSubAgentFoldMinutes \* time\.Minute`, "-20 * time.Second"},
	{"cmd/tofu/drive.go", `(\treply, err := c\.take\(request\)\r?\n\tif err != nil \{\r?\n\t\treturn llm\.Decision\{\}, err\r?\n\t\}\r?\n)`,
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
	args := []string{"build", "-trimpath", "-p", childProcs, "-o", exe}
	if seamed {
		overlay, err := writeOverlay(src, filepath.Join(work, name))
		if err != nil {
			return "", err
		}
		args = append(args, "-overlay", overlay)
	}
	cmd := exec.Command("go", append(args, "./cmd/tofu")...)
	cmd.Dir, cmd.Env = src, append(os.Environ(), "GOMAXPROCS="+childProcs)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("go build %s in %s: %v: %s", name, src, err, out)
	}
	return exe, nil
}

func writeOverlay(src, dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	patched := map[string]string{}
	for _, each := range seams {
		from := filepath.Join(src, filepath.FromSlash(each.file))
		body, err := readPatched(patched, from)
		if err != nil {
			return "", err
		}
		expression := regexp.MustCompile(each.pattern)
		if !expression.MatchString(body) {
			return "", fmt.Errorf("seam %q is not in %s, so the seamed build cannot be made", each.pattern, from)
		}
		patched[from] = expression.ReplaceAllString(body, each.replacement)
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
