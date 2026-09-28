package extension_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image/png"
	"io/fs"
	"os/exec"
	"strconv"
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

func TestManifestKeepsTheIDHasNoPopupAndCarriesTheIcon(t *testing.T) {
	text := shipped(t, "manifest.json")
	var manifest struct {
		Key        string            `json:"key"`
		Version    string            `json:"version"`
		Permission []string          `json:"permissions"`
		Icons      map[string]string `json:"icons"`
		Background struct {
			ServiceWorker string `json:"service_worker"`
		} `json:"background"`
	}
	if err := json.Unmarshal([]byte(text), &manifest); err != nil {
		t.Fatal(err)
	}
	der, err := base64.StdEncoding.DecodeString(manifest.Key)
	if err != nil {
		t.Fatal(err)
	}
	if id := browser.ExtensionID(der); id != pinnedExtensionID {
		t.Fatalf("the extension id is %s, want %s", id, pinnedExtensionID)
	}
	if strings.Join(manifest.Permission, " ") != "nativeMessaging debugger tabs tabGroups storage" || manifest.Version == "0.3.0" {
		t.Fatalf("permissions %v and version %s; want tabGroups and storage added, activeTab gone, and the version bumped", manifest.Permission, manifest.Version)
	}
	if strings.Contains(text, "default_popup") || manifest.Background.ServiceWorker != "background.js" {
		t.Fatalf("the manifest names a popup, or the service worker is %q", manifest.Background.ServiceWorker)
	}
	for _, size := range []int{16, 32, 48, 128} {
		name := manifest.Icons[strconv.Itoa(size)]
		config, err := png.DecodeConfig(bytes.NewReader([]byte(shipped(t, name))))
		if err != nil || config.Width != size || config.Height != size {
			t.Fatalf("the %d pixel icon %q is %dx%d, %v", size, name, config.Width, config.Height, err)
		}
	}
	names, err := fs.Glob(extension.Files, "*")
	if err != nil || strings.Join(names, " ") != "background.js icon-128.png icon-16.png icon-32.png icon-48.png manifest.json snapshot.js" {
		t.Fatalf("the extension ships %v, %v", names, err)
	}
}

func handler(t *testing.T, background, name string) string {
	t.Helper()
	start := strings.Index(background, "async function "+name+"(")
	if start < 0 {
		t.Fatalf("background.js has no %s handler", name)
	}
	end := strings.Index(background[start:], "\n}\n")
	if end < 0 {
		t.Fatalf("the %s handler in background.js does not end", name)
	}
	return background[start : start+end]
}

func onlyIn(t *testing.T, background, call, name string) {
	t.Helper()
	if total, inside := strings.Count(background, call), strings.Count(handler(t, background, name), call); total != 1 || inside != 1 {
		t.Errorf("background.js calls %s %d times, %d of them in %s; want exactly one, there", call, total, inside, name)
	}
}

func TestBackgroundAttachesAndGroupsInOnePlaceEachAndNeverNavigates(t *testing.T) {
	background := shipped(t, "background.js")
	onlyIn(t, background, "chrome.debugger.attach(", "attach")
	onlyIn(t, background, "chrome.tabs.group(", "groupTab")
	onlyIn(t, background, "chrome.tabs.ungroup(", "restoreGroups")
	onlyIn(t, background, "chrome.tabs.create({url, active: false})", "openTab")
	onlyIn(t, background, "args.url", "perform")
	disconnect := strings.Index(background, "port.onDisconnect.addListener(")
	if disconnect < 0 || !strings.Contains(background[disconnect:disconnect+strings.Index(background[disconnect:], "});")], "restoreGroups") {
		t.Error("restoreGroups does not run when the port to tofu drops")
	}
	if !strings.Contains(background, "const DRIVE_OPS = ['click', 'fill', 'select', 'scroll', 'wait'];") {
		t.Error("the drive ops are not exactly click, fill, select, scroll and wait")
	}
	closeOpened := handler(t, background, "closeOpened")
	check, remove := strings.Index(closeOpened, "if (!opened.has(tabId)) throw"), strings.Index(closeOpened, "chrome.tabs.remove(tabId)")
	if strings.Count(background, "tabs.remove") != 1 || check < 0 || remove < check {
		t.Errorf("background.js removes a tab %d times; want exactly one, in closeOpened after the opened check", strings.Count(background, "tabs.remove"))
	}
	for _, never := range []string{"chrome.tabs.update", "Page.navigate", "location.href =", "eval(", "new Function", "['attach']", `["attach"]`, "popup"} {
		if strings.Contains(background, never) {
			t.Errorf("background.js contains %s", never)
		}
	}
}

func TestBackgroundInAStubbedChromeAttachesOnFirstUseGroupsAndRestores(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("no node on the PATH to run background.js")
	}
	var stderr bytes.Buffer
	stub := exec.Command(node, "testdata/chrome.js", ".")
	stub.Stderr = &stderr
	out, err := stub.Output()
	if err != nil {
		t.Fatalf("background.js in a stubbed Chrome: %v\n%s", err, stderr.String())
	}
	var run struct {
		Heard    [][]any          `json:"heard"`
		Posted   []map[string]any `json:"posted"`
		Grouped  map[string]any   `json:"grouped"`
		Restored map[string]any   `json:"restored"`
	}
	if err := json.Unmarshal(out, &run); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	heard, first := map[string][]string{}, map[string]int{}
	for i, entry := range run.Heard {
		rest, _ := json.Marshal(entry[1:])
		kind := entry[0].(string)
		heard[kind] = append(heard[kind], string(rest))
		if _, seen := first[kind+string(rest)]; !seen {
			first[kind+string(rest)] = i
		}
	}
	t.Logf("heard %v", heard)
	for kind, want := range map[string]string{
		"attach":      "[9] [3] [5] [6] [9]",
		"group":       "[[9],100] [[6],100] [[9],101]",
		"groupUpdate": `[100,{"color":"orange","title":"tofu •"}] [100,{"title":"tofu"}] [101,{"color":"orange","title":"tofu"}]`,
		"ungroup":     "[[9]]",
		"detach":      "[9] [3] [5] [6]",
		"badge":       `["on"] ["act"] ["on"] ["off"] ["on"]`,
	} {
		if got := strings.Join(heard[kind], " "); got != want {
			t.Errorf("%s: heard %s, want %s", kind, got, want)
		}
	}
	if attach, click := first["attach[9]"], first[`evaluate[9,"click"]`]; heard["evaluate"][0] != `[9,"click"]` || click < attach {
		t.Errorf("the first evaluate is %s, at %d, and tab 9 attached at %d; want the click on tab 9 after its attach", heard["evaluate"][0], click, attach)
	}
	hello, _ := json.Marshal(run.Posted[0])
	if !strings.Contains(string(hello), `"t":"hello","tabs":[{"id":9,`) || strings.Contains(string(hello), "mode") {
		t.Errorf("the extension said %s; want a hello listing every tab with no mode", hello)
	}
	results := map[float64]map[string]any{}
	for _, message := range run.Posted[1:] {
		if message["t"] == "result" {
			results[message["id"].(float64)] = message
		}
	}
	for id := 1.0; id <= 7; id++ {
		timing, _ := results[id]["timing"].(map[string]any)
		if results[id]["ok"] != true || len(timing) != 3 {
			t.Errorf("call %v answered %v; want ok with evaluate, settle and act timed", id, results[id])
		}
	}
	if settled := results[1]["timing"].(map[string]any)["settle_ms"].(float64); settled <= 0 {
		t.Errorf("the click on tab 9 settled for %v ms", settled)
	}
	if kept, _ := json.Marshal(run.Grouped); string(kept) != `{"groups":{"1":100}}` {
		t.Errorf("session storage held %s while connected; want the group of window 1", kept)
	}
	if left, _ := json.Marshal(run.Restored); string(left) != `{"groups":{}}` {
		t.Errorf("session storage held %s after the port dropped", left)
	}
}

func TestSnapshotExcludesPasswordFileAndHiddenInputs(t *testing.T) {
	snapshot := shipped(t, "snapshot.js")
	if !strings.Contains(snapshot, "['password', 'file', 'hidden']") || !strings.Contains(snapshot, "!excluded.includes(e.type)") {
		t.Fatal("snapshot.js does not name the password, file and hidden exclusion")
	}
}

func TestNoShippedFileCarriesACommentOrAnEmDash(t *testing.T) {
	for _, name := range []string{"background.js", "snapshot.js", "manifest.json"} {
		text := shipped(t, name)
		if strings.ContainsRune(text, emDash) || strings.Contains(text, "/*") {
			t.Errorf("%s carries an em dash or a block comment", name)
		}
		for number, line := range strings.Split(text, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				t.Errorf("%s:%d is a comment", name, number+1)
			}
		}
	}
}
