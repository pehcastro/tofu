package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"cmp"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
)

type fakeRelease struct {
	tag       string
	asset     string
	checksums string
	sums      bool
}

func fakeArchive(t *testing.T, binary []byte) ([]byte, string) {
	t.Helper()
	var packed bytes.Buffer
	if goruntime.GOOS == "windows" {
		archive := zip.NewWriter(&packed)
		file, err := archive.Create("tofu.exe")
		if err == nil {
			_, err = file.Write(binary)
		}
		if err != nil || archive.Close() != nil {
			t.Fatal(err)
		}
		return packed.Bytes(), ".zip"
	}
	zipped := gzip.NewWriter(&packed)
	archive := tar.NewWriter(zipped)
	if err := archive.WriteHeader(&tar.Header{Name: "./tofu", Mode: 0o755, Size: int64(len(binary)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := archive.Write(binary); err != nil || archive.Close() != nil || zipped.Close() != nil {
		t.Fatal(err)
	}
	return packed.Bytes(), ".tar.gz"
}

func serveRelease(t *testing.T, release fakeRelease, binary []byte) {
	t.Helper()
	archive, suffix := fakeArchive(t, binary)
	version := strings.TrimPrefix(release.tag, "v")
	name := "tofu_" + version + "_" + goruntime.GOOS + "_" + goruntime.GOARCH + suffix
	sum := sha256.Sum256(archive)
	checksums := strings.ReplaceAll(cmp.Or(release.checksums, "{sum}  {name}\n"), "{name}", name)
	checksums = strings.ReplaceAll(checksums, "{sum}", hex.EncodeToString(sum[:]))
	name = cmp.Or(release.asset, name)
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	assets := []map[string]string{{"name": name, "browser_download_url": server.URL + "/download/asset"}}
	if release.sums {
		assets = append(assets, map[string]string{"name": "checksums.txt", "browser_download_url": server.URL + "/download/sums"})
	}
	mux.HandleFunc("/repos/pehcastro/tofu/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": release.tag, "assets": assets})
	})
	mux.HandleFunc("/download/asset", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(archive) })
	mux.HandleFunc("/download/sums", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(checksums)) })
	t.Setenv(updateAPIVariable, server.URL)
}

func TestUpdateAgainstAFakeRelease(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	const oldBinary, newBinary = "old tofu", "new tofu"
	cases := []struct {
		name      string
		release   fakeRelease
		current   string
		isRelease bool
		args      []string
		code      int
		installed string
		says      string
	}{
		{"check sees a newer release", fakeRelease{tag: "v0.5.1", sums: true}, "0.5.0", true, []string{"--check"}, exitUpdateAvailable, oldBinary, "tofu update installs 0.5.1"},
		{"check compares prerelease numbers, not strings", fakeRelease{tag: "v0.5.0-rc-fix10", sums: true}, "0.5.0-rc-fix9", true, []string{"--check"}, exitUpdateAvailable, oldBinary, "0.5.0-rc-fix10"},
		{"check on the latest is current", fakeRelease{tag: "v0.5.0", sums: true}, "0.5.0", true, []string{"--check"}, exitOK, oldBinary, "tofu is current"},
		{"update on the latest installs nothing", fakeRelease{tag: "v0.5.0", sums: true}, "0.5.0", true, nil, exitOK, oldBinary, "tofu is current"},
		{"a local build ahead of the release is not downgraded", fakeRelease{tag: "v0.5.0-rc-fix24", sums: true}, "0.5.0", true, nil, exitOK, oldBinary, "tofu is current"},
		{"update installs a newer release", fakeRelease{tag: "v0.5.1", sums: true}, "0.5.0", true, nil, exitOK, newBinary, "tofu 0.5.0 → 0.5.1"},
		{"a checksum mismatch installs nothing", fakeRelease{tag: "v0.5.1", sums: true, checksums: strings.Repeat("0", 64) + "  {name}\n"}, "0.5.0", true, nil, exitVerdict, oldBinary, "so nothing was installed"},
		{"a release without checksums installs nothing", fakeRelease{tag: "v0.5.1"}, "0.5.0", true, nil, exitVerdict, oldBinary, "cannot be verified"},
		{"checksums without the asset's line install nothing", fakeRelease{tag: "v0.5.1", sums: true, checksums: "abc  tofu_0.5.1_plan9_mips.zip\n"}, "0.5.0", true, nil, exitVerdict, oldBinary, "has no line for"},
		{"no asset for this system installs nothing", fakeRelease{tag: "v0.5.1", sums: true, asset: "tofu_0.5.1_plan9_mips.zip"}, "0.5.0", true, nil, exitVerdict, oldBinary, "for this system"},
		{"a tag that is not a version installs nothing", fakeRelease{tag: "nightly", sums: true}, "0.5.0", true, nil, exitVerdict, oldBinary, "not a version"},
		{"a dev build is left alone", fakeRelease{tag: "v0.5.1", sums: true}, "0.5.0+dev", false, nil, exitVerdict, oldBinary, "not a release build"},
		{"a dev build check says it is not a release", fakeRelease{tag: "v0.5.1", sums: true}, "0.5.0+dev", false, []string{"--check"}, exitUpdateAvailable, oldBinary, "tofu update --force replaces it"},
		{"force replaces a dev build", fakeRelease{tag: "v0.5.0", sums: true}, "0.5.0+dev", false, []string{"--force"}, exitOK, newBinary, "tofu 0.5.0+dev → 0.5.0"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			serveRelease(t, c.release, []byte(newBinary))
			dir := t.TempDir()
			target := filepath.Join(dir, "tofu.exe")
			if err := os.WriteFile(target, []byte(oldBinary), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv(updateTargetVariable, target)
			var out, errOut bytes.Buffer
			code := updateFrom(c.current, c.isRelease, c.args, &out, &errOut)
			t.Logf("exit %d\n%s%s", code, out.String(), errOut.String())
			printed := strings.Join(strings.Fields(out.String()+errOut.String()), " ")
			if code != c.code {
				t.Errorf("exit %d, want %d", code, c.code)
			}
			if !strings.Contains(printed, c.says) {
				t.Errorf("printed nothing saying %q", c.says)
			}
			if got, _ := os.ReadFile(target); string(got) != c.installed {
				t.Errorf("the target holds %q, want %q", got, c.installed)
			}
			entries, _ := os.ReadDir(dir)
			var names []string
			for _, entry := range entries {
				names = append(names, entry.Name())
			}
			want := []string{"tofu.exe"}
			if c.installed == newBinary && goruntime.GOOS == "windows" {
				want = append(want, "tofu.exe.old")
			}
			if strings.Join(names, " ") != strings.Join(want, " ") {
				t.Errorf("the directory holds %v, want %v", names, want)
			}
		})
	}
}
