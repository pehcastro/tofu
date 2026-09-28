package browser

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"tofu/internal/browser/extension"
	"tofu/internal/sys"
)

const (
	HostName       = "com.ephem.tofu"
	ChromeHostsKey = `Software\Google\Chrome\NativeMessagingHosts`
)

type hostManifest struct {
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	Path           string   `json:"path"`
	Type           string   `json:"type"`
	AllowedOrigins []string `json:"allowed_origins"`
}

func ExtensionID(publicKeyDER []byte) string {
	sum := sha256.Sum256(publicKeyDER)
	return strings.Map(func(digit rune) rune {
		if digit <= '9' {
			return 'a' + digit - '0'
		}
		return 'k' + digit - 'a'
	}, hex.EncodeToString(sum[:16]))
}

func Install(home, exe, hostsKey string) (string, error) {
	raw, err := extension.Files.ReadFile("manifest.json")
	if err != nil {
		return "", err
	}
	var shipped struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(raw, &shipped); err != nil {
		return "", err
	}
	der, err := base64.StdEncoding.DecodeString(shipped.Key)
	if err != nil {
		return "", err
	}
	id := ExtensionID(der)

	extensionDir, manifestPath := installPaths(home)
	if err := os.RemoveAll(extensionDir); err != nil {
		return "", err
	}
	if err := os.CopyFS(extensionDir, extension.Files); err != nil {
		return "", err
	}
	manifest, err := json.MarshalIndent(hostManifest{
		Name:           HostName,
		Description:    "tofu reads and drives the Chrome tabs you share with it",
		Path:           exe,
		Type:           "stdio",
		AllowedOrigins: []string{"chrome-extension://" + id + "/"},
	}, "", "  ")
	if err != nil {
		return "", err
	}
	if err := sys.WriteFile(manifestPath, manifest, 0o644); err != nil {
		return "", err
	}
	return id, register(hostsKey+`\`+HostName, manifestPath)
}

func Uninstall(home, hostsKey string) error {
	extensionDir, manifestPath := installPaths(home)
	if err := unregister(hostsKey + `\` + HostName); err != nil {
		return err
	}
	if err := os.Remove(manifestPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.RemoveAll(extensionDir)
}

func installPaths(home string) (extensionDir, manifestPath string) {
	dir := filepath.Join(home, sys.StateDirName, "browser")
	return filepath.Join(dir, "extension"), filepath.Join(dir, HostName+".json")
}
