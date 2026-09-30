package extension_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/png"
	"io/fs"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"tofu/internal/browser"
	"tofu/internal/browser/extension"
	"tofu/internal/konst"
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
	Heard     [][]any          `json:"heard"`
	Posted    []map[string]any `json:"posted"`
	Grouped   map[string]any   `json:"grouped"`
	Restored  map[string]any   `json:"restored"`
	Listening []int            `json:"listening"`
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

func TestATabTheSiteOpensWithNoOpenerDuringTofusClickIsTofus(t *testing.T) {
	run := inStubbedChrome(t, "orphan")
	ours := map[float64]bool{}
	for _, message := range run.Posted {
		if tab, _ := message["tab"].(map[string]any); message["t"] == "tabUpdated" && tab != nil {
			ours[tab["id"].(float64)] = tab["opened"] == true
		}
	}
	t.Logf("tabs posted as tofu's: %v", ours)
	if !ours[21] || !ours[22] || ours[40] {
		t.Fatalf("tab 21 from tofu's relayed click is tofu's = %v; tab 22 from tofu's click op = %v; tab 40, opened later by the person, = %v", ours[21], ours[22], ours[40])
	}
}

func TestALinkToABlankTargetAndWindowOpenLoadInTofusTabAndCreateNoTab(t *testing.T) {
	run := inStubbedChrome(t, "keep")
	t.Logf("heard: %v", run.Heard)
	if created := run.at("created", 21); created >= 0 {
		t.Errorf("a page act in tofu's tab created tab 21 at %d", created)
	}
	if run.at("load", 20, "https://stays.test/rooms/1") < 0 {
		t.Error("the target=_blank link did not load its href in tofu's tab 20")
	}
	if run.at("load", 20, "https://stays.test/rooms/2") < 0 {
		t.Error("window.open did not load its url in tofu's tab 20")
	}
	for _, entry := range run.Heard {
		if entry[0] == "keep" && entry[1] == 9.0 {
			t.Fatalf("the person's tab 9 got the keep script: %v", entry)
		}
	}
}

func TestTheCursorMovesBeforeTheClickOnlyWhenTheRelaySaysSoAndLeavesWithTofu(t *testing.T) {
	run := inStubbedChrome(t, "cursor")
	var order []string
	for _, entry := range run.Heard {
		switch {
		case entry[0] == "cursor":
			order = append(order, strings.TrimSpace("cursor "+entry[2].(string)+" "+entry[3].(string)))
		case entry[0] == "input" && entry[2] == "Input.dispatchMouseEvent":
			order = append(order, "mouse")
		}
	}
	t.Logf("heard %v", order)
	want := []string{"mouse", "mouse", "mouse", "cursor move tofu", "mouse", "mouse", "mouse", "mouse", "mouse", "mouse",
		"cursor move tofu typing", "cursor move tofu → www.airbnb.test", "cursor remove", "cursor remove"}
	if !slices.Equal(order, want) {
		t.Fatalf("heard %v, want %v: no cursor in the person's tab 9, the cursor before the click in tofu's tab 20, none when the call carries no cursor, tofu typing at a fill's point, the host after a navigate, and removed from both when tofu leaves", order, want)
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

func TestThePillReadsThinkingBetweenActsAndTheNextActClearsIt(t *testing.T) {
	run := inStubbedChrome(t, "thinking")
	var painted []string
	for _, entry := range run.Heard {
		if entry[0] == "cursor" {
			painted = append(painted, fmt.Sprint(entry[1], " ", entry[3]))
		}
	}
	t.Logf("painted %q", painted)
	if want := []string{"20 tofu", "20 tofu thinking", "20 tofu typing"}; !slices.Equal(painted, want) {
		t.Fatalf("painted %q; want the act, thinking after it, the next act's label, and nothing on the person's tab 9", painted)
	}
	background := shipped(t, "background.js")
	if !strings.Contains(background, "prefers-reduced-motion") || strings.Contains(background, "rotate") {
		t.Error("the thinking pulse spins, or ignores reduced motion")
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

func TestTheScreencastNeverSendsAChunkOverOneMegabyte(t *testing.T) {
	background := shipped(t, "background.js")
	if !strings.Contains(handler(t, background, "screencast"), "post({t: 'frames', id, frames})") {
		t.Error("the screencast handler does not post the chunks before the last as frames messages")
	}
	chunkLine := regexp.MustCompile(`(?m)^const SCREENCAST_CHUNK_BYTES = (\d+);$`).FindStringSubmatch(background)
	if chunkLine == nil {
		t.Fatal("background.js declares no SCREENCAST_CHUNK_BYTES")
	}
	chunkBytes, _ := strconv.Atoi(chunkLine[1])
	start := strings.Index(background, "function screencastChunks(")
	end := strings.Index(background[max(start, 0):], "\n}\n")
	if chunkBytes <= 0 || chunkBytes > konst.BrowserHostMessageBytes || start < 0 || end < 0 {
		t.Fatalf("the chunk size is %d bytes and screencastChunks is at %d; want at most %d bytes and the function present", chunkBytes, start, konst.BrowserHostMessageBytes)
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("no node on the PATH to run screencastChunks")
	}
	driver := `
const frames = Array.from({length: 9}, (_, i) => ({data: 'x'.repeat(i === 4 ? SCREENCAST_CHUNK_BYTES - 64 : 300000), timestamp: 1000 + i / 60}));
const chunks = screencastChunks(frames);
const widest = frames => Math.max(JSON.stringify({t: 'frames', id: Number.MAX_SAFE_INTEGER, frames}).length,
  JSON.stringify({t: 'result', id: Number.MAX_SAFE_INTEGER, ok: true, value: frames, timing: {evaluate_ms: 1e300, settle_ms: 1e300, act_ms: 1e300}}).length);
let oversize = '';
try { screencastChunks([{data: 'x'.repeat(SCREENCAST_CHUNK_BYTES), timestamp: 1}]); } catch (error) { oversize = error.message; }
console.log(JSON.stringify({sizes: chunks.map(widest), sent: chunks.flat().map(frame => frame.timestamp), wanted: frames.map(frame => frame.timestamp), empty: screencastChunks([]), oversize}));
`
	out, err := exec.Command(node, "-e", chunkLine[0]+"\n"+background[start:start+end+2]+driver).Output()
	if err != nil {
		t.Fatalf("screencastChunks under node: %v", err)
	}
	var run struct {
		Sizes    []int
		Sent     []float64
		Wanted   []float64
		Empty    [][]any
		Oversize string
	}
	if err := json.Unmarshal(out, &run); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	t.Logf("chunk size %d bytes, messages of %v bytes", chunkBytes, run.Sizes)
	if len(run.Sizes) < 3 || slices.Max(run.Sizes) > konst.BrowserHostMessageBytes {
		t.Errorf("nine frames went out as messages of %v bytes; want at least three, none over %d", run.Sizes, konst.BrowserHostMessageBytes)
	}
	if !slices.Equal(run.Sent, run.Wanted) || len(run.Empty) != 1 || len(run.Empty[0]) != 0 || run.Oversize == "" {
		t.Errorf("sent %v for %v, no frames chunked as %v, and one frame over the cap said %q", run.Sent, run.Wanted, run.Empty, run.Oversize)
	}
}

func TestAScreencastInTheStubAcksOnlyItsTabAndListensOnlyWhileRecording(t *testing.T) {
	run := inStubbedChrome(t, "screencast")
	results := run.results()
	stopped, _ := json.Marshal(results[2]["value"])
	t.Logf("stop answered %s; listeners after start, stop, start, close, start, detach: %v", stopped, run.Listening)
	if results[1]["ok"] != true || string(stopped) != `[{"data":"AAE=","timestamp":1000.5},{"data":"AAE=","timestamp":1000.6}]` {
		t.Errorf("start answered %v and stop %s; want tab 9's two frames in order and nothing from tab 3", results[1], stopped)
	}
	acks := map[float64]int{}
	for _, entry := range run.Heard {
		if entry[0] == "input" && entry[2] == "Page.screencastFrameAck" {
			acks[entry[1].(float64)]++
		}
	}
	if acks[9] != 10 || acks[3] != 0 || run.at("input", 9, "Page.startScreencast") < 0 || run.at("input", 9, "Page.stopScreencast") < 0 {
		t.Errorf("acks by tab %v; want ten on tab 9 (its two frames and eight spare on the first), none on tab 3, and the start and stop sent to tab 9", acks)
	}
	if !slices.Equal(run.Listening, []int{1, 0, 1, 0, 1, 0}) || results[5]["ok"] != true {
		t.Errorf("listeners %v and a start after the detach answered %v; want the listener gone after a stop, a closed tab and a detach", run.Listening, results[5])
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
