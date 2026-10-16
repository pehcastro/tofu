package browser

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/net/websocket"

	"tofu/internal/konst"
	"tofu/internal/safe"
	"tofu/internal/sys"
)

type Endpoint struct {
	Home     string
	Profile  string
	Headless bool
}

func (e Endpoint) Dial() (*Client, error) {
	if e.Profile == "" {
		return Dial(e.Home)
	}
	dir, err := profileDir(e.Home, e.Profile)
	if err != nil {
		return nil, err
	}
	conn, err := attachChrome(dir)
	if err != nil {
		conn, err = launchChrome(dir, e.Headless)
	}
	if err != nil {
		return nil, err
	}
	return serveDirect(conn, e.Profile)
}

type Profile struct {
	Name    string `json:"name"`
	Dir     string `json:"dir"`
	Running bool   `json:"running"`
}

const (
	devToolsActivePort = "DevToolsActivePort"
	debuggerOrigin     = "http://tofu.invalid"
	LoginURL           = "https://accounts.google.com"
)

func ProfilesDir(home string) string {
	return filepath.Join(sys.StateDir(home), "browser", "profiles")
}

func profileDir(home, name string) (string, error) {
	dir, err := namedDir(home, name)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(dir); err != nil {
		return "", fmt.Errorf("there is no browser profile %q in %s: tofu browser profile new %s", name, ProfilesDir(home), name)
	}
	return dir, nil
}

func namedDir(home, name string) (string, error) {
	plain := name != "" && name[0] != '-' && strings.Trim(name, "abcdefghijklmnopqrstuvwxyz0123456789-") == ""
	if !plain {
		return "", fmt.Errorf("a browser profile name is lowercase letters, digits and hyphens, not %q", name)
	}
	return filepath.Join(ProfilesDir(home), name), nil
}

func NewProfile(home, name string) (Profile, error) {
	dir, err := namedDir(home, name)
	if err != nil {
		return Profile{}, err
	}
	if _, err := os.Stat(dir); err == nil {
		return Profile{}, fmt.Errorf("the browser profile %q already exists in %s", name, dir)
	}
	return Profile{Name: name, Dir: dir}, os.MkdirAll(dir, 0o700)
}

func Profiles(home string) ([]Profile, error) {
	entries, err := os.ReadDir(ProfilesDir(home))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	var profiles []Profile
	for _, entry := range entries {
		if entry.IsDir() {
			dir := filepath.Join(ProfilesDir(home), entry.Name())
			profiles = append(profiles, Profile{Name: entry.Name(), Dir: dir, Running: profileHeld(dir)})
		}
	}
	return profiles, err
}

func StopProfile(home, name string) (bool, error) {
	dir, err := profileDir(home, name)
	if err != nil {
		return false, err
	}
	conn, err := attachChrome(dir)
	if err != nil {
		return false, nil
	}
	_, err = conn.send("", "Browser.close", nil)
	if err != nil {
		_ = conn.ws.Close()
	}
	<-conn.done
	for until := time.Now().Add(konst.BrowserCallTimeoutMillis * time.Millisecond); profileHeld(dir) && time.Now().Before(until); {
		time.Sleep(konst.BrowserSettleTickMillis * time.Millisecond)
	}
	return true, err
}

func RemoveProfile(home, name string) error {
	dir, err := profileDir(home, name)
	if err == nil {
		_, err = StopProfile(home, name)
	}
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

func LoginProfile(home, name, url string) error {
	dir, err := profileDir(home, name)
	if err != nil {
		return err
	}
	if profileHeld(dir) {
		return fmt.Errorf("a Chrome already runs on the browser profile %q: tofu browser profile stop %s, then log in", name, name)
	}
	chrome, err := findChrome()
	if err != nil {
		return err
	}
	command := exec.Command(chrome, "--user-data-dir="+dir, "--no-first-run", "--no-default-browser-check", url)
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}

func attachChrome(dir string) (*cdpConn, error) {
	raw, err := os.ReadFile(filepath.Join(dir, devToolsActivePort))
	port, path, written := strings.Cut(strings.TrimSpace(string(raw)), "\n")
	if err != nil || !written {
		return nil, fmt.Errorf("no Chrome runs on the profile in %s", dir)
	}
	address := net.JoinHostPort("127.0.0.1", strings.TrimSpace(port))
	conn, err := net.DialTimeout("tcp", address, konst.BrowserDialTimeoutMillis*time.Millisecond)
	if err != nil {
		return nil, err
	}
	_ = conn.Close()
	ws, err := websocket.Dial("ws://"+address+strings.TrimSpace(path), "", debuggerOrigin)
	if err != nil {
		return nil, err
	}
	return newCDPConn(ws), nil
}

func launchChrome(dir string, headless bool) (*cdpConn, error) {
	if profileHeld(dir) {
		return nil, fmt.Errorf("a Chrome window holds the profile in %s, from tofu browser profile login: close it", dir)
	}
	chrome, err := findChrome()
	if err == nil {
		err = os.Remove(filepath.Join(dir, devToolsActivePort))
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	args := []string{"--user-data-dir=" + dir, "--remote-debugging-port=0", "--remote-allow-origins=" + debuggerOrigin, "--no-first-run", "--no-default-browser-check"}
	if headless {
		args = append(args, "--headless=new")
	}
	command := exec.Command(chrome, append(args, "about:blank")...)
	if err := command.Start(); err != nil {
		return nil, err
	}
	exited := make(chan error, 1)
	report := sys.Crashes(".")
	safe.Go(report, "chrome wait", func() { exited <- safe.Call(report, "chrome wait", command.Wait) })
	for until := time.Now().Add(konst.BrowserCallTimeoutMillis * time.Millisecond); time.Now().Before(until); {
		if conn, err := attachChrome(dir); err == nil {
			return conn, nil
		}
		select {
		case err := <-exited:
			return nil, fmt.Errorf("%s exited before it opened a debugging port on %s: %v", chrome, dir, err)
		case <-time.After(konst.BrowserSettleTickMillis * time.Millisecond):
		}
	}
	return nil, errors.Join(fmt.Errorf("%s opened no debugging port on %s within %d ms, so tofu stopped it", chrome, dir, konst.BrowserCallTimeoutMillis), command.Process.Kill())
}

func profileHeld(dir string) bool {
	if _, err := os.Lstat(filepath.Join(dir, "SingletonLock")); err == nil {
		return true
	}
	err := os.Remove(filepath.Join(dir, "lockfile"))
	return err != nil && !errors.Is(err, os.ErrNotExist)
}

func findChrome() (string, error) {
	candidates := map[string][]string{
		"windows": {
			filepath.Join(os.Getenv("ProgramFiles"), "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(os.Getenv("ProgramFiles(x86)"), "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(os.Getenv("LOCALAPPDATA"), "Google", "Chrome", "Application", "chrome.exe"),
		},
		"darwin": {
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			filepath.Join(os.Getenv("HOME"), "Applications", "Google Chrome.app", "Contents", "MacOS", "Google Chrome"),
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		},
		"linux": {"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"},
	}[runtime.GOOS]
	for _, candidate := range candidates {
		if found, err := exec.LookPath(candidate); err == nil && filepath.IsAbs(found) {
			return found, nil
		}
	}
	return "", fmt.Errorf("no installed Chrome found on %s: tofu looked for %s", runtime.GOOS, strings.Join(candidates, ", "))
}
