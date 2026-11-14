package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"tofu/internal/judge/questions"
	"tofu/internal/models/classifiers/core"
	"tofu/internal/store/walls"
	"tofu/internal/tools/web/browser/driver"
	"tofu/internal/tools/web/browser/loop"
	libraryquestions "tofu/library/questions"
)

const (
	fakeOrigin        = "chrome-extension://tofustepsbench/"
	operationQuestion = "operation"
)

type latency struct {
	native, evaluate, settle, act, jev time.Duration
}

type fakeExtension struct {
	delay     latency
	moves     []Move
	scrolled  bool
	snapshots int
}

func (f *fakeExtension) page() driver.Page {
	f.snapshots++
	page := driver.Page{
		URL: "http://127.0.0.1/", Title: "Stays", Text: fmt.Sprintf("Prices checked %d times", f.snapshots),
		Fingerprint: fmt.Sprintf("print-%d", f.snapshots), Scroll: driver.Scroll{Up: f.scrolled, Down: true},
	}
	roles := map[driver.Op]driver.Role{driver.OpClick: driver.RoleButton, driver.OpTypeText: driver.RoleTextbox, driver.OpSelect: driver.RoleSelect}
	for i, move := range f.moves {
		if move.Label == "" {
			continue
		}
		element := driver.Element{Index: i + 1, Role: roles[move.Op], Label: move.Label}
		if move.Op == driver.OpSelect {
			element.Options = []driver.SelectOption{{Label: move.Value + " guests", Value: move.Value}}
		}
		page.Elements = append(page.Elements, element)
	}
	return page
}

func (f *fakeExtension) answer(op string, args json.RawMessage) (any, driver.Timing) {
	var request struct {
		Element   int    `json:"element"`
		Direction string `json:"direction"`
	}
	_ = json.Unmarshal(args, &request)
	time.Sleep(f.delay.evaluate)
	timing := driver.Timing{EvaluateMS: millis(f.delay.evaluate)}
	switch {
	case op == "snapshot":
		return f.page(), timing
	case op == "click" && f.scrolled && f.moves[request.Element-1].Label == "Casa Flora":
		return map[string]driver.Stale{"stale": driver.StaleCovered}, timing
	case op == "scroll":
		f.scrolled = request.Direction == "down"
	}
	time.Sleep(f.delay.act + f.delay.settle)
	timing.ActMS, timing.SettleMS = millis(f.delay.act), millis(f.delay.settle)
	return struct{}{}, timing
}

func (f *fakeExtension) serve(fromHost io.Reader, toHost io.Writer, hello []byte) {
	for {
		raw, err := driver.ReadMessage(fromHost)
		if err != nil {
			return
		}
		var call struct {
			T    string          `json:"t"`
			ID   int64           `json:"id"`
			Op   string          `json:"op"`
			Args json.RawMessage `json:"args"`
		}
		if json.Unmarshal(raw, &call) != nil || call.T != "call" {
			if call.T == "wake" && driver.WriteMessage(toHost, hello) != nil {
				return
			}
			continue
		}
		value, timing := f.answer(call.Op, call.Args)
		time.Sleep(f.delay.native)
		message, _ := json.Marshal(map[string]any{"t": "result", "id": call.ID, "ok": true, "value": value, "timing": timing})
		if driver.WriteMessage(toHost, message) != nil {
			return
		}
	}
}

type scriptedJev struct {
	delay time.Duration
	moves []Move
}

func (s scriptedJev) Caps() core.WireCaps {
	return core.WireCaps{Name: "scripted", CriteriaKinds: []core.CriteriaKind{core.CriteriaString, core.CriteriaObject}}
}

func (s scriptedJev) Model() string { return "~typesafe/jev-latest" }

func (s scriptedJev) Post(_ context.Context, body []byte) (core.Raw, error) {
	started := time.Now()
	time.Sleep(s.delay)
	var posted struct {
		State struct {
			Goal string `json:"goal"`
		} `json:"state"`
		Questions map[string]struct {
			Criteria map[string]json.RawMessage `json:"criteria"`
		} `json:"questions"`
	}
	if err := json.Unmarshal(body, &posted); err != nil {
		return core.Raw{}, err
	}
	var want Move
	for _, move := range s.moves {
		if move.Goal == posted.State.Goal {
			want = move
		}
	}
	answers := map[string]any{}
	for id, asked := range posted.Questions {
		names := slices.Sorted(maps.Keys(asked.Criteria))
		choice := names[0]
		for _, name := range names {
			var target struct {
				Element string `json:"element"`
			}
			if id == strings.ToLower(want.Op.String())+"_target" && json.Unmarshal(asked.Criteria[name], &target) == nil && strings.Contains(target.Element, "] "+want.Label) {
				choice = name
			}
		}
		if id == operationQuestion {
			choice = want.Op.String()
		}
		answers[id] = map[string]any{"type": "choice", "choice": choice, "confidence": 1, "probabilities": map[string]float64{choice: 1}}
	}
	answer, err := json.Marshal(map[string]any{
		"model": "jev-scripted", "provider": "scripted", "id": "scripted",
		"usage": map[string]any{"input_tokens": len(body) / 4, "output_tokens": len(answers)}, "answers": answers,
	})
	return core.Raw{Body: answer, Attempts: 1, Latency: time.Since(started)}, err
}

func browserStep(t *testing.T) questions.Set {
	t.Helper()
	set, _, err := questions.Resolve("browser_step@1", []questions.Layer{{Name: "library", Origin: "library/questions", FS: libraryquestions.Files()}})
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func sharedTab(t *testing.T, ext *fakeExtension) driver.SharedTab {
	t.Helper()
	home, err := os.MkdirTemp("", "tb")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	manifest := filepath.Join(filepath.Dir(driver.ExtensionDir(home)), driver.HostName+".json")
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
		hostDone <- driver.Host(fakeOrigin, toHostR, fromHostW, home, func(walls.Wall) {})
		_ = fromHostW.Close()
	}()
	t.Cleanup(func() {
		_ = toHostW.Close()
		_ = fromHostR.Close()
		<-hostDone
	})
	hello := []byte(`{"t":"hello","version":2,"tabs":[{"id":7,"url":"http://127.0.0.1/","title":"Stays"}]}`)
	if err := driver.WriteMessage(toHostW, hello); err != nil {
		t.Fatal(err)
	}
	go ext.serve(fromHostR, toHostW, hello)
	client, err := driver.Dial(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(time.Millisecond) {
		if tabs, err := client.Tabs(); err == nil && len(tabs) == 1 {
			return driver.SharedTab{Client: client, ID: 7}
		}
		if time.Now().After(deadline) {
			t.Fatal("the host never listed the shared tab")
		}
	}
}

func TestTheScriptAgainstAFakeExtensionFillsEveryPhase(t *testing.T) {
	moves := Script()
	delay := latency{native: 2 * time.Millisecond, evaluate: 3 * time.Millisecond, settle: 4 * time.Millisecond, act: 5 * time.Millisecond, jev: 6 * time.Millisecond}
	tab := sharedTab(t, &fakeExtension{delay: delay, moves: moves})
	client, err := core.NewClient(core.Config{Wire: scriptedJev{delay: delay.jev, moves: moves}})
	if err != nil {
		t.Fatal(err)
	}
	rows := Run(context.Background(), tab, &loop.Jev{Client: client, Set: browserStep(t)}, moves)

	var written strings.Builder
	if err := Write(&written, rows); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(written.String(), "\n"), "\n")
	if len(lines) != len(moves) {
		t.Fatalf("the run wrote %d rows, want %d", len(lines), len(moves))
	}
	for i, line := range lines {
		var row Row
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatal(err)
		}
		ran := row.Stale == driver.StaleNone
		filled := row.Error == "" && row.ExtensionTimed && row.Calls == 2 && strings.Contains(line, `"socket_ms":`) && row.Socket >= 0 && row.Native > 0 && row.Evaluate > 0 &&
			row.Jev > 0 && row.InputTokens > 0 && row.OutputTokens > 0 && (row.Settle > 0 && row.Act > 0) == ran
		agreed := row.JevChose == moves[i].Op.String() || row.JevChose == fmt.Sprintf("%s %q", moves[i].Op, moves[i].Label)
		if !filled || !agreed || ran == (moves[i].Label == "Casa Flora") {
			t.Errorf("row %d is not filled, or jev chose %q, or the cover went wrong: %s", i+1, row.JevChose, line)
		}
		t.Log(line)
	}
	if summary := Summarise(rows); summary.Steps != 12 || summary.DidNotRun != 1 || summary.Failed != 0 || summary.Untimed != 0 {
		t.Errorf("the summary reads %+v", summary)
	}
}
