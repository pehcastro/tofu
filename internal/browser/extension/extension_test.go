package extension_test

import (
	"encoding/base64"
	"encoding/json"
	"io/fs"
	"strings"
	"testing"

	"tofu/internal/browser"
	"tofu/internal/browser/extension"
)

const (
	pinnedExtensionID = "jednanpboiikklhkkkimnmdmjmgjgphh"
	emDash            = rune(0x2014)
)

func shipped(t *testing.T, name string) string {
	t.Helper()
	raw, err := extension.Files.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestManifestKeepsTheIDAndNamesEveryShippedFile(t *testing.T) {
	var manifest struct {
		Key        string   `json:"key"`
		Version    string   `json:"version"`
		Permission []string `json:"permissions"`
		Background struct {
			ServiceWorker string `json:"service_worker"`
		} `json:"background"`
		Action struct {
			Popup string `json:"default_popup"`
		} `json:"action"`
	}
	if err := json.Unmarshal([]byte(shipped(t, "manifest.json")), &manifest); err != nil {
		t.Fatal(err)
	}
	der, err := base64.StdEncoding.DecodeString(manifest.Key)
	if err != nil {
		t.Fatal(err)
	}
	if id := browser.ExtensionID(der); id != pinnedExtensionID {
		t.Fatalf("the extension id is %s, want %s", id, pinnedExtensionID)
	}
	if strings.Join(manifest.Permission, " ") != "nativeMessaging debugger tabs activeTab" || manifest.Version == "0.1.0" {
		t.Fatalf("permissions %v and version %s; want the four permissions kept and the version bumped", manifest.Permission, manifest.Version)
	}
	if manifest.Background.ServiceWorker != "background.js" || manifest.Action.Popup != "popup.html" {
		t.Fatalf("the service worker is %q and the popup %q", manifest.Background.ServiceWorker, manifest.Action.Popup)
	}
	names, err := fs.Glob(extension.Files, "*")
	if err != nil || strings.Join(names, " ") != "background.js manifest.json popup.html popup.js snapshot.js" {
		t.Fatalf("the extension ships %v, %v", names, err)
	}
	if !strings.Contains(shipped(t, "popup.html"), `<script src="popup.js">`) {
		t.Fatal("popup.html does not load popup.js")
	}
}

func TestBackgroundAttachesOnlyInTheShareHandlerAndNeverNavigates(t *testing.T) {
	background := shipped(t, "background.js")
	start := strings.Index(background, "async function share(")
	if start < 0 {
		t.Fatal("background.js has no share handler")
	}
	end := start + strings.Index(background[start:], "\n}\n")
	if end < start {
		t.Fatal("the share handler in background.js does not end")
	}
	if total, inside := strings.Count(background, "debugger.attach"), strings.Count(background[start:end], "chrome.debugger.attach("); total != 1 || inside != 1 {
		t.Fatalf("background.js attaches %d times, %d of them in the share handler; want exactly one, in it", total, inside)
	}
	for _, never := range []string{"chrome.tabs.update", "chrome.tabs.create", "Page.navigate", "location.href =", "eval(", "new Function", "['attach']", `["attach"]`} {
		if strings.Contains(background, never) {
			t.Errorf("background.js contains %s", never)
		}
	}
}

func TestSnapshotExcludesPasswordFileAndHiddenInputs(t *testing.T) {
	snapshot := shipped(t, "snapshot.js")
	if !strings.Contains(snapshot, "['password', 'file', 'hidden']") || !strings.Contains(snapshot, "!excluded.includes(e.type)") {
		t.Fatal("snapshot.js does not name the password, file and hidden exclusion")
	}
}

func TestNoShippedFileCarriesACommentOrAnEmDash(t *testing.T) {
	for _, name := range []string{"background.js", "popup.js", "snapshot.js", "popup.html", "manifest.json"} {
		text := shipped(t, name)
		if strings.ContainsRune(text, emDash) || strings.Contains(text, "/*") || strings.Contains(text, "<!--") {
			t.Errorf("%s carries an em dash or a block comment", name)
		}
		for number, line := range strings.Split(text, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				t.Errorf("%s:%d is a comment", name, number+1)
			}
		}
	}
}
