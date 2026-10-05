package shell

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"tofu/internal/konst"
	"tofu/internal/sys"
)

type Choice struct {
	Path  string
	Label string
	Note  string
}

const powerShellUTF8Output = "try { [Console]::OutputEncoding=[System.Text.Encoding]::UTF8 } catch {}\n"

func (c Choice) Command(ctx context.Context, dir, command string, extraEnv ...string) *exec.Cmd {
	args := []string{"-c", command}
	switch strings.TrimSuffix(strings.ToLower(filepath.Base(c.Path)), ".exe") {
	case "pwsh", "powershell":
		args = []string{"-NoProfile", "-NonInteractive", "-Command", powerShellUTF8Output + command}
	}
	cmd := exec.CommandContext(ctx, c.Path, args...)
	cmd.Dir = dir
	cmd.Env = slices.DeleteFunc(os.Environ(), func(entry string) bool {
		name, _, _ := strings.Cut(entry, "=")
		return slices.ContainsFunc(sys.KeyNames(), func(key string) bool { return strings.EqualFold(name, key) })
	})
	for _, entry := range []string{"AI_AGENT=tofu", "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never", "EDITOR=false"} {
		if name, _, _ := strings.Cut(entry, "="); os.Getenv(name) == "" {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_EDITOR=true", "GIT_SEQUENCE_EDITOR=true")
	cmd.Env = append(cmd.Env, extraEnv...)
	return cmd
}

const posixSyntaxDoesNotApply = "this project's tools assume a posix shell: heredocs, $VAR, forward slashes and /dev/null do not work here, " +
	"and && and || are a parse error in Windows PowerShell 5.1"

func Resolve(setting string) (Choice, error) {
	if setting != "" {
		return resolveOverride(setting, "the shell setting")
	}
	if override := os.Getenv("TOFU_SHELL"); override != "" {
		return resolveOverride(override, "TOFU_SHELL")
	}
	if runtime.GOOS == "windows" {
		return resolveWindows()
	}
	return resolvePosix()
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func family(path string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), konst.ShellProbeTimeoutMillis*time.Millisecond)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "-c", "uname -o").Output()
	return strings.TrimSpace(string(out)), err
}

func isWSL(family string) bool { return strings.Contains(family, "GNU/Linux") }

func resolveOverride(setting, source string) (Choice, error) {
	path := setting
	if setting == "wsl" {
		found, err := exec.LookPath("bash")
		if err != nil {
			return Choice{}, fmt.Errorf("bash: %s=wsl but no bash is on PATH: %w", source, err)
		}
		path = found
	}
	if _, err := os.Stat(path); err != nil {
		return Choice{}, fmt.Errorf("bash: %s is set to %q and it does not exist: %w", source, path, err)
	}
	named, err := family(path)
	if err != nil {
		return Choice{}, fmt.Errorf("bash: %s is set to %q and it would not run: %w", source, path, err)
	}
	if isWSL(named) {
		return Choice{Path: path, Label: "wsl bash on " + named,
			Note: "wsl, chosen on purpose through " + source + ": its filesystem is separate from Windows and its PATH will not see software installed only on Windows"}, nil
	}
	return Choice{Path: path, Label: filepath.Base(path) + " on " + named}, nil
}

func gitBashCandidates() []string {
	var candidates []string
	if git, err := exec.LookPath("git"); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(filepath.Dir(git)), "bin", "bash.exe"), filepath.Join(filepath.Dir(git), "bash.exe"))
	}
	for _, root := range [][2]string{
		{"ProgramFiles", "Git"},
		{"ProgramFiles(x86)", "Git"},
		{"LOCALAPPDATA", `Programs\Git`},
		{"GIT_INSTALL_ROOT", ""},
		{"SCOOP", `apps\git\current`},
		{"USERPROFILE", `scoop\apps\git\current`},
	} {
		if base := os.Getenv(root[0]); base != "" {
			candidates = append(candidates, filepath.Join(base, root[1], "bin", "bash.exe"))
		}
	}
	return candidates
}

func looksLikeWSLPath(path string) bool {
	lower := strings.ToLower(path)
	return strings.Contains(lower, `\system32\bash.exe`) || strings.Contains(lower, `windowsapps\bash.exe`)
}

func resolveWindows() (Choice, error) {
	var rejected []string
	for _, bash := range gitBashCandidates() {
		if !exists(bash) {
			continue
		}
		if named, err := family(bash); err == nil {
			return Choice{Path: bash, Label: "git bash on " + named}, nil
		}
		rejected = append(rejected, bash+" (found but would not run)")
	}
	if len(rejected) == 0 {
		rejected = append(rejected, "git bash (not beside git on PATH, nor under Program Files, LOCALAPPDATA, GIT_INSTALL_ROOT or scoop)")
	}
	if path, err := exec.LookPath("bash"); err == nil {
		named, probeErr := family(path)
		if probeErr == nil && !isWSL(named) && !looksLikeWSLPath(path) {
			return Choice{Path: path, Label: "bash on " + named}, nil
		}
		rejected = append(rejected, path+" (this is wsl, a separate filesystem namespace with its own PATH; set TOFU_SHELL=wsl to use it on purpose)")
	}
	for _, name := range []string{"pwsh", "powershell"} {
		if path, err := exec.LookPath(name); err == nil {
			return Choice{Path: path, Label: name + ", " + posixSyntaxDoesNotApply,
				Note: "no posix shell was found (tried: " + strings.Join(rejected, "; ") + "). " + posixSyntaxDoesNotApply}, nil
		}
	}
	return Choice{}, fmt.Errorf(
		"bash: no shell resolved. tried %s, and neither pwsh nor powershell is on PATH either. "+
			"fixes: install Git for Windows, set TOFU_SHELL to a shell's path, or set TOFU_SHELL=wsl",
		strings.Join(rejected, "; "))
}

func resolvePosix() (Choice, error) {
	for _, path := range []string{os.Getenv("SHELL"), "/bin/sh"} {
		if path != "" && exists(path) {
			named, _ := family(path)
			return Choice{Path: path, Label: filepath.Base(path) + " on " + cmp.Or(named, runtime.GOOS)}, nil
		}
	}
	return Choice{}, errors.New(
		"bash: $SHELL is not set and /bin/sh does not exist. fixes: set $SHELL to your shell, or install a posix shell at /bin/sh")
}
