package browser

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"

	"tofu/apps/cli/output"
	cli "tofu/apps/cli/page"
	"tofu/internal/konst"
	"tofu/internal/platform/fs"
	"tofu/internal/tools/web/browser/driver"
	"tofu/internal/tools/web/browser/steps"
)

type unreadStdin struct{ t *testing.T }

func (u unreadStdin) Read([]byte) (int, error) {
	u.t.Error("host mode read stdin before checking the origin")
	return 0, io.EOF
}

var envelopeClock = regexp.MustCompile(`"at": "[^"]+"`)

func browserHome(t *testing.T) string {
	t.Helper()
	home, err := os.MkdirTemp("", "tb")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("NO_COLOR", "1")
	t.Setenv("FORCE_COLOR", "")
	return home
}

func fakeRelay(t *testing.T, home string, tabs []driver.Tab) {
	t.Helper()
	dir := filepath.Join(home, fs.StateDirName, "browser")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", filepath.Join(dir, "relay.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				in, out := json.NewDecoder(conn), json.NewEncoder(conn)
				for {
					var call struct {
						ID int64  `json:"id"`
						Op string `json:"op"`
					}
					if in.Decode(&call) != nil {
						return
					}
					value := map[string]any{"tabs": tabs, "open": 812, "close": true}[call.Op]
					if out.Encode(map[string]any{"id": call.ID, "ok": true, "value": value}) != nil {
						return
					}
				}
			}()
		}
	}()
}

func runBrowser(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := Verb(args, &out, &errOut)
	return code, stable(out.String()), errOut.String()
}

func stable(document string) string {
	return envelopeClock.ReplaceAllString(strings.ReplaceAll(document, konst.Version, "VERSION"), `"at": "AT"`)
}

func TestBrowserTabsOpenAndCloseMatchTheirTextAndJSON(t *testing.T) {
	home := browserHome(t)
	fakeRelay(t, home, []driver.Tab{
		{ID: 12, URL: "https://example.com/docs?page=2", Title: "Example Domain"},
		{ID: 812, URL: "https://lisbon.example.org/stays", Title: "Stays in Lisbon", Opened: true},
		{ID: 9, URL: "about:blank"},
	})
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"tabs"}, "" +
			"Chrome tabs · 3 reachable · 1 opened by tofu                         ✓ connected\n" +
			"\n" +
			"  ○ 12   example.com         Example Domain\n" +
			"  ● 812  lisbon.example.org  Stays in Lisbon\n" +
			"  ○ 9    about:blank\n"},
		{[]string{"tabs", "--json"}, `{
  "tofu": "VERSION",
  "verb": "browser tabs",
  "ok": true,
  "at": "AT",
  "data": {
    "tabs": [
      {
        "id": 12,
        "url": "https://example.com/docs?page=2",
        "title": "Example Domain"
      },
      {
        "id": 812,
        "url": "https://lisbon.example.org/stays",
        "title": "Stays in Lisbon",
        "opened": true
      },
      {
        "id": 9,
        "url": "about:blank",
        "title": ""
      }
    ]
  },
  "problems": []
}
`},
		{[]string{"open", "https://example.com/"}, "+ opened tab 812  https://example.com/\n"},
		{[]string{"open", "https://example.com/", "--json"}, `{
  "tofu": "VERSION",
  "verb": "browser open",
  "ok": true,
  "at": "AT",
  "data": {
    "tab": 812,
    "url": "https://example.com/"
  },
  "problems": []
}
`},
		{[]string{"close", "12"}, "- closed tab 12\n"},
		{[]string{"--json", "close", "12"}, `{
  "tofu": "VERSION",
  "verb": "browser close",
  "ok": true,
  "at": "AT",
  "data": {
    "tab": 12
  },
  "problems": []
}
`},
	}
	for _, c := range cases {
		code, out, errOut := runBrowser(t, c.args...)
		if code != output.ExitOK || errOut != "" || out != c.want {
			t.Errorf("tofu browser %v exited %d with stderr %q and printed\n%s\nwant\n%s", c.args, code, errOut, out, c.want)
		}
	}
}

func TestBrowserWithNoTabSaysWhatIsNeverListed(t *testing.T) {
	home := browserHome(t)
	fakeRelay(t, home, nil)
	want := "" +
		"Chrome tabs · 0 reachable                                            ✓ connected\n" +
		"\n" +
		"  chrome:// pages, DevTools, extensions and the web store are never listed\n"
	if code, out, errOut := runBrowser(t, "tabs"); code != output.ExitOK || out != want || errOut != "" {
		t.Fatalf("tofu browser tabs with no tab exited %d, printed\n%s\nand %q", code, out, errOut)
	}
	if _, out, _ := runBrowser(t, "tabs", "--json"); !strings.Contains(out, `"tabs": []`) {
		t.Fatalf("tofu browser tabs --json with no tab printed\n%s", out)
	}
}

func TestBrowserOnARelayThatAnswersNoStateSaysItIsOlderAndExitsOne(t *testing.T) {
	fakeRelay(t, browserHome(t), nil)
	want := "✗ the relay runs an older tofu that cannot say whether it is asleep\n" +
		"  → tofu browser install, then reload the tofu card in chrome://extensions\n"
	if code, out, errOut := runBrowser(t); code != output.ExitVerdict || out != "" || errOut != want {
		t.Errorf("tofu browser on a relay that answers no state exited %d, printed %q and on stderr\n%s\nwant\n%s", code, out, errOut, want)
	}
	code, out, _ := runBrowser(t, "--json")
	var envelope struct {
		OK       bool          `json:"ok"`
		Problems []cli.Problem `json:"problems"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil || code != output.ExitVerdict || envelope.OK || len(envelope.Problems) != 1 || !strings.Contains(envelope.Problems[0].What, "older tofu that cannot say whether it is asleep") {
		t.Errorf("tofu browser --json on a relay that answers no state exited %d and printed\n%s", code, out)
	}
}

func TestBrowserWithNoHostIsOneLineAndAHintAndTheDialErrorOnlyInJSON(t *testing.T) {
	browserHome(t)
	for _, args := range [][]string{{}, {"open", "https://example.com/"}, {"close", "12"}} {
		code, out, errOut := runBrowser(t, args...)
		want := "✗ the tofu extension is not connected\n  → tofu browser install, then load it in Chrome\n"
		if code != output.ExitVerdict || out != "" || errOut != want {
			t.Errorf("tofu browser %v exited %d, printed %q and on stderr\n%s\nwant\n%s", args, code, out, errOut, want)
		}
		code, out, errOut = runBrowser(t, append(args, "--json")...)
		var envelope struct {
			OK       bool          `json:"ok"`
			Problems []cli.Problem `json:"problems"`
		}
		if err := json.Unmarshal([]byte(out), &envelope); err != nil || code != output.ExitVerdict || errOut != "" || envelope.OK || len(envelope.Problems) != 1 ||
			!strings.HasPrefix(envelope.Problems[0].What, driver.ErrNotConnected.Error()+" (") || envelope.Problems[0].Hint != driver.InstallHint {
			t.Errorf("tofu browser %v --json exited %d, printed\n%s\nand %q", args, code, out, errOut)
		}
	}
	for _, args := range [][]string{{"open"}, {"close"}, {"close", "twelve"}, {"install", "x"}, {"open", "a", "b"}, {"bench", "--n", "0"}} {
		if code, out, errOut := runBrowser(t, args...); code != output.ExitUsage || !strings.HasPrefix(errOut, "usage: tofu browser") || out != "" {
			t.Errorf("tofu browser %v exited %d and printed %q and %q", args, code, out, errOut)
		}
	}
}

func TestBrowserNoColourWritesNoEscape(t *testing.T) {
	home := browserHome(t)
	fakeRelay(t, home, []driver.Tab{{ID: 12, URL: "https://example.com/", Title: "Example Domain"}})
	for _, noColour := range []string{"", "1"} {
		t.Setenv("FORCE_COLOR", "1")
		t.Setenv("NO_COLOR", noColour)
		_, out, _ := runBrowser(t, "tabs")
		if strings.Contains(out, "\x1b") == (noColour != "") {
			t.Errorf("with NO_COLOR=%q and FORCE_COLOR=1 tofu browser tabs printed %q", noColour, out)
		}
	}
	t.Setenv("NO_COLOR", "1")
	_ = os.RemoveAll(filepath.Join(home, fs.StateDirName))
	if _, _, errOut := runBrowser(t); strings.Contains(errOut, "\x1b") {
		t.Errorf("with NO_COLOR the not connected error printed %q", errOut)
	}
}

func TestBrowserInstallUninstallAndBenchPagesMatchTheirTextAndJSON(t *testing.T) {
	page := cli.Page{Profile: colorprofile.NoTTY, Width: konst.ProseWidthChars, Home: "/home/ada"}
	installed := installedExtension(page.Home, "/home/ada/.local/bin/tofu.exe", "jednanpboiikklhkkkimnmdmjmgjgphh")
	removed := removedExtension(page.Home)
	copyName := filepath.Base(driver.CopyPath(page.Home))
	report := benchReport{Chooser: "jev", RowsFile: "/home/ada/.tofu/bench/browser-steps-20260929-120000.jsonl", Summary: steps.Summary{
		Steps: 12, DidNotRun: 1, Failed: 2, Untimed: 3,
		Phases: []steps.Phase{{Name: "wall ms", P50: 412.5, P90: 1338.25}, {Name: "jev input tokens", P50: 1800, P90: 2310}},
	}}
	cases := []struct {
		name  string
		lines []string
		data  any
		text  string
		json  string
	}{
		{"browser install", extensionPage(page, installed), installed, "" +
			"Chrome extension                                                     ✓ installed\n" +
			"\n" +
			"  folder    ~/.tofu/browser/extension\n" +
			"  id        jednanpboiikklhkkkimnmdmjmgjgphh\n" +
			"  host      ~/.tofu/bin/" + copyName + "\n" +
			"  copy of   ~/.local/bin/tofu.exe\n" +
			"\n" +
			"next, once in Chrome\n" +
			"  1  open chrome://extensions and turn on Developer mode\n" +
			"  2  Load unpacked, pick the folder above, check the id matches\n" +
			"  3  pin the tofu icon so its badge shows\n" +
			"\n" +
			"  → tofu update keeps the copy and the extension current: nothing to run\n", `{
  "tofu": "VERSION",
  "verb": "browser install",
  "ok": true,
  "at": "AT",
  "data": {
    "installed": true,
    "folder": "/home/ada/.tofu/browser/extension",
    "id": "jednanpboiikklhkkkimnmdmjmgjgphh",
    "host": "/home/ada/.tofu/bin/` + copyName + `",
    "copy_of": "/home/ada/.local/bin/tofu.exe",
    "next": [
      "open chrome://extensions and turn on Developer mode",
      "Load unpacked, pick the folder above, check the id matches",
      "pin the tofu icon so its badge shows"
    ],
    "hint": "tofu update keeps the copy and the extension current: nothing to run"
  },
  "problems": []
}
`},
		{"browser uninstall", extensionPage(page, removed), removed, "" +
			"Chrome extension                                                       ○ removed\n" +
			"\n" +
			"  folder    ~/.tofu/browser/extension\n" +
			"\n" +
			"  → remove the tofu card in chrome://extensions\n", `{
  "tofu": "VERSION",
  "verb": "browser uninstall",
  "ok": true,
  "at": "AT",
  "data": {
    "installed": false,
    "folder": "/home/ada/.tofu/browser/extension",
    "hint": "remove the tofu card in chrome://extensions"
  },
  "problems": []
}
`},
		{"browser bench", benchPage(page, report), report, "" +
			"Browser bench · 12 steps · jev · 1 did not run                        ✗ 2 failed\n" +
			"\n" +
			"    phase                 p50      p90\n" +
			"    wall ms            412.50  1338.25\n" +
			"    jev input tokens  1800.00  2310.00\n" +
			"\n" +
			"  untimed   ⚠ 3 steps: evaluate, settle and act read 0\n" +
			"  rows      ~/.tofu/bench/browser-steps-20260929-120000.jsonl\n", `{
  "tofu": "VERSION",
  "verb": "browser bench",
  "ok": true,
  "at": "AT",
  "data": {
    "chooser": "jev",
    "steps": 12,
    "did_not_run": 1,
    "failed": 2,
    "untimed": 3,
    "phases": [
      {
        "phase": "wall ms",
        "p50": 412.5,
        "p90": 1338.25
      },
      {
        "phase": "jev input tokens",
        "p50": 1800,
        "p90": 2310
      }
    ],
    "rows_file": "/home/ada/.tofu/bench/browser-steps-20260929-120000.jsonl"
  },
  "problems": []
}
`},
	}
	for _, c := range cases {
		var text, document bytes.Buffer
		o := browserOutput{page: page, verb: c.name, out: &text}
		if code := o.show(c.data, c.lines); code != output.ExitOK || text.String() != c.text {
			t.Errorf("%s exited %d and printed\n%s\nwant\n%s", c.name, code, text.String(), c.text)
		}
		o.asJSON, o.out = true, &document
		if code := o.show(c.data, c.lines); code != output.ExitOK || strings.ReplaceAll(stable(document.String()), `\\`, "/") != c.json {
			t.Errorf("%s --json exited %d and printed\n%s\nwant\n%s", c.name, code, document.String(), c.json)
		}
	}
}

func TestBrowserMotionCaptureRefusesABadScenarioNamingTheFieldBeforeDialling(t *testing.T) {
	home := browserHome(t)
	for field, scenario := range map[string]string{
		`"rady"`:              `{"url": "http://localhost:4173/", "rady": {}, "trigger": {"action": "press", "key": "Escape"}}`,
		"viewport.width":      `{"url": "http://localhost:4173/", "viewport": {"width": "wide"}, "trigger": {"action": "press", "key": "Escape"}}`,
		"trigger.action":      `{"url": "http://localhost:4173/", "trigger": {"action": "tap"}}`,
		"url must start with": `{"url": "localhost:4173", "trigger": {"action": "press", "key": "Escape"}}`,
		driver.NotConnected:   `{"url": "http://localhost:4173/", "trigger": {"action": "press", "key": "Escape"}}`,
	} {
		path := filepath.Join(home, "scenario.json")
		if err := os.WriteFile(path, []byte(scenario), 0o644); err != nil {
			t.Fatal(err)
		}
		code, out, errOut := runBrowser(t, "motion", "capture", path, "--takes", "2", "--label", "before")
		errOut = strings.Join(strings.Fields(errOut), " ")
		if code != output.ExitVerdict || out != "" || !strings.Contains(errOut, field) || field != driver.NotConnected && strings.Contains(errOut, driver.NotConnected) {
			t.Errorf("scenario %s exited %d and printed %q and %q; want it refused naming %s", scenario, code, out, errOut, field)
		}
	}
	for _, args := range [][]string{{"motion"}, {"motion", "capture"}, {"motion", "inspect", "x.json"}, {"motion", "capture", "x.json", "--takes", "0"}} {
		if code, _, errOut := runBrowser(t, args...); code != output.ExitUsage || !strings.Contains(errOut, "usage: tofu browser motion capture <scenario.json>") {
			t.Errorf("tofu browser %v exited %d and printed %q", args, code, errOut)
		}
	}
}

func TestHostModeRefusesAWrongOriginBeforeStdin(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	var out, errOut bytes.Buffer
	code := HostVerb("chrome-extension://ponmlkjihgfedcbaponmlkjihgfedcba/", unreadStdin{t}, &out, &errOut)
	if code != output.ExitVerdict || out.Len() != 0 || !strings.HasPrefix(errOut.String(), "tofu host: ") || !strings.Contains(errOut.String(), "chrome-extension://ponmlkjihgfedcbaponmlkjihgfedcba/") {
		t.Fatalf("host mode with a wrong origin exited %d, wrote %q to Chrome and %q to stderr", code, out.String(), errOut.String())
	}
}
