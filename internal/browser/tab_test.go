package browser_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"tofu/internal/browser"
	"tofu/internal/browser/jevloop"
	"tofu/internal/judge/jev"
	"tofu/internal/judge/question"
	"tofu/library/questions"
)

const fakeOrigin = "chrome-extension://tofutabtest/"

type extensionCall struct {
	ID    int64           `json:"id"`
	TabID int             `json:"tabId"`
	Op    string          `json:"op"`
	Args  json.RawMessage `json:"args"`
}

type fakeExtension struct {
	mu     sync.Mutex
	calls  []string
	answer func(extensionCall) string
}

func (f *fakeExtension) serve(fromHost io.Reader, toHost io.Writer) {
	for {
		raw, err := browser.ReadMessage(fromHost)
		if err != nil {
			return
		}
		var call extensionCall
		_ = json.Unmarshal(raw, &call)
		f.mu.Lock()
		f.calls = append(f.calls, fmt.Sprintf("tab %d %s %s", call.TabID, call.Op, string(call.Args)))
		f.mu.Unlock()
		if browser.WriteMessage(toHost, fmt.Appendf(nil, `{"t":"result","id":%d,%s}`, call.ID, f.answer(call))) != nil {
			return
		}
	}
}

func (f *fakeExtension) sawOnly(t *testing.T, want ...string) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if !slices.Equal(f.calls, want) {
		t.Fatalf("the extension saw\n%s\nwant\n%s", strings.Join(f.calls, "\n"), strings.Join(want, "\n"))
	}
}

func sharedTab(t *testing.T, answer func(extensionCall) string) (browser.SharedTab, *fakeExtension) {
	t.Helper()
	home, err := os.MkdirTemp("", "tb")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	manifest := filepath.Join(filepath.Dir(browser.ExtensionDir(home)), browser.HostName+".json")
	if err := os.MkdirAll(filepath.Dir(manifest), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, []byte(`{"allowed_origins":["`+fakeOrigin+`"]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	toHostR, toHostW := io.Pipe()
	fromHostR, fromHostW := io.Pipe()
	hostDone := make(chan error, 1)
	go func() {
		hostDone <- browser.Host(fakeOrigin, toHostR, fromHostW, home)
		_ = fromHostW.Close()
	}()
	t.Cleanup(func() {
		_ = toHostW.Close()
		_ = fromHostR.Close()
		select {
		case <-hostDone:
		case <-time.After(5 * time.Second):
			t.Error("the host did not stop")
		}
	})
	hello := `{"t":"hello","version":1,"tabs":[{"id":7,"url":"http://127.0.0.1:8000/fixture.html","title":"Forma","mode":"drive"}]}`
	if err := browser.WriteMessage(toHostW, []byte(hello)); err != nil {
		t.Fatalf("the host did not read hello: %v, %v", err, <-hostDone)
	}
	ext := &fakeExtension{answer: answer}
	go ext.serve(fromHostR, toHostW)

	client, err := browser.Dial(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(time.Millisecond) {
		tabs, err := client.Tabs()
		if err == nil && len(tabs) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the host never listed the shared tab: %v, %v", tabs, err)
		}
	}
	return browser.SharedTab{Client: client, ID: 7}, ext
}

func recordedSnapshot(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("jevloop/testdata/hotel.json")
	if err != nil {
		t.Fatal(err)
	}
	return `"ok":true,"value":` + string(raw)
}

func TestSnapshotParsesTheFixtureWithoutPasswordOrHiddenInputs(t *testing.T) {
	snapshot := recordedSnapshot(t)
	tab, ext := sharedTab(t, func(extensionCall) string { return snapshot })

	page, err := tab.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ext.sawOnly(t, "tab 7 snapshot ")
	var selects, typeable []string
	for _, element := range page.Elements {
		if element.Input == "password" || element.Input == "hidden" || element.Label == "Account password" || element.Label == "csrf" {
			t.Fatalf("the page offers %+v", element)
		}
		if element.Role == browser.RoleSelect && len(element.Options) == 3 {
			selects = append(selects, element.Label)
		}
		if browser.OpTypeText.Accepts(element.Role) {
			typeable = append(typeable, element.Label)
		}
	}
	if !slices.Equal(selects, []string{"Stay category"}) || !slices.Equal(typeable, []string{"Destination"}) || page.Fingerprint != "hotel-0" {
		t.Fatalf("selects %q, typeable %q, fingerprint %q; want the category select, the destination and hotel-0", selects, typeable, page.Fingerprint)
	}
}

func TestSnapshotRefusesAnAnswerThatIsNotAPage(t *testing.T) {
	tab, _ := sharedTab(t, func(extensionCall) string { return `"ok":true,"value":{"url":"https://a.test/"}` })
	if page, err := tab.Snapshot(context.Background()); err == nil {
		t.Fatalf("a snapshot without a fingerprint parsed into %+v", page)
	}
}

func TestActSendsTheTargetGuardAndReadsTheStaleKind(t *testing.T) {
	answers := map[string]string{
		"select": `"ok":true,"value":{"stale":"covered"}`,
		"fill":   `"ok":false,"error":"element 4 is read-only"`,
		"scroll": `"ok":true,"value":{}`,
		"click":  `"ok":true,"value":{}`,
		"wait":   `"ok":true,"value":{"stale":"moved"}`,
	}
	tab, ext := sharedTab(t, func(call extensionCall) string { return answers[call.Op] })
	page := browser.Page{Fingerprint: "hotel-0", Guards: map[int]string{5: "g5", 6: "g6"}}
	ctx := context.Background()

	if stale, err := tab.Act(ctx, page, browser.Action{Op: browser.OpSelect, Element: 6, Value: "Design"}); stale != browser.StaleCovered || err != nil {
		t.Fatalf("Act on a covered target is %q, %v; want covered and no error", stale, err)
	}
	if _, err := tab.Act(ctx, page, browser.Action{Op: browser.OpTypeText, Element: 4, Value: "Lisbon"}); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("Act on a read-only field is %v; want the error", err)
	}
	for _, action := range []browser.Action{{Op: browser.OpScrollUp}, {Op: browser.OpScrollDown}, {Op: browser.OpClick, Element: 5}} {
		if stale, err := tab.Act(ctx, page, action); stale != browser.StaleNone || err != nil {
			t.Fatalf("Act %s is %q, %v", action.Op, stale, err)
		}
	}
	if _, err := tab.Act(ctx, page, browser.Action{Op: browser.OpWait}); err == nil || !strings.Contains(err.Error(), "moved") {
		t.Fatalf("an unknown stale kind answered %v, want an error naming it", err)
	}
	for _, op := range []browser.Op{browser.OpDone, browser.OpBlocked} {
		if _, err := tab.Act(ctx, page, browser.Action{Op: op}); err == nil {
			t.Fatalf("Act %s reached the tab", op)
		}
	}

	ext.sawOnly(t,
		`tab 7 select {"element":6,"guard":"g6","value":"Design"}`,
		`tab 7 fill {"element":4,"value":"Lisbon"}`,
		`tab 7 scroll {"direction":"up"}`,
		`tab 7 scroll {"direction":"down"}`,
		`tab 7 click {"element":5,"guard":"g5"}`,
		`tab 7 wait {}`,
	)
}

type recordedWire struct{ answer []byte }

func (w recordedWire) Caps() jev.WireCaps {
	return jev.WireCaps{Name: "recorded", CriteriaKinds: []jev.CriteriaKind{jev.CriteriaString, jev.CriteriaObject}}
}

func (w recordedWire) Model() string { return "~typesafe/jev-latest" }

func (w recordedWire) Post(context.Context, []byte) (jev.Raw, error) {
	return jev.Raw{Body: w.answer, Attempts: 1, Latency: time.Millisecond}, nil
}

func TestBrowserRunsOneJevStepOnTheSharedTab(t *testing.T) {
	answer, err := os.ReadFile("jevloop/testdata/hotel_answer.json")
	if err != nil {
		t.Fatal(err)
	}
	set, _, err := question.Resolve("browser_step@1", []question.Layer{{Name: "library", Origin: "library/questions", FS: questions.Files()}})
	if err != nil {
		t.Fatal(err)
	}
	client, err := jev.NewClient(jev.Config{Wire: recordedWire{answer}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := recordedSnapshot(t)
	tab, ext := sharedTab(t, func(call extensionCall) string {
		if call.Op == "snapshot" {
			return snapshot
		}
		return `"ok":true,"value":{}`
	})

	loop := jevloop.Loop{
		Browser: jevloop.Browser{Snapshot: tab.Snapshot, Act: tab.Act},
		Choose:  jevloop.Jev{Client: client, Set: set}.Choose,
		Actions: 1,
	}
	result := loop.Run(context.Background(), "show only design stays")

	ext.sawOnly(t,
		"tab 7 snapshot ",
		`tab 7 select {"element":6,"value":"Design"}`,
		"tab 7 snapshot ",
	)
	if len(result.Steps) != 1 || result.Steps[0].Action != (browser.Action{Op: browser.OpSelect, Element: 6, Value: "Design"}) || result.Steps[0].Stale != browser.StaleNone {
		t.Fatalf("the loop recorded %+v", result.Steps)
	}
	if result.Status != jevloop.StatusBlocked || !strings.Contains(result.Reason, "action budget of 1") {
		t.Fatalf("the loop stopped %v with %q, want the action budget", result.Status, result.Reason)
	}
}

func scripted(actions ...browser.Action) jevloop.Chooser {
	return func(context.Context, string, browser.Page, []jevloop.Step) (jevloop.Choice, error) {
		action := actions[0]
		actions = actions[min(1, len(actions)-1):]
		return jevloop.Choice{Action: action}, nil
	}
}

func TestACoveredTargetStopsBlockedAfterThreeDecisions(t *testing.T) {
	snapshot := recordedSnapshot(t)
	tab, _ := sharedTab(t, func(call extensionCall) string {
		if call.Op == "snapshot" {
			return snapshot
		}
		return `"ok":true,"value":{"stale":"covered"}`
	})
	result := jevloop.Loop{
		Browser: jevloop.Browser{Snapshot: tab.Snapshot, Act: tab.Act},
		Choose:  scripted(browser.Action{Op: browser.OpClick, Element: 5}),
		Actions: 30,
	}.Run(context.Background(), "find stays")
	t.Logf("stopped %v after %d decisions: %s", result.Status, result.Decisions, result.Reason)
	if result.Status != jevloop.StatusBlocked || result.Decisions != 3 || !strings.Contains(result.Reason, "covered") {
		t.Fatalf("stopped %v after %d decisions with %q, want blocked after 3 naming covered", result.Status, result.Decisions, result.Reason)
	}
}

func TestAPageWhoseFingerprintAlwaysMovesStillClicksOnceAndIsDone(t *testing.T) {
	var page map[string]any
	if err := json.Unmarshal([]byte(strings.TrimPrefix(recordedSnapshot(t), `"ok":true,"value":`)), &page); err != nil {
		t.Fatal(err)
	}
	snapshots := 0
	tab, ext := sharedTab(t, func(call extensionCall) string {
		if call.Op == "snapshot" {
			snapshots++
			page["fingerprint"] = fmt.Sprint("tick-", snapshots)
			raw, _ := json.Marshal(page)
			return `"ok":true,"value":` + string(raw)
		}
		return `"ok":true,"value":{}`
	})
	result := jevloop.Loop{
		Browser: jevloop.Browser{Snapshot: tab.Snapshot, Act: tab.Act},
		Choose:  scripted(browser.Action{Op: browser.OpClick, Element: 5}, browser.Action{Op: browser.OpDone}),
		Actions: 30,
	}.Run(context.Background(), "find stays")
	ext.mu.Lock()
	clicks := slices.DeleteFunc(slices.Clone(ext.calls), func(call string) bool { return !strings.Contains(call, " click ") })
	ext.mu.Unlock()
	t.Logf("stopped %v after %d decisions and %d clicks: %s", result.Status, result.Decisions, len(clicks), result.Reason)
	if result.Status != jevloop.StatusDone || len(clicks) != 1 || result.Decisions != 2 {
		t.Fatalf("stopped %v after %d decisions and %d clicks, want done after 2 and 1", result.Status, result.Decisions, len(clicks))
	}
}
