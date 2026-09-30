package browser

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"unicode"

	"tofu/internal/browser/extension"
	"tofu/internal/konst"
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

func Build() (string, error) {
	info, _ := debug.ReadBuildInfo()
	return buildOf(info)
}

func buildOf(info *debug.BuildInfo) (string, error) {
	hash := sha256.New()
	_, _ = fmt.Fprintln(hash, konst.Version)
	revision, clean := "", false
	if info != nil {
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				revision = setting.Value
			case "vcs.modified":
				clean = setting.Value == "false"
			}
		}
	}
	if revision != "" && clean {
		_, _ = fmt.Fprintln(hash, revision)
	} else if err := hashExecutable(hash); err != nil {
		return "", err
	}
	err := fs.WalkDir(extension.Files, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		raw, err := extension.Files.ReadFile(name)
		_, _ = fmt.Fprintf(hash, "%s %d\n%s", name, len(raw), raw)
		return err
	})
	return hex.EncodeToString(hash.Sum(nil)[:6]), err
}

func hashExecutable(to io.Writer) error {
	exe, err := os.Executable()
	var binary *os.File
	if err == nil {
		binary, err = os.Open(exe)
	}
	if err != nil {
		return err
	}
	defer func() { _ = binary.Close() }()
	_, err = io.Copy(to, binary)
	return err
}

func chromeVersion(tofu string) string {
	end := strings.IndexFunc(tofu, func(r rune) bool { return r != '.' && !unicode.IsDigit(r) })
	if end < 0 {
		return tofu
	}
	fix, isFix := strings.CutPrefix(strings.TrimPrefix(tofu[end:], "-rc"), "-fix")
	if number, err := strconv.ParseUint(fix, 10, 16); isFix && err == nil {
		return tofu[:end] + "." + strconv.FormatUint(number, 10)
	}
	return tofu[:end]
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
	if err := writeExtension(extensionDir); err != nil {
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

func writeExtension(extensionDir string) error {
	build, err := Build()
	var raw []byte
	if err == nil {
		raw, err = extension.Files.ReadFile("manifest.json")
	}
	var manifest map[string]json.RawMessage
	if err == nil {
		err = json.Unmarshal(raw, &manifest)
	}
	if err != nil {
		return err
	}
	manifest["version"], _ = json.Marshal(chromeVersion(konst.Version))
	manifest["version_name"], _ = json.Marshal(konst.Version + " · " + build)
	stamped, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}

	parent := filepath.Dir(extensionDir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	fresh, err := os.MkdirTemp(parent, filepath.Base(extensionDir)+"-new-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(fresh) }()
	if err := os.CopyFS(fresh, extension.Files); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(fresh, "manifest.json"), stamped, 0o644); err != nil {
		return err
	}
	retired := fresh + "-old"
	if err := os.Rename(extensionDir, retired); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(fresh, extensionDir); err != nil {
		_ = os.Rename(retired, extensionDir)
		return err
	}
	_ = os.RemoveAll(retired)
	return nil
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
