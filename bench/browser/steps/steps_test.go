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

	"tofu/internal/browser"
	"tofu/internal/browser/jevloop"
	"tofu/internal/judge/jev"
	"tofu/internal/judge/question"
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

func (f *fakeExtension) page() browser.Page {
	f.snapshots++
	page := browser.Page{
		URL: "http://127.0.0.1/", Title: "Stays", Text: fmt.Sprintf("Prices checked %d times", f.snapshots),
		Fingerprint: fmt.Sprintf("print-%d", f.snapshots), Scroll: browser.Scroll{Up: f.scrolled, Down: true},
	}
	roles := map[browser.Op]browser.Role{browser.OpClick: browser.RoleButton, browser.OpTypeText: browser.RoleTextbox, browser.OpSelect: browser.RoleSelect}
	for i, move := range f.moves {
		if move.Label == "" {
			continue
		}
		element := browser.Element{Index: i + 1, Role: roles[move.Op], Label: move.Label}
		if move.Op == browser.OpSelect {
			element.Options = []browser.SelectOption{{Label: move.Value + " guests", Value: move.Value}}
		}
		page.Elements = append(page.Elements, element)
	}
	return page
}

func (f *fakeExtension) answer(op string, args json.RawMessage) (any, browser.Timing) {
	var request struct {
		Element   int    `json:"element"`
		Direction string `json:"direction"`
	}
	_ = json.Unmarshal(args, &request)
	time.Sleep(f.delay.evaluate)
	timing := browser.Timing{EvaluateMS: millis(f.delay.evaluate)}
	switch {
	case op == "snapshot":
		return f.page(), timing
	case op == "click" && f.scrolled && f.moves[request.Element-1].Label == "Casa Flora":
		return map[string]browser.Stale{"stale": browser.StaleCovered}, timing
	case op == "scroll":
		f.scrolled = request.Direction == "down"
	}
	time.Sleep(f.delay.act + f.delay.settle)
	timing.ActMS, timing.SettleMS = millis(f.delay.act), millis(f.delay.settle)
	return struct{}{}, timing
}

func (f *fakeExtension) serve(fromHost io.Reader, toHost io.Writer) {
	for {
		raw, err := browser.ReadMessage(fromHost)
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
			continue
		}
		value, timing := f.answer(call.Op, call.Args)
		time.Sleep(f.delay.native)
		message, _ := json.Marshal(map[string]any{"t": "result", "id": call.ID, "ok": true, "value": value, "timing": timing})
		if browser.WriteMessage(toHost, message) != nil {
			return
		}
	}
}

type scriptedJev struct {
	delay time.Duration
	moves []Move
}

func (s scriptedJev) Caps() jev.WireCaps {
	return jev.WireCaps{Name: "scripted", CriteriaKinds: []jev.CriteriaKind{jev.CriteriaString, jev.CriteriaObject}}
}

func (s scriptedJev) Model() string { return "~typesafe/jev-latest" }

func (s scriptedJev) Post(_ context.Context, body []byte) (jev.Raw, error) {
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
		return jev.Raw{}, err
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
	return jev.Raw{Body: answer, Attempts: 1, Latency: time.Since(started)}, err
}

func browserStep(t *testing.T) question.Set {
	t.Helper()
	set, _, err := question.Resolve("browser_step@1", []question.Layer{{Name: "library", Origin: "library/questions", FS: libraryquestions.Files()}})
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func sharedTab(t *testing.T, ext *fakeExtension) browser.SharedTab {
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
		<-hostDone
	})
	if err := browser.WriteMessage(toHostW, []byte(`{"t":"hello","version":2,"tabs":[{"id":7,"url":"http://127.0.0.1/","title":"Stays"}]}`)); err != nil {
		t.Fatal(err)
	}
	go ext.serve(fromHostR, toHostW)
	client, err := browser.Dial(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(time.Millisecond) {
		if tabs, err := client.Tabs(); err == nil && len(tabs) == 1 {
			return browser.SharedTab{Client: client, ID: 7}
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
	client, err := jev.NewClient(jev.Config{Wire: scriptedJev{delay: delay.jev, moves: moves}})
	if err != nil {
		t.Fatal(err)
	}
	rows := Run(context.Background(), tab, &jevloop.Jev{Client: client, Set: browserStep(t)}, moves)

	var written strings.Builder
	if err := Write(&written, rows); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(written.String(), "\n"), "\n")
	if len(lines) != len(moves) {
		t.Fatalf("the run wrote %d rows, want %d", len(lines), len(moves))
	}
	socket := 0.0
	for i, line := range lines {
		var row Row
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatal(err)
		}
		socket += row.Socket
		ran := row.Stale == browser.StaleNone
		filled := row.Error == "" && row.ExtensionTimed && row.Calls == 2 && row.Socket >= 0 && row.Native > 0 && row.Evaluate > 0 &&
			row.Jev > 0 && row.InputTokens > 0 && row.OutputTokens > 0 && (row.Settle > 0 && row.Act > 0) == ran
		agreed := row.JevChose == moves[i].Op.String() || row.JevChose == fmt.Sprintf("%s %q", moves[i].Op, moves[i].Label)
		if !filled || !agreed || ran == (moves[i].Label == "Casa Flora") {
			t.Errorf("row %d is not filled, or jev chose %q, or the cover went wrong: %s", i+1, row.JevChose, line)
		}
		t.Log(line)
	}
	if socket <= 0 {
		t.Errorf("the socket phase is %.3f ms over the whole run", socket)
	}
	var table strings.Builder
	if err := Table(&table, rows); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(table.String(), "12 steps, 1 did not run, 0 failed") || strings.Contains(table.String(), "timed no phase") {
		t.Errorf("the table reads\n%s", table.String())
	}
	t.Log("\n" + table.String())
}
