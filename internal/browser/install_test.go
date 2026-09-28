//go:build windows

package browser

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/sys/windows/registry"

	"tofu/internal/browser/extension"
)

const shippedExtensionID = "jednanpboiikklhkkkimnmdmjmgjgphh"

func TestExtensionIDOfTheShippedKeyIsStable(t *testing.T) {
	raw, err := extension.Files.ReadFile("manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	der, err := base64.StdEncoding.DecodeString(manifest.Key)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(der)
	independent := make([]byte, 0, 32)
	for _, b := range sum[:16] {
		independent = append(independent, 'a'+(b>>4), 'a'+(b&15))
	}
	if got := ExtensionID(der); got != string(independent) || got != shippedExtensionID {
		t.Fatalf("ExtensionID is %q; from the DER %q; pinned %q", got, independent, shippedExtensionID)
	}
}

func TestInstallWritesTheHostManifestAndKeyAndUninstallRemovesBoth(t *testing.T) {
	home := t.TempDir()
	hosts := `Software\tofu-test\` + t.Name()
	t.Cleanup(func() {
		for _, scratch := range []string{hosts + `\com.ephem.tofu`, hosts, `Software\tofu-test`} {
			_ = registry.DeleteKey(registry.CURRENT_USER, scratch)
		}
	})
	exe := filepath.Join(home, "bin", "tofu.exe")
	for range 2 {
		if id, err := Install(home, exe, hosts); err != nil || id != shippedExtensionID {
			t.Fatalf("Install returned %q, %v; want %q", id, err, shippedExtensionID)
		}
	}

	manifestPath := filepath.Join(home, ".tofu", "browser", "com.ephem.tofu.json")
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Name           string   `json:"name"`
		Path           string   `json:"path"`
		Type           string   `json:"type"`
		AllowedOrigins []string `json:"allowed_origins"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	origins := []string{"chrome-extension://" + shippedExtensionID + "/"}
	if manifest.Name != "com.ephem.tofu" || manifest.Path != exe || manifest.Type != "stdio" || !slices.Equal(manifest.AllowedOrigins, origins) {
		t.Fatalf("host manifest is %s", raw)
	}

	key, err := registry.OpenKey(registry.CURRENT_USER, hosts+`\com.ephem.tofu`, registry.QUERY_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	value, _, err := key.GetStringValue("")
	_ = key.Close()
	if err != nil || value != manifestPath {
		t.Fatalf("key default value is %q, %v; want %q", value, err, manifestPath)
	}

	copied, err := os.ReadFile(filepath.Join(home, ".tofu", "browser", "extension", "manifest.json"))
	shipped, _ := extension.Files.ReadFile("manifest.json")
	if err != nil || string(copied) != string(shipped) {
		t.Fatalf("the unpacked extension manifest is %q, %v", copied, err)
	}

	if err := Uninstall(home, hosts); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.OpenKey(registry.CURRENT_USER, hosts+`\com.ephem.tofu`, registry.QUERY_VALUE); !errors.Is(err, registry.ErrNotExist) {
		t.Fatalf("the key survived Uninstall: %v", err)
	}
	for _, path := range []string{manifestPath, filepath.Join(home, ".tofu", "browser", "extension")} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s survived Uninstall: %v", path, err)
		}
	}
	if err := Uninstall(home, hosts); err != nil {
		t.Fatalf("a second Uninstall failed: %v", err)
	}
}
