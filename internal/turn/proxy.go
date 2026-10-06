package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"tofu/internal/llm"
)

const (
	proxySheetPath    = "tools/shell/proxy.yaml"
	projectFilterFile = ".rtk/filters.toml"
	proxyUseOff       = "off"
	proxyUseRTK       = "rtk"
)

type CommandProxy struct {
	binary  string
	root    string
	timeout time.Duration
}

func LoadCommandProxy(files fs.FS, origin, root string) (*CommandProxy, error) {
	body, err := fs.ReadFile(files, proxySheetPath)
	if err != nil {
		return nil, nil
	}
	at := origin + "/" + proxySheetPath
	values := map[string]string{}
	for index, raw := range strings.Split(strings.ReplaceAll(string(body), "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, split := strings.Cut(line, ":")
		if !split {
			return nil, fmt.Errorf("turn: %s line %d expects key: value, found %q", at, index+1, line)
		}
		key = strings.TrimSpace(key)
		if key != "use" && key != "timeout_ms" {
			return nil, fmt.Errorf("turn: %s line %d: %s is not a field of this file", at, index+1, key)
		}
		values[key] = strings.Trim(strings.TrimSpace(value), `"`)
	}
	switch values["use"] {
	case "", proxyUseOff:
		return nil, nil
	case proxyUseRTK:
	default:
		return nil, fmt.Errorf("turn: %s: use has to be %s or %s, found %q", at, proxyUseOff, proxyUseRTK, values["use"])
	}
	millis, err := strconv.Atoi(values["timeout_ms"])
	if err != nil || millis <= 0 {
		return nil, fmt.Errorf("turn: %s: timeout_ms has to be a positive whole number, found %q", at, values["timeout_ms"])
	}
	found, _ := exec.LookPath(proxyUseRTK)
	return &CommandProxy{binary: found, root: root, timeout: time.Duration(millis) * time.Millisecond}, nil
}

type ProxyRow struct {
	Proxy      string `json:"proxy"`
	Asked      string `json:"asked"`
	Ran        string `json:"ran,omitempty"`
	ProxyBytes int    `json:"proxy_bytes,omitempty"`
	Note       string `json:"note,omitempty"`
}

func (p *CommandProxy) rewrite(ctx context.Context, call llm.ToolCall) (json.RawMessage, *ProxyRow) {
	var args bashArgs
	if p == nil || call.Name != bashToolName || json.Unmarshal(call.Arguments, &args) != nil || strings.TrimSpace(args.Command) == "" {
		return call.Arguments, nil
	}
	row := &ProxyRow{Proxy: proxyUseRTK, Asked: args.Command}
	if args.Background || strings.Contains(strings.ReplaceAll(args.Command, "||", ""), "|") || strings.Contains(args.Command, ">") {
		return call.Arguments, row
	}
	if p.binary == "" {
		row.Note = proxyUseRTK + " is not on PATH, so the command ran as it was asked for"
		return call.Arguments, row
	}
	if _, err := os.Stat(filepath.Join(p.root, filepath.FromSlash(projectFilterFile))); err == nil {
		row.Note = proxyUseRTK + " was told to stand aside: this checkout ships " + projectFilterFile +
			", which rewrites command output arbitrarily, and that trust is not inherited from a repository"
		return call.Arguments, row
	}
	rewritten, err := p.ask(ctx, args.Command)
	if err != nil {
		row.Note = proxyUseRTK + " was asked to rewrite the command and could not: " + err.Error()
		return call.Arguments, row
	}
	if rewritten == "" || rewritten == args.Command {
		return call.Arguments, row
	}
	if verb, reason := refusedVerb(rewritten); verb != "" {
		row.Note = "the command ran as it was asked for, without " + proxyUseRTK + ": " + proxyUseRTK + " " + verb + " would " + reason + ". its output is whole and was not filtered"
		return call.Arguments, row
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(call.Arguments, &fields)
	fields["command"], _ = json.Marshal(rewritten)
	proxied, _ := json.Marshal(fields)
	row.Ran = rewritten
	return proxied, row
}

func (p *CommandProxy) ask(ctx context.Context, command string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, p.binary, "rewrite", command).Output()
	if rewritten := strings.TrimSpace(string(out)); rewritten != "" {
		return rewritten, nil
	}
	if ctx.Err() != nil {
		return "", fmt.Errorf("it did not answer within %s", p.timeout)
	}
	var declined *exec.ExitError
	if err != nil && !errors.As(err, &declined) {
		return "", err
	}
	return "", nil
}

func (p *CommandProxy) InstalledVersion(ctx context.Context) (string, error) {
	if p.binary == "" {
		return "", nil
	}
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, p.binary, "--version").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimPrefix(strings.TrimSpace(string(out)), proxyUseRTK+" "), nil
}

func refusedVerb(rewritten string) (string, string) {
	fields := strings.Fields(rewritten)
	for index, field := range fields {
		if field != proxyUseRTK || index+1 == len(fields) {
			continue
		}
		switch verb := fields[index+1]; {
		case verb == "tsc" || verb == "npx":
			return verb, "download a compiler when the project has none, which is the one network fetch in it"
		case verb == "ping":
			return verb, "drop every reply line and keep the last 4 behind a (N lines omitted) marker, and on Windows read ping's OEM code page bytes as UTF-8, writing U+FFFD for every accented letter"
		case verb == "tree" && runtime.GOOS == "windows":
			return verb, "find no tree on a Windows shell's PATH and print nothing with exit 0, where tree alone says not found"
		}
	}
	return "", ""
}

func proxyPanicked(content string) bool {
	return strings.Contains(content, "thread '") && strings.Contains(content, "panicked at")
}
