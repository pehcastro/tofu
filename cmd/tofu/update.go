package main

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"cmp"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"time"

	"tofu/interface/cli"
	"tofu/interface/tui/frame"
	"tofu/internal/konst"
	settingspkg "tofu/internal/settings"
	"tofu/internal/sys"
	"tofu/internal/transport"
)

const (
	updateUsage          = "tofu update [--check] [--force] [--json]"
	updateAPIVariable    = "TOFU_UPDATE_API"
	updateTargetVariable = "TOFU_UPDATE_TARGET"
	updateAPI            = "https://api.github.com"
	updateRepository     = "pehcastro/tofu"
	updateLatestPath     = "/repos/" + updateRepository + "/releases/latest"
	updateChecksums      = "checksums.txt"
	updateAsideSuffix    = ".old"
	updateCacheFile      = "update.json"
	updateExecutableMode = 0o755
	updateAttemptTimeout = 5 * time.Minute
	exitUpdateAvailable  = 10
)

type latestRelease struct {
	Tag    string `json:"tag_name"`
	Assets []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

type updateReport struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	Available bool   `json:"available"`
	Release   bool   `json:"release"`
	Source    string `json:"source"`
	Installed string `json:"installed,omitempty"`
	Aside     string `json:"aside,omitempty"`
}

func updateVerb(args []string, out, errOut io.Writer) int {
	build := frame.Release(sys.Version(), sys.BuildRevision())
	return updateFrom(build, build == konst.Version, args, out, errOut)
}

func updateFrom(current string, release bool, args []string, out, errOut io.Writer) int {
	o := verbOutput{verb: "update", usageLine: updateUsage, asJSON: jsonAsked(args), out: out, errOut: errOut}
	var check, force bool
	for _, arg := range withoutJSON(args) {
		switch arg {
		case "--check":
			check = true
		case "--force":
			force = true
		default:
			return o.usage(errors.New("unknown argument " + strconv.Quote(arg)))
		}
	}
	client, err := updateClient()
	if err != nil {
		return o.fail(err)
	}
	ctx := context.Background()
	report := updateReport{Current: current, Release: release, Source: updateSource()}
	latest, err := readLatest(ctx, client)
	var failed *transport.Error
	if errors.As(err, &failed) && failed.Status == http.StatusNotFound {
		return o.refuse(exitVerdict, cli.Problem{What: "no release published yet at github.com/" + updateRepository})
	}
	if err != nil {
		return o.fail(err)
	}
	report.Latest = strings.TrimPrefix(latest.Tag, "v")
	latestNumber, ok := parseSemver(report.Latest)
	if !ok {
		return o.fail(fmt.Errorf("the latest release is tagged %q, which is not a version", latest.Tag))
	}
	currentNumber, _ := parseSemver(current)
	report.Available = latestNumber.after(currentNumber)
	lines := func(page cli.Page) []string { return updateLines(page, report) }
	if check {
		if code := o.done(true, report, lines); code != exitOK || !report.Available {
			return code
		}
		return exitUpdateAvailable
	}
	if !release && !force {
		return o.refuse(exitVerdict, cli.Problem{What: current + " is not a release build, so tofu update leaves it alone", Hint: "tofu update --force replaces it with " + report.Latest})
	}
	if !report.Available && !force {
		return o.done(true, report, lines)
	}
	target, err := updateTarget()
	if err != nil {
		return o.fail(err)
	}
	binary, err := downloadVerified(ctx, client, latest, report.Latest)
	if err != nil {
		return o.fail(err)
	}
	if report.Aside, err = replaceBinary(target, binary); err != nil {
		return o.fail(err)
	}
	report.Installed = target
	return o.done(true, report, lines)
}

func updateClient() (*transport.Client, error) {
	return transport.New(transport.Config{AttemptTimeout: updateAttemptTimeout, Concurrency: 1})
}

func updateSource() string {
	return cmp.Or(strings.TrimSpace(os.Getenv(updateAPIVariable)), updateAPI)
}

func readLatest(ctx context.Context, client *transport.Client) (latestRelease, error) {
	var latest latestRelease
	source := updateSource()
	body, err := fetch(ctx, client, source+updateLatestPath, "application/vnd.github+json")
	if err != nil {
		return latest, fmt.Errorf("reading the latest release from %s: %w", source, err)
	}
	if err := json.Unmarshal(body, &latest); err != nil {
		return latest, fmt.Errorf("the latest release from %s is not JSON: %w", source, err)
	}
	return latest, nil
}

func updateTarget() (string, error) {
	target := os.Getenv(updateTargetVariable)
	if target == "" {
		var err error
		if target, err = os.Executable(); err != nil {
			return "", err
		}
	}
	if resolved, err := filepath.EvalSymlinks(target); err == nil {
		target = resolved
	}
	return target, nil
}

type updateWatch struct {
	client        *transport.Client
	target, cache string
	onDiskStat    os.FileInfo
	onDisk        string
	tried         string
	installErr    error
}

func appUpdates(dir string) func() string {
	if frame.Release(sys.Version(), sys.BuildRevision()) != konst.Version {
		return nil
	}
	target, err := updateTarget()
	home, homeErr := sys.HomeConfigDir()
	if errors.Join(err, homeErr) != nil {
		return nil
	}
	watch, err := newUpdateWatch(target, filepath.Join(home, updateCacheFile))
	if err != nil {
		return nil
	}
	return func() string {
		return watch.check(context.Background(), settingInt(dir, settingspkg.AutoUpdate, nil) != 0)
	}
}

func newUpdateWatch(target, cache string) (*updateWatch, error) {
	client, err := updateClient()
	started, statErr := os.Stat(target)
	return &updateWatch{client: client, target: target, cache: cache, onDiskStat: started, onDisk: konst.Version}, errors.Join(err, statErr)
}

func (w *updateWatch) check(ctx context.Context, auto bool) string {
	release := w.latest(ctx)
	latest := strings.TrimPrefix(release.Tag, "v")
	w.readDisk(ctx)
	if newer(latest, w.onDisk) && !strings.Contains(w.onDisk, "+") {
		if !auto {
			return "tofu " + latest + " is out · tofu update installs it"
		}
		if w.tried != latest {
			w.tried, w.installErr = latest, w.install(ctx, release, latest)
		}
		if w.installErr != nil {
			return "tofu " + latest + " is out and installing it failed: " + w.installErr.Error() + " · tofu update installs it"
		}
	}
	if newer(w.onDisk, konst.Version) {
		return "tofu " + w.onDisk + " is installed · restart to use it"
	}
	return ""
}

func (w *updateWatch) latest(ctx context.Context) latestRelease {
	cached, readErr := os.ReadFile(w.cache)
	info, statErr := os.Stat(w.cache)
	if readErr != nil || statErr != nil || time.Since(info.ModTime()) >= konst.UpdateCheckHours*time.Hour {
		if fetched, err := readLatest(ctx, w.client); err == nil {
			cached, _ = json.Marshal(fetched)
		}
		_ = os.MkdirAll(filepath.Dir(w.cache), 0o755)
		_ = os.WriteFile(w.cache, cached, 0o644)
	}
	var release latestRelease
	_ = json.Unmarshal(cached, &release)
	return release
}

func (w *updateWatch) readDisk(ctx context.Context) {
	now, err := os.Stat(w.target)
	if err != nil || now.Size() == w.onDiskStat.Size() && now.ModTime().Equal(w.onDiskStat.ModTime()) {
		return
	}
	probe, cancel := context.WithTimeout(ctx, konst.UpdateProbeSeconds*time.Second)
	defer cancel()
	var printed struct {
		Data versionReport `json:"data"`
	}
	output, err := exec.CommandContext(probe, w.target, "version", "--json").Output()
	if err != nil || json.Unmarshal(output, &printed) != nil {
		printed.Data.Version = konst.Version
	}
	w.onDiskStat, w.onDisk = now, printed.Data.Version
}

func (w *updateWatch) install(ctx context.Context, release latestRelease, version string) error {
	binary, err := downloadVerified(ctx, w.client, release, version)
	if err != nil {
		return err
	}
	if _, err := replaceBinary(w.target, binary); err != nil {
		return err
	}
	installed, err := os.Stat(w.target)
	if err != nil {
		return err
	}
	w.onDiskStat, w.onDisk = installed, version
	return nil
}

func newer(version, than string) bool {
	this, ok := parseSemver(version)
	that, thatOK := parseSemver(than)
	return ok && thatOK && this.after(that)
}

func updateLines(page cli.Page, report updateReport) []string {
	lines := page.Facts([]cli.Fact{{Label: "current", Text: report.Current}, {Label: "latest", Text: report.Latest}, {Label: "from", Text: report.Source}})
	switch {
	case report.Installed != "":
		lines = append(lines, page.Receipt(cli.Changed, "tofu "+report.Current+" → "+report.Latest, report.Installed))
		if report.Aside != "" {
			lines = append(lines, cli.Indent(page.Hint("the previous build stays at "+page.Path(report.Aside)+" until the next update"))...)
		}
	case !report.Release:
		lines = append(lines, page.Glyph(cli.Warn)+" this is not a release build")
		lines = append(lines, cli.Indent(page.Hint("tofu update --force replaces it with "+report.Latest))...)
	case report.Available:
		lines = append(lines, page.Hint("tofu update installs "+report.Latest))
	default:
		lines = append(lines, page.Glyph(cli.Done)+" tofu is current")
	}
	return lines
}

func fetch(ctx context.Context, client *transport.Client, url, accept string) ([]byte, error) {
	response, err := client.Do(ctx, transport.Request{
		Method: http.MethodGet,
		URL:    url,
		Header: http.Header{"Accept": {accept}, "User-Agent": {"tofu/" + konst.Version}},
	})
	return response.Body, err
}

func downloadVerified(ctx context.Context, client *transport.Client, release latestRelease, version string) ([]byte, error) {
	executable, archiveSuffix := "tofu", ".tar.gz"
	if goruntime.GOOS == "windows" {
		executable, archiveSuffix = "tofu.exe", ".zip"
	}
	name := "tofu_" + version + "_" + goruntime.GOOS + "_" + goruntime.GOARCH + archiveSuffix
	urls := map[string]string{}
	for _, asset := range release.Assets {
		urls[asset.Name] = asset.URL
	}
	if urls[name] == "" {
		return nil, fmt.Errorf("release %s has no %s for this system", release.Tag, name)
	}
	if urls[updateChecksums] == "" {
		return nil, fmt.Errorf("release %s has no %s, so %s cannot be verified and is not installed", release.Tag, updateChecksums, name)
	}
	sums, err := fetch(ctx, client, urls[updateChecksums], "application/octet-stream")
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", updateChecksums, err)
	}
	var want string
	for scanner := bufio.NewScanner(bytes.NewReader(sums)); scanner.Scan(); {
		if fields := strings.Fields(scanner.Text()); len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name {
			want = strings.ToLower(fields[0])
		}
	}
	if want == "" {
		return nil, fmt.Errorf("%s has no line for %s, so it is not installed", updateChecksums, name)
	}
	archive, err := fetch(ctx, client, urls[name], "application/octet-stream")
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", name, err)
	}
	sum := sha256.Sum256(archive)
	if got := hex.EncodeToString(sum[:]); got != want {
		return nil, fmt.Errorf("%s has SHA-256 %s and %s says %s, so nothing was installed", name, got, updateChecksums, want)
	}
	binary, err := unpack(archive, archiveSuffix, executable)
	if err != nil {
		return nil, fmt.Errorf("unpacking %s: %w", name, err)
	}
	return binary, nil
}

func unpack(archive []byte, archiveSuffix, executable string) ([]byte, error) {
	if archiveSuffix == ".zip" {
		files, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return nil, err
		}
		for _, file := range files.File {
			if path.Base(file.Name) != executable {
				continue
			}
			opened, err := file.Open()
			if err != nil {
				return nil, err
			}
			defer func() { _ = opened.Close() }()
			return io.ReadAll(opened)
		}
		return nil, errors.New("no " + executable + " inside")
	}
	unzipped, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	entries := tar.NewReader(unzipped)
	for {
		header, err := entries.Next()
		if errors.Is(err, io.EOF) {
			return nil, errors.New("no " + executable + " inside")
		}
		if err != nil {
			return nil, err
		}
		if header.Typeflag == tar.TypeReg && path.Base(header.Name) == executable {
			return io.ReadAll(entries)
		}
	}
}

func replaceBinary(target string, binary []byte) (string, error) {
	staged, err := os.CreateTemp(filepath.Dir(target), filepath.Base(target)+".new-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(staged.Name()) }()
	_, err = staged.Write(binary)
	if err = errors.Join(err, staged.Close(), os.Chmod(staged.Name(), updateExecutableMode)); err != nil {
		return "", err
	}
	if goruntime.GOOS != "windows" {
		return "", os.Rename(staged.Name(), target)
	}
	setAside, _ := filepath.Glob(target + updateAsideSuffix + "*")
	for _, old := range setAside {
		_ = os.Remove(old)
	}
	aside := target + updateAsideSuffix + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	if err := os.Rename(target, aside); errors.Is(err, fs.ErrNotExist) {
		aside = ""
	} else if err != nil {
		return "", err
	}
	if err := os.Rename(staged.Name(), target); err != nil {
		if aside != "" {
			_ = os.Rename(aside, target)
		}
		return "", err
	}
	return aside, nil
}
