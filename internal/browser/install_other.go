//go:build !windows

package browser

import (
	"os"
	"path/filepath"
	"runtime"
)

func register(string, string) error { return nil }

func unregister(string) error { return nil }

func browserManifests(home string) []string {
	config, always, whenPresent := filepath.Join(home, ".config"), []string{"google-chrome", "chromium"}, []string{"BraveSoftware/Brave-Browser", "microsoft-edge"}
	if runtime.GOOS == "darwin" {
		config, always, whenPresent = filepath.Join(home, "Library", "Application Support"), []string{"Google/Chrome", "Chromium"}, []string{"BraveSoftware/Brave-Browser", "Microsoft Edge"}
	}
	for _, browser := range whenPresent {
		if _, err := os.Stat(filepath.Join(config, browser)); err == nil {
			always = append(always, browser)
		}
	}
	manifests := make([]string, len(always))
	for i, browser := range always {
		manifests[i] = filepath.Join(config, browser, "NativeMessagingHosts", HostName+".json")
	}
	return manifests
}
