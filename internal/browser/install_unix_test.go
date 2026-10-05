//go:build !windows

package browser

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

func scratchHostsKey(*testing.T) string { return ChromeHostsKey }

func TestInstallWritesTheHostManifestForEachBrowserAndUninstallRemovesOnlyIt(t *testing.T) {
	home := t.TempDir()
	config, chrome, chromium, brave, edge := filepath.Join(home, ".config"), "google-chrome", "chromium", "BraveSoftware/Brave-Browser", "microsoft-edge"
	if runtime.GOOS == "darwin" {
		config, chrome, chromium, brave, edge = filepath.Join(home, "Library", "Application Support"), "Google/Chrome", "Chromium", "BraveSoftware/Brave-Browser", "Microsoft Edge"
	}
	hosts := func(browser string) string { return filepath.Join(config, browser, "NativeMessagingHosts") }
	neighbour := filepath.Join(hosts(chromium), "org.other.host.json")
	err := errors.Join(os.MkdirAll(filepath.Join(config, brave), 0o755), os.MkdirAll(hosts(chromium), 0o755))
	if err == nil {
		err = os.WriteFile(neighbour, []byte("{}"), 0o644)
	}
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.Join(hosts(chrome), HostName+".json"),
		filepath.Join(hosts(chromium), HostName+".json"),
		filepath.Join(hosts(brave), HostName+".json"),
	}

	exe, id := filepath.Join(home, "bin", "tofu"), ""
	for range 2 {
		var wrote []string
		if id, wrote, err = Install(home, exe, ChromeHostsKey); err != nil || !slices.Equal(wrote, want) {
			t.Fatalf("Install returned %q, %v; want %q", wrote, err, want)
		}
	}
	_, manifestPath := installPaths(home)
	shipped, err := os.ReadFile(manifestPath)
	var manifest hostManifest
	if err == nil {
		err = json.Unmarshal(shipped, &manifest)
	}
	if err != nil || manifest.Path != exe || !slices.Equal(manifest.AllowedOrigins, []string{"chrome-extension://" + id + "/"}) {
		t.Fatalf("the host manifest is %s, %v; want path %s and the origin of %q", shipped, err, exe, id)
	}
	for _, path := range want {
		if copied, err := os.ReadFile(path); err != nil || string(copied) != string(shipped) {
			t.Fatalf("%s is %s, %v; want %s", path, copied, err, shipped)
		}
	}
	if _, err := os.Stat(filepath.Join(config, edge)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Install made a folder for Edge, which is not installed: %v", err)
	}

	for range 2 {
		if removed, err := Uninstall(home, ChromeHostsKey); err != nil || !slices.Equal(removed, want) {
			t.Fatalf("Uninstall returned %q, %v; want %q", removed, err, want)
		}
	}
	for _, path := range append(want, manifestPath) {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s survived Uninstall: %v", path, err)
		}
	}
	if _, err := os.Stat(neighbour); err != nil {
		t.Fatalf("Uninstall removed another host's manifest: %v", err)
	}
}
