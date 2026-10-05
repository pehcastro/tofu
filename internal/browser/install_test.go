//go:build windows

package browser

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime/debug"
	"slices"
	"testing"

	"golang.org/x/sys/windows/registry"

	"tofu/internal/browser/extension"
	"tofu/internal/konst"
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
		if id, browsers, err := Install(home, exe, hosts); err != nil || id != shippedExtensionID || browsers != nil {
			t.Fatalf("Install returned %q, %q, %v; want %q and no browser folder, since the registry key is the registration", id, browsers, err, shippedExtensionID)
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
	build, _ := Build()
	var unpacked, want map[string]any
	if err == nil {
		err = errors.Join(json.Unmarshal(copied, &unpacked), json.Unmarshal(shipped, &want))
	}
	if err != nil {
		t.Fatalf("the unpacked extension manifest is %s: %v", copied, err)
	}
	want["version"], want["version_name"] = chromeVersion(konst.Version), konst.Version+" · "+build
	if !reflect.DeepEqual(unpacked, want) {
		t.Fatalf("the unpacked extension manifest is %s; want the shipped one with version %s and version_name %s", copied, want["version"], want["version_name"])
	}

	if _, err := Uninstall(home, hosts); err != nil {
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
	if _, err := Uninstall(home, hosts); err != nil {
		t.Fatalf("a second Uninstall failed: %v", err)
	}
}

func TestTwoCommitsAtTheSameVersionAndExtensionAreTwoBuilds(t *testing.T) {
	at := func(revision, modified string) *debug.BuildInfo {
		return &debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: revision}, {Key: "vcs.modified", Value: modified}}}
	}
	builds := map[string]string{}
	for name, info := range map[string]*debug.BuildInfo{
		"c2a0da0":       at("c2a0da0", "false"),
		"f50b4ff":       at("f50b4ff", "false"),
		"f50b4ff dirty": at("f50b4ff", "true"),
		"no vcs":        nil,
	} {
		build, err := buildOf(info)
		if err != nil || build == "" {
			t.Fatalf("the build of %s is %q, %v", name, build, err)
		}
		builds[name] = build
	}
	if builds["c2a0da0"] == builds["f50b4ff"] || builds["f50b4ff"] == builds["f50b4ff dirty"] || builds["f50b4ff dirty"] != builds["no vcs"] {
		t.Fatalf("builds %v; want each commit distinct, and a dirty tree hashed from the executable like a binary with no commit", builds)
	}
	if again, err := buildOf(at("c2a0da0", "false")); err != nil || again != builds["c2a0da0"] {
		t.Fatalf("c2a0da0 built twice gives %q then %q, %v", builds["c2a0da0"], again, err)
	}
}
