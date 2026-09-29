package extension_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image/png"
	"io/fs"
	"os/exec"
	"slices"
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

func TestBackgroundAttachesGroupsAndNavigatesInOnePlaceEach(t *testing.T) {
	background := shipped(t, "background.js")
	onlyIn(t, background, "chrome.debugger.attach(", "attach")
	onlyIn(t, background, "chrome.tabs.group(", "groupTab")
	onlyIn(t, background, "chrome.tabs.ungroup(", "restoreGroups")
	onlyIn(t, background, "chrome.tabs.create({url, active: false})", "openTab")
	onlyIn(t, background, "chrome.tabs.update(tabId, {url})", "navigateOpened")
	onlyIn(t, background, "args.url", "perform")
	navigate := handler(t, background, "navigateOpened")
	if check, update := strings.Index(navigate, "ownOnly(tabId"), strings.Index(navigate, "chrome.tabs.update("); check < 0 || update < check {
		t.Error("navigateOpened does not refuse a tab tofu did not open before it navigates")
	}
	disconnect := strings.Index(background, "port.onDisconnect.addListener(")
	if disconnect < 0 || !strings.Contains(background[disconnect:disconnect+strings.Index(background[disconnect:], "});")], "restoreGroups") {
		t.Error("restoreGroups does not run when the port to tofu drops")
	}
	if !strings.Contains(background, "const DRIVE_OPS = ['click', 'fill', 'select', 'scroll', 'wait'];") {
		t.Error("the drive ops are not exactly click, fill, select, scroll and wait")
	}
	closeOpened := handler(t, background, "closeOpened")
	check, remove := strings.Index(closeOpened, "ownOnly(tabId"), strings.Index(closeOpened, "chrome.tabs.remove(tabId)")
	if strings.Count(background, "tabs.remove") != 1 || check < 0 || remove < check {
		t.Errorf("background.js removes a tab %d times; want exactly one, in closeOpened after the opened check", strings.Count(background, "tabs.remove"))
	}
	if strings.Count(background, "tabs.update(") != 1 {
		t.Errorf("background.js updates a tab %d times; want once, in navigateOpened", strings.Count(background, "tabs.update("))
	}
	for _, never := range []string{"Page.navigate", "location.href =", "eval(", "new Function", "['attach']", `["attach"]`, "popup"} {
		if strings.Contains(background, never) {
			t.Errorf("background.js contains %s", never)
		}
	}
}

type stubRun struct {
	Heard    [][]any          `json:"heard"`
	Posted   []map[string]any `json:"posted"`
	Grouped  map[string]any   `json:"grouped"`
	Restored map[string]any   `json:"restored"`
}

func inStubbedChrome(t *testing.T, scenario string) stubRun {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("no node on the PATH to run background.js")
	}
	var stderr bytes.Buffer
	stub := exec.Command(node, "testdata/chrome.js", ".", scenario)
	stub.Stderr = &stderr
	out, err := stub.Output()
	if err != nil {
		t.Fatalf("background.js in a stubbed Chrome: %v\n%s", err, stderr.String())
	}
	var run stubRun
	if err := json.Unmarshal(out, &run); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	return run
}

func (run stubRun) results() map[float64]map[string]any {
	results := map[float64]map[string]any{}
	for _, message := range run.Posted {
		if message["t"] == "result" {
			results[message["id"].(float64)] = message
		}
	}
	return results
}

func (run stubRun) at(want ...any) int {
	wanted, _ := json.Marshal(want)
	for i, entry := range run.Heard {
		if got, _ := json.Marshal(entry); string(got) == string(wanted) {
			return i
		}
	}
	return -1
}

func TestBackgroundWaitsForTheLoadFollowsTabsItsTabOpensAndNavigatesOnlyItsOwn(t *testing.T) {
	run := inStubbedChrome(t, "open")
	results := run.results()
	complete, answered := run.at("complete", 20), run.at("post", "result", 1)
	t.Logf("tab 20 complete at %d, the open answered at %d with %v", complete, answered, results[1])
	if results[1]["value"] != 20.0 || complete < 0 || answered < complete {
		t.Errorf("open answered %v at %d and the tab completed at %d; want tab 20, after complete", results[1], answered, complete)
	}
	followed, _ := json.Marshal(results[2]["value"])
	if string(followed) != `{"opened":21}` || run.at("group", []any{21}, 100) < 0 || run.at("complete", 21) > run.at("post", "result", 2) {
		t.Errorf("the click on tab 20 answered %s; want tab 21 opened, grouped and loaded before the answer", followed)
	}
	for _, message := range run.Posted {
		if tab, _ := message["tab"].(map[string]any); message["t"] == "tabUpdated" && tab["id"] == 21.0 && tab["opened"] != true {
			t.Errorf("tab 21 was posted as %v, not as tofu's", tab)
		}
	}
	refused, _ := results[4]["error"].(string)
	if results[3]["ok"] != true || run.at("navigate", 20, "https://stays.test/other") < 0 || results[4]["ok"] != false || !strings.Contains(refused, "person's") || run.at("navigate", 9, "https://stays.test/other") >= 0 {
		t.Errorf("navigate answered %v on tofu's tab and %v on the person's; want the first run and the second refused", results[3], results[4])
	}
}

func TestBackgroundInAStubbedChromeAttachesOnFirstUseGroupsAndRestores(t *testing.T) {
	run := inStubbedChrome(t, "groups")
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
		"groupUpdate": `[100,{"color":"orange","title":"tofu 🔄"}] [100,{"title":"tofu ⏸️"}] [100,{"title":"tofu ✅"}] [101,{"color":"orange","title":"tofu 🔄"}]`,
		"ungroup":     "[[9]]",
		"detach":      "[9] [3] [5] [6]",
		"badge":       `["on"] ["act"] ["on"] ["off"] ["on"] ["act"]`,
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
	results := run.results()
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

func TestTheCursorMovesBeforeTheClickOnlyWhenTheRelaySaysSoAndLeavesWithTofu(t *testing.T) {
	run := inStubbedChrome(t, "cursor")
	var order []string
	for _, entry := range run.Heard {
		switch {
		case entry[0] == "cursor":
			order = append(order, "cursor "+entry[2].(string))
		case entry[0] == "input" && entry[2] == "Input.dispatchMouseEvent":
			order = append(order, "mouse")
		}
	}
	t.Logf("heard %v", order)
	want := []string{"mouse", "mouse", "mouse", "cursor move", "mouse", "mouse", "mouse", "mouse", "mouse", "mouse", "cursor remove", "cursor remove"}
	if !slices.Equal(order, want) {
		t.Fatalf("heard %v, want %v: no cursor in the person's tab 9, the cursor before the click in tofu's tab 20, none when the call carries no cursor, and removed from both when tofu leaves", order, want)
	}
	background := shipped(t, "background.js")
	for _, must := range []string{"pointer-events: none", "aria-hidden", "mode: 'closed'", "2147483647"} {
		if !strings.Contains(background, must) {
			t.Errorf("the cursor overlay lacks %q", must)
		}
	}
	if strings.Contains(background, "await moveCursor") {
		t.Error("the act waits for the cursor")
	}
}

func TestEveryAttachTurnsOnFocusEmulationSoABackgroundTabTakesClicks(t *testing.T) {
	run := inStubbedChrome(t, "groups")
	var focus []float64
	for _, entry := range run.Heard {
		if entry[0] == "input" && entry[2] == "Emulation.setFocusEmulationEnabled" {
			focus = append(focus, entry[1].(float64))
		}
	}
	t.Logf("focus emulation was turned on for tabs %v", focus)
	for _, tab := range []float64{9, 3, 5, 6} {
		if !slices.Contains(focus, tab) {
			t.Errorf("tab %v was attached with no focus emulation; focus went to %v", tab, focus)
		}
	}
	background := shipped(t, "background.js")
	if strings.Contains(background, "active: true") || strings.Contains(background, "chrome.tabs.update(tabId, {active") {
		t.Error("background.js activates a tab")
	}
}

func TestTheGroupTitleAlwaysCarriesItsStateAndChangesOnlyWithIt(t *testing.T) {
	run := inStubbedChrome(t, "groups")
	last := map[float64]string{}
	var titles []string
	for _, entry := range run.Heard {
		if entry[0] != "groupUpdate" {
			continue
		}
		group, _ := entry[1].(float64)
		changed, _ := entry[2].(map[string]any)
		title, _ := changed["title"].(string)
		titles = append(titles, title)
		if !strings.HasPrefix(title, "tofu ") || len(title) <= len("tofu ") {
			t.Errorf("group %v was titled %q; want tofu and a state emoji, never tofu alone", group, title)
		}
		if last[group] == title {
			t.Errorf("group %v was set to %q again with no change of state", group, title)
		}
		last[group] = title
	}
	t.Logf("the group titles, in order: %q", titles)
	if want := []string{"tofu 🔄", "tofu ⏸️", "tofu ✅", "tofu 🔄"}; !slices.Equal(titles, want) {
		t.Errorf("working, waiting, finished and working again titled the groups %q; want %q", titles, want)
	}
}

func TestSnapshotExcludesPasswordFileAndHiddenInputs(t *testing.T) {
	snapshot := shipped(t, "snapshot.js")
	if !strings.Contains(snapshot, "['password', 'file', 'hidden']") || !strings.Contains(snapshot, "!excluded.includes(e.type)") {
		t.Fatal("snapshot.js does not name the password, file and hidden exclusion")
	}
}

func TestSnapshotReturnsWebLinksBesideTheElementsAndOutOfTheFingerprint(t *testing.T) {
	snapshot := shipped(t, "snapshot.js")
	fingerprint := strings.Index(snapshot, "fingerprint: hash(JSON.stringify([location.href, document.title, text, elements]))")
	if !strings.Contains(snapshot, "if (/^https?:/.test(e.href)) links[element.index] = e.href;") ||
		!strings.Contains(snapshot, "elements, guards, names, links,") || fingerprint < 0 {
		t.Fatal("snapshot.js does not return http and https links in their own map, or hashes more than the elements into the fingerprint")
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
