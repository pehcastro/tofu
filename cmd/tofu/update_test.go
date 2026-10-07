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
	"sync/atomic"
	"testing"

	"tofu/internal/konst"
)

type fakeRelease struct {
	tag       string
	asset     string
	checksums string
	sums      bool
	missing   bool
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

type releaseHits struct{ latest, asset atomic.Int32 }

func serveRelease(t *testing.T, release fakeRelease, binary []byte) *releaseHits {
	t.Helper()
	hits := &releaseHits{}
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
		hits.latest.Add(1)
		if release.missing {
			http.Error(w, `{"message":"Not Found","status":"404"}`, http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": release.tag, "assets": assets})
	})
	mux.HandleFunc("/download/asset", func(w http.ResponseWriter, r *http.Request) {
		hits.asset.Add(1)
		_, _ = w.Write(archive)
	})
	mux.HandleFunc("/download/sums", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(checksums)) })
	t.Setenv(updateAPIVariable, server.URL)
	return hits
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
		{"no release published yet says so in plain words", fakeRelease{missing: true}, "0.5.0", true, []string{"--check"}, exitVerdict, oldBinary, "no release published yet at github.com/pehcastro/tofu"},
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
			if !strings.Contains(printed, c.says) || strings.Contains(printed, "transport.") {
				t.Errorf("printed nothing saying %q, or named the transport", c.says)
			}
			if got, _ := os.ReadFile(target); string(got) != c.installed {
				t.Errorf("the target holds %q, want %q", got, c.installed)
			}
			entries, _ := os.ReadDir(dir)
			var names []string
			for _, entry := range entries {
				names = append(names, entry.Name())
			}
			setAside := c.installed == newBinary && goruntime.GOOS == "windows"
			want := 1
			if setAside {
				want = 2
			}
			if len(names) != want || names[0] != "tofu.exe" || setAside && !strings.HasPrefix(names[1], "tofu.exe.old-") {
				t.Errorf("the directory holds %v, want tofu.exe and, after a Windows install, one tofu.exe.old-*", names)
			}
		})
	}
}

func TestUpdateInTheBackground(t *testing.T) {
	const oldBinary, newBinary = "old tofu", "new tofu"
	cases := []struct {
		name      string
		release   fakeRelease
		auto      bool
		says      string
		installed string
		downloads int32
	}{
		{"on, a newer release is installed once and the restart line stays", fakeRelease{tag: "v99.0.0", sums: true}, true, "tofu 99.0.0 is installed · restart to use it", newBinary, 1},
		{"off, a newer release is only named", fakeRelease{tag: "v99.0.0", sums: true}, false, "tofu 99.0.0 is out · tofu update installs it", oldBinary, 0},
		{"a bad checksum installs nothing, says so, and is not retried every tick", fakeRelease{tag: "v99.0.0", sums: true, checksums: strings.Repeat("0", 64) + "  {name}\n"}, true, "installing it failed", oldBinary, 1},
		{"no release says nothing and asks once", fakeRelease{missing: true}, true, "", oldBinary, 0},
		{"a tag that is not a version installs nothing", fakeRelease{tag: "nightly", sums: true}, true, "", oldBinary, 0},
		{"the running release is current", fakeRelease{tag: "v" + konst.Version, sums: true}, true, "", oldBinary, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			hits := serveRelease(t, c.release, []byte(newBinary))
			dir := t.TempDir()
			target := filepath.Join(dir, "tofu.exe")
			if err := os.WriteFile(target, []byte(oldBinary), 0o755); err != nil {
				t.Fatal(err)
			}
			watch, err := newUpdateWatch(target, filepath.Join(dir, "update.json"))
			if err != nil {
				t.Fatal(err)
			}
			for tick := range 3 {
				said := watch.check(t.Context(), c.auto)
				t.Logf("tick %d: %q", tick, said)
				if c.says == "" && said != "" || !strings.Contains(said, c.says) {
					t.Errorf("tick %d said %q, want %q", tick, said, c.says)
				}
			}
			if got, _ := os.ReadFile(target); string(got) != c.installed {
				t.Errorf("the target holds %q, want %q", got, c.installed)
			}
			if hits.latest.Load() != 1 || hits.asset.Load() != c.downloads {
				t.Errorf("three ticks asked for the release %d times and downloaded it %d times, want 1 and %d", hits.latest.Load(), hits.asset.Load(), c.downloads)
			}
		})
	}
	t.Run("a test binary is a dev build and gets no updater", func(t *testing.T) {
		if appUpdates(t.TempDir()) != nil {
			t.Error("a dev build got an update check")
		}
	})
}

func TestReplaceBinaryPassesACopyStillRunning(t *testing.T) {
	if goruntime.GOOS != "windows" {
		t.Skip("only Windows sets the running binary aside")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "tofu.exe")
	free, held := target+".old", target+".old-held"
	for _, path := range []string{target, free, held} {
		if err := os.WriteFile(path, []byte(path), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	running, err := os.Open(held)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = running.Close() }()
	aside, err := replaceBinary(target, []byte("new tofu"))
	if err != nil {
		t.Fatalf("a held set-aside copy blocked the install: %v", err)
	}
	t.Logf("set aside at %s", aside)
	if _, err := os.Stat(free); err == nil {
		t.Error("the free tofu.exe.old was kept")
	}
	if _, err := os.Stat(held); err != nil {
		t.Error("the held copy was deleted")
	}
	if got, _ := os.ReadFile(target); string(got) != "new tofu" {
		t.Errorf("the target holds %q", got)
	}
}
