package browser

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"tofu/internal/konst"
)

const testOrigin = "chrome-extension://abcdefghijklmnopabcdefghijklmnop/"

type fakeExtension struct {
	t      *testing.T
	toHost *io.PipeWriter
	heard  <-chan toExtension
}

func (f fakeExtension) send(message string) {
	f.t.Helper()
	if err := WriteMessage(f.toHost, []byte(message)); err != nil {
		f.t.Fatalf("the host did not read %s: %v", message, err)
	}
}

func (f fakeExtension) answer(id int64, fields string) {
	f.t.Helper()
	f.send(`{"t":"result","id":` + strconv.FormatInt(id, 10) + `,` + fields + `}`)
}

func (f fakeExtension) next() toExtension {
	f.t.Helper()
	select {
	case message, open := <-f.heard:
		if !open {
			f.t.Fatal("the host stopped writing to the extension")
		}
		return message
	case <-time.After(5 * time.Second):
		f.t.Fatal("the host wrote nothing to the extension")
	}
	return toExtension{}
}

func (f fakeExtension) call() toExtension {
	f.t.Helper()
	for {
		if message := f.next(); message.T == messageCall {
			return message
		}
	}
}

func shortHome(t *testing.T) string {
	t.Helper()
	home, err := os.MkdirTemp("", "tb")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	return home
}

func installFor(t *testing.T, home, origin string) {
	t.Helper()
	_, manifestPath := installPaths(home)
	raw, err := json.Marshal(hostManifest{Name: HostName, Type: "stdio", AllowedOrigins: []string{origin}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func socketOf(t *testing.T, home string) string {
	t.Helper()
	path, err := socketPath(home)
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func startHost(t *testing.T, home string) (fakeExtension, <-chan error) {
	t.Helper()
	build, err := Build()
	if err != nil {
		t.Fatal(err)
	}
	return startHostOn(t, home, build)
}

func startHostOn(t *testing.T, home, build string) (fakeExtension, <-chan error) {
	t.Helper()
	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	done := make(chan error, 1)
	go func() {
		err := host(testOrigin, stdinR, stdoutW, home, build, func(string) (string, error) { return Build() })
		_ = stdinR.CloseWithError(errors.New("the host returned"))
		_ = stdoutW.Close()
		done <- err
	}()
	heard := make(chan toExtension, 64)
	go func() {
		defer close(heard)
		for {
			raw, err := ReadMessage(stdoutR)
			if err != nil {
				return
			}
			var message toExtension
			if err := json.Unmarshal(raw, &message); err != nil {
				t.Errorf("the host wrote %s to the extension: %v", raw, err)
				return
			}
			heard <- message
		}
	}()
	t.Cleanup(func() {
		_ = stdinW.Close()
		_ = stdoutR.Close()
	})
	return fakeExtension{t, stdinW, heard}, done
}

func dial(t *testing.T, home string) *Client {
	t.Helper()
	client, err := Dial(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

type answer struct {
	value json.RawMessage
	err   error
}

func callAsync(client *Client, tab int, op string, args json.RawMessage) <-chan answer {
	answered := make(chan answer, 1)
	go func() {
		value, err := client.Call(tab, op, args)
		answered <- answer{value, err}
	}()
	return answered
}

func TestHostRelaysAndClaims(t *testing.T) {
	home := shortHome(t)
	installFor(t, home, testOrigin)
	ext, done := startHost(t, home)
	ext.send(`{"t":"hello","version":2,"tabs":[{"id":7,"url":"https://a.test/","title":"A"},{"id":4,"url":"chrome://settings/","title":"Settings"}]}`)
	ext.send(`{"t":"tabUpdated","tab":{"id":9,"url":"https://b.test/","title":"B"}}`)
	ext.send(`{"t":"tabUpdated","tab":{"id":9,"url":"https://b.test/next","title":"B next"}}`)
	ext.send(`{"t":"result","id":999,"ok":true,"value":"nobody asked"}`)

	first, second := dial(t, home), dial(t, home)
	tabs, err := first.Tabs()
	want := []Tab{{7, "https://a.test/", "A", false}, {9, "https://b.test/next", "B next", false}}
	if err != nil || !slices.Equal(tabs, want) {
		t.Fatalf("Tabs is %v, %v; want %v", tabs, err, want)
	}

	clicked := callAsync(first, 7, "click", json.RawMessage(`{"element":3}`))
	firstCall := ext.call()
	if firstCall.TabID != 7 || firstCall.Op != "click" || string(firstCall.Args) != `{"element":3}` {
		t.Fatalf("the extension got %+v", firstCall)
	}
	read := callAsync(second, 7, "snapshot", nil)
	secondCall := ext.call()
	if secondCall.TabID != 7 || secondCall.Op != "snapshot" || secondCall.ID == firstCall.ID {
		t.Fatalf("the extension got %+v after %+v", secondCall, firstCall)
	}
	ext.answer(secondCall.ID, `"ok":true,"value":"second page"`)
	ext.answer(firstCall.ID, `"ok":true,"value":"first clicked"`)
	for _, got := range []struct {
		answered <-chan answer
		want     string
	}{{read, `"second page"`}, {clicked, `"first clicked"`}} {
		select {
		case a := <-got.answered:
			if a.err != nil || string(a.value) != got.want {
				t.Fatalf("a session read %s, %v; want %s", a.value, a.err, got.want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("no answer %s came back", got.want)
		}
	}

	if _, err := second.Call(7, "click", nil); err == nil || !strings.Contains(err.Error(), "another tofu session") {
		t.Fatalf("a drive op on a tab the first session claimed returned %v", err)
	}
	if _, err := first.Call(5, "snapshot", nil); err == nil || !strings.Contains(err.Error(), "no tab 5") {
		t.Fatalf("an op on a tab Chrome does not have returned %v", err)
	}
	if _, err := first.Call(7, "reload", nil); err == nil || !strings.Contains(err.Error(), "unknown browser op") {
		t.Fatalf("an unknown op returned %v", err)
	}
	failed := callAsync(first, 9, "snapshot", nil)
	failedCall := ext.call()
	if failedCall.Op != "snapshot" || failedCall.TabID != 9 {
		t.Fatalf("a refused op reached the extension: %+v", failedCall)
	}
	ext.answer(failedCall.ID, `"ok":false,"error":"the page changed"`)
	if a := <-failed; a.err == nil || !strings.Contains(a.err.Error(), "the page changed") {
		t.Fatalf("a failed result came back as %s, %v", a.value, a.err)
	}

	_ = first.Close()
	reached := make(chan toExtension, 1)
	go func() {
		for message := range ext.heard {
			if message.T == messageCall {
				reached <- message
				return
			}
		}
	}()
	deadline := time.Now().Add(5 * time.Second)
	for driven := false; !driven; {
		attempt := callAsync(second, 7, "click", nil)
		select {
		case a := <-attempt:
			if a.err == nil || !strings.Contains(a.err.Error(), "another tofu session") || time.Now().After(deadline) {
				t.Fatalf("the claim was not released when its session closed: %v", a.err)
			}
			time.Sleep(10 * time.Millisecond)
		case call := <-reached:
			if call.Op != "click" || call.TabID != 7 {
				t.Fatalf("the second session's drive op reached the extension as %+v", call)
			}
			ext.answer(call.ID, `"ok":true`)
			if a := <-attempt; a.err != nil {
				t.Fatalf("the second session could not drive after the first closed: %v", a.err)
			}
			driven = true
		}
	}

	ext.send(`{"t":"tabRemoved","tabId":9}`)
	ext.send(`{"t":"result","id":998,"ok":true}`)
	if tabs, err := second.Tabs(); err != nil || !slices.Equal(tabs, want[:1]) {
		t.Fatalf("after tabRemoved, Tabs is %v, %v", tabs, err)
	}

	_ = ext.toHost.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("the host ended with %v when stdin closed", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the host did not end when stdin closed")
	}
	if _, err := os.Stat(socketOf(t, home)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the socket survived the host: %v", err)
	}
	if _, err := second.Tabs(); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("a session of a host that ended read %v", err)
	}
}

func TestHostOpensAndClosesOnlyTheTabsTofuOpened(t *testing.T) {
	home := shortHome(t)
	installFor(t, home, testOrigin)
	ext, _ := startHost(t, home)
	ext.send(`{"t":"hello","version":2,"tabs":[{"id":7,"url":"https://mine.test/","title":"Mine"},{"id":5,"url":"https://kept.test/","title":"Kept","opened":true}]}`)
	client := dial(t, home)

	for _, url := range []string{"javascript:alert(1)", "JAVASCRIPT:alert(1)", "chrome://settings", "data:text/html,x", ""} {
		if _, err := client.Open(url); err == nil || !strings.Contains(err.Error(), "http, https and file") {
			t.Fatalf("open %q returned %v", url, err)
		}
	}
	for _, tab := range []int{7, 4} {
		if err := client.CloseTab(tab); err == nil || !strings.Contains(err.Error(), "the person's") {
			t.Fatalf("close of tab %d returned %v", tab, err)
		}
	}

	opened := make(chan answer, 1)
	go func() {
		tab, err := client.Open("https://example.com/")
		opened <- answer{json.RawMessage(strconv.Itoa(tab)), err}
	}()
	create := ext.call()
	if create.Op != "open" || create.TabID != 0 || string(create.Args) != `{"url":"https://example.com/"}` {
		t.Fatalf("the first call to reach the extension is %+v", create)
	}
	ext.send(`{"t":"tabUpdated","tab":{"id":12,"url":"","title":"","opened":true}}`)
	ext.answer(create.ID, `"ok":true,"value":12`)
	if a := <-opened; a.err != nil || string(a.value) != "12" {
		t.Fatalf("open returned tab %s, %v", a.value, a.err)
	}
	tabs, err := client.Tabs()
	if err != nil || len(tabs) != 3 || tabs[2] != (Tab{12, "", "", true}) {
		t.Fatalf("after open, Tabs is %v, %v", tabs, err)
	}

	for _, tab := range []int{12, 5} {
		closed := make(chan error, 1)
		go func() { closed <- client.CloseTab(tab) }()
		remove := ext.call()
		if remove.Op != "close" || remove.TabID != tab || remove.Args != nil {
			t.Fatalf("close of tab %d reached the extension as %+v", tab, remove)
		}
		ext.answer(remove.ID, `"ok":true`)
		if err := <-closed; err != nil {
			t.Fatalf("close of tab %d returned %v", tab, err)
		}
	}
}

func TestHostDrivesAnyTabWithNoShareStepAndSaysWhatItIsDoing(t *testing.T) {
	home := shortHome(t)
	installFor(t, home, testOrigin)
	ext, _ := startHost(t, home)
	ext.send(`{"t":"hello","version":2,"tabs":[{"id":9,"url":"https://stays.test/","title":"Stays"},` +
		`{"id":2,"url":"chrome://settings/","title":"Settings"},{"id":3,"url":"DEVTOOLS://devtools/bundled/inspector.html","title":"DevTools"},` +
		`{"id":4,"url":"chrome-extension://jednanpboiikklhkkkimnmdmjmgjgphh/x.html","title":"An extension"},` +
		`{"id":5,"url":"https://chromewebstore.google.com/detail/x","title":"Store"},{"id":6,"url":"https://chrome.google.com/webstore/detail/x","title":"Old store"},` +
		`{"id":8,"url":"https://leaves.test/","title":"Leaves"}]}`)
	ext.send(`{"t":"tabUpdated","tab":{"id":8,"url":"chrome://newtab/","title":"New tab"}}`)
	client := dial(t, home)

	tabs, err := client.Tabs()
	if err != nil || len(tabs) != 1 || tabs[0] != (Tab{9, "https://stays.test/", "Stays", false}) {
		t.Fatalf("Tabs is %v, %v; want tab 9 alone", tabs, err)
	}
	for _, tab := range []int{2, 3, 4, 5, 6, 8} {
		for _, op := range []string{"snapshot", "click"} {
			if _, err := client.Call(tab, op, nil); err == nil || !strings.Contains(err.Error(), "never reads or drives") {
				t.Fatalf("%s on tab %d returned %v", op, tab, err)
			}
		}
	}

	read := callAsync(client, 9, "snapshot", nil)
	if heard := ext.next(); heard.T != messageStatus || heard.State != statusReading {
		t.Fatalf("before the snapshot the extension heard %+v; want the status reading, and no refused op reached it", heard)
	}
	snapshot := ext.call()
	ext.answer(snapshot.ID, `"ok":true,"value":{}`)
	if a := <-read; a.err != nil {
		t.Fatal(a.err)
	}
	reread := callAsync(client, 9, "snapshot", nil)
	again := ext.next()
	if again.T != messageCall {
		t.Fatalf("a second snapshot sent %+v; want the call alone, with no second reading", again)
	}
	ext.answer(again.ID, `"ok":true,"value":{}`)
	<-reread
	var timed CallTime
	client.Timed = func(call CallTime) { timed = call }
	clicked := callAsync(client, 9, "click", json.RawMessage(`{"element":1}`))
	if heard := ext.next(); heard.T != messageStatus || heard.State != statusActing {
		t.Fatalf("before the click the extension heard %+v; want the status acting", heard)
	}
	click := ext.call()
	if click.TabID != 9 || click.Op != "click" {
		t.Fatalf("the extension got %+v", click)
	}
	ext.answer(click.ID, `"ok":true,"value":{},"timing":{"evaluate_ms":1,"settle_ms":2,"act_ms":3}`)
	if a := <-clicked; a.err != nil || timed.Extension == nil || *timed.Extension != (Timing{1, 2, 3}) {
		t.Fatalf("the click returned %v with the timing %+v", a.err, timed.Extension)
	}
	answered := time.Now()
	if heard := ext.next(); heard.T != messageStatus || heard.State != statusIdle || time.Since(answered) < idleAfter/2 {
		t.Fatalf("after the click the extension heard %+v %v later; want the status idle after %v", heard, time.Since(answered), idleAfter)
	}
}

type unreadable struct{ t *testing.T }

func (u unreadable) Read([]byte) (int, error) {
	u.t.Error("the host read stdin")
	return 0, io.EOF
}

func TestHostRefusesAWrongOrigin(t *testing.T) {
	for name, installed := range map[string]string{"another extension": "chrome-extension://ponmlkjihgfedcbaponmlkjihgfedcba/", "nothing installed": ""} {
		t.Run(name, func(t *testing.T) {
			home := shortHome(t)
			if installed != "" {
				installFor(t, home, installed)
			}
			if err := Host(testOrigin, unreadable{t}, io.Discard, home); err == nil || !strings.Contains(err.Error(), testOrigin) {
				t.Fatalf("Host returned %v for an origin that is not installed", err)
			}
			if _, err := os.Stat(socketOf(t, home)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("a refused host left a socket: %v", err)
			}
		})
	}
}

func TestHostFailsOnABadMessage(t *testing.T) {
	for name, message := range map[string]string{
		"unknown type": `{"t":"navigate","url":"https://a.test/"}`,
		"a shared tab": `{"t":"shared","tab":{"id":1},"mode":"drive"}`,
		"old protocol": `{"t":"hello","version":1,"tabs":[{"id":1,"mode":"drive"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			home := shortHome(t)
			installFor(t, home, testOrigin)
			ext, done := startHost(t, home)
			ext.send(message)
			select {
			case err := <-done:
				if err == nil {
					t.Fatalf("the host ended cleanly on %s", message)
				}
			case <-time.After(5 * time.Second):
				t.Fatalf("the host kept running after %s", message)
			}
		})
	}
}

func TestSecondHostKeepsOffTheSocket(t *testing.T) {
	home := shortHome(t)
	installFor(t, home, testOrigin)
	ext, _ := startHost(t, home)
	ext.send(`{"t":"hello","version":2,"tabs":[{"id":3,"url":"https://a.test/","title":"A"}]}`)
	ext.send(`{"t":"result","id":1,"ok":true}`)
	if err := Host(testOrigin, unreadable{t}, io.Discard, home); err == nil || !strings.Contains(err.Error(), "already") {
		t.Fatalf("a second host returned %v", err)
	}
	if tabs, err := dial(t, home).Tabs(); err != nil || len(tabs) != 1 || tabs[0].ID != 3 {
		t.Fatalf("after a second host tried, the first serves %v, %v", tabs, err)
	}
}

func homeWithSocketPathOf(t *testing.T, length int) (string, string) {
	t.Helper()
	base := shortHome(t)
	tail := len(filepath.Join(base, "x", ".tofu", "browser", "relay.sock")) - 1
	home := filepath.Join(base, strings.Repeat("p", length-tail))
	path := filepath.Join(home, ".tofu", "browser", "relay.sock")
	if len(path) != length {
		t.Fatalf("the padded socket path is %d bytes, want %d", len(path), length)
	}
	return home, path
}

func TestSocketPathOverTheLimitIsNamed(t *testing.T) {
	home, path := homeWithSocketPathOf(t, 108)
	installFor(t, home, testOrigin)
	for name, err := range map[string]error{
		"Host": Host(testOrigin, unreadable{t}, io.Discard, home),
		"Dial": func() error { _, err := Dial(home); return err }(),
	} {
		if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "108 bytes") || !strings.Contains(err.Error(), "107") {
			t.Errorf("%s with a 108 byte socket path returned %v", name, err)
		}
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a refused host left a socket: %v", err)
	}

	home, _ = homeWithSocketPathOf(t, 107)
	if _, err := Dial(home); !errors.Is(err, ErrNotConnected) || strings.Contains(err.Error(), "limit") {
		t.Fatalf("Dial with a 107 byte socket path and no host returned %v", err)
	}
}

func TestStaleSocketIsNotConnectedAndIsReplaced(t *testing.T) {
	home := shortHome(t)
	installFor(t, home, testOrigin)
	if _, err := Dial(home); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("Dial with no socket returned %v", err)
	}
	path := socketOf(t, home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	listener.(*net.UnixListener).SetUnlinkOnClose(false)
	_ = listener.Close()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the stale socket file is not there: %v", err)
	}
	if _, err := Dial(home); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("Dial on a socket nobody answers returned %v", err)
	}
	ext, _ := startHost(t, home)
	ext.send(`{"t":"hello","version":2,"tabs":[]}`)
	if tabs, err := dial(t, home).Tabs(); err != nil || len(tabs) != 0 {
		t.Fatalf("a host over a stale socket serves %v, %v", tabs, err)
	}
}

func TestTheCursorSettingRidesOnEveryCDPCallAndOffSendsNone(t *testing.T) {
	for setting, want := range map[string]bool{"": true, `{"browserCursor": 0}`: false} {
		home := shortHome(t)
		installFor(t, home, testOrigin)
		if setting != "" {
			if err := os.WriteFile(filepath.Join(home, ".tofu", "settings.json"), []byte(setting), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		ext, _ := startHost(t, home)
		ext.send(`{"t":"hello","version":2,"tabs":[{"id":7,"url":"https://a.test/","title":"A"}]}`)
		sent := callAsync(dial(t, home), 7, opCDP, json.RawMessage(`{"calls":[{"method":"Input.dispatchMouseEvent","params":{"type":"mousePressed","x":5,"y":5}}],"act":true}`))
		call := ext.call()
		ext.answer(call.ID, `"ok":true,"value":[{"result":{}}]`)
		<-sent
		if call.Cursor != want {
			t.Fatalf("with the settings file %q the call carried cursor = %v, want %v", setting, call.Cursor, want)
		}
	}
}

func TestTheFocusEmulationCallsPassTheRelay(t *testing.T) {
	home := shortHome(t)
	installFor(t, home, testOrigin)
	ext, _ := startHost(t, home)
	ext.send(`{"t":"hello","version":2,"tabs":[{"id":7,"url":"https://a.test/","title":"A"}]}`)
	client := dial(t, home)
	for _, method := range []string{"Emulation.setFocusEmulationEnabled", "Page.setWebLifecycleState"} {
		sent := callAsync(client, 7, opCDP, json.RawMessage(`{"calls":[{"method":"`+method+`"}]}`))
		call := ext.call()
		if call.Op != opCDP || !strings.Contains(string(call.Args), method) {
			t.Fatalf("%s reached the extension as %+v", method, call)
		}
		ext.answer(call.ID, `"ok":true,"value":[{"result":{}}]`)
		if answer := <-sent; answer.err != nil {
			t.Fatalf("%s was refused: %v", method, answer.err)
		}
	}
}

func TestAnOlderClientIsRefusedAndTheCurrentRelayKeepsServing(t *testing.T) {
	home := shortHome(t)
	installFor(t, home, testOrigin)
	ext, done := startHost(t, home)
	helloFrom(ext, "")
	client := dial(t, home)
	if _, err := client.Call(0, opHello, json.RawMessage(`{"build":"0ld"}`)); err == nil || !strings.Contains(err.Error(), "this tofu is older than the relay: restart it") {
		t.Fatalf("a client on build 0ld said hello to the current relay and got %v; want it refused as older", err)
	}
	select {
	case err := <-done:
		t.Fatalf("the current relay exited with %v when an older client dialled it", err)
	case <-time.After(200 * time.Millisecond):
	}
	if tabs, err := dial(t, home).Tabs(); err != nil || len(tabs) != 1 {
		t.Fatalf("after the older client, the relay serves %v, %v", tabs, err)
	}
}

func TestANewerClientRestartsAStaleRelayAndTheReconnectRewrites(t *testing.T) {
	home := shortHome(t)
	installFor(t, home, testOrigin)
	stale := staleInstall(t, home)
	old, oldDone := startHostOn(t, home, "0ld")
	helloFrom(old, "0ld")

	if client, err := Dial(home); !errors.Is(err, ErrRelayRestarting) {
		if client != nil {
			_ = client.Close()
		}
		t.Fatalf("a client on this build dialled a relay on build 0ld and got %v; want ErrRelayRestarting", err)
	}
	select {
	case err := <-oldDone:
		if err != nil {
			t.Fatalf("the stale relay exited with %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the stale relay kept serving after a newer client dialled it")
	}
	if _, err := os.Stat(socketOf(t, home)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the stale relay left its socket: %v", err)
	}

	fresh, _ := startHost(t, home)
	fresh.send(`{"t":"hello","version":2,"build":"0ld","tabs":[]}`)
	if heard := fresh.next(); heard.T != messageReload {
		t.Fatalf("the extension reconnected to the new relay and heard %+v; want reload", heard)
	}
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the new relay did not rewrite the extension: %v", err)
	}
	client, err := Dial(home)
	if err != nil {
		t.Fatalf("a client on the relay's own build was refused: %v", err)
	}
	_ = client.Close()
}

func staleInstall(t *testing.T, home string) string {
	t.Helper()
	extensionDir, _ := installPaths(home)
	if err := os.MkdirAll(extensionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(extensionDir, "stale.js")
	if err := os.WriteFile(stale, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	return stale
}

func helloFrom(ext fakeExtension, build string) {
	ext.send(`{"t":"hello","version":2,"build":"` + build + `","tabs":[{"id":7,"url":"https://a.test/","title":"A"}]}`)
	ext.send(`{"t":"result","id":999,"ok":true}`)
}

func buildsOf(t *testing.T, client *Client) Builds {
	t.Helper()
	builds, err := client.Builds()
	if err != nil || builds == nil {
		t.Fatalf("the host answered builds with %v, %v", builds, err)
	}
	return *builds
}

func snapshotGoesThrough(t *testing.T, ext fakeExtension, client *Client) {
	t.Helper()
	read := callAsync(client, 7, "snapshot", nil)
	if heard := ext.next(); heard.T != messageStatus {
		t.Fatalf("before the snapshot the extension heard %+v; want the status reading and no reload", heard)
	}
	ext.answer(ext.call().ID, `"ok":true,"value":{}`)
	if a := <-read; a.err != nil {
		t.Fatalf("the snapshot returned %v", a.err)
	}
}

func TestHostRewritesAnOldBuildReloadsItAndAcceptsTheReconnect(t *testing.T) {
	home := shortHome(t)
	installFor(t, home, testOrigin)
	stale := staleInstall(t, home)
	tofu, err := Build()
	if err != nil || tofu == "" {
		t.Fatalf("Build is %q, %v", tofu, err)
	}

	ext, done := startHost(t, home)
	ext.send(`{"t":"hello","version":2,"build":"0ld","tabs":[{"id":7,"url":"https://a.test/","title":"A"}]}`)
	if heard := ext.next(); heard.T != messageReload {
		t.Fatalf("an extension on build 0ld heard %+v; want reload", heard)
	}
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the install directory was not rewritten before the reload: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(stale), "manifest.json"))
	var manifest struct {
		Key         string `json:"key"`
		Version     string `json:"version"`
		VersionName string `json:"version_name"`
	}
	if err == nil {
		err = json.Unmarshal(raw, &manifest)
	}
	if err != nil || manifest.VersionName != konst.Version+" · "+tofu || manifest.Version != chromeVersion(konst.Version) || manifest.Key == "" {
		t.Fatalf("the rewritten manifest is %s, %v; want version %s and version_name %s · %s beside the key", raw, err, chromeVersion(konst.Version), konst.Version, tofu)
	}
	if _, err := dial(t, home).Call(7, "snapshot", nil); err == nil || !strings.Contains(err.Error(), "reloading") {
		t.Fatalf("a snapshot on the stale extension returned %v; want it refused while it reloads", err)
	}
	_ = ext.toHost.Close()
	if err := <-done; err != nil {
		t.Fatalf("the stale host ended with %v when the extension reloaded", err)
	}

	for _, updatedFrom := range []string{"0ld", ""} {
		ext, done = startHost(t, home)
		helloFrom(ext, tofu)
		client := dial(t, home)
		if got, want := buildsOf(t, client), (Builds{Extension: tofu, Tofu: tofu, UpdatedFrom: updatedFrom}); got != want {
			t.Fatalf("the reconnected host reports %+v; want %+v", got, want)
		}
		snapshotGoesThrough(t, ext, client)
		_ = ext.toHost.Close()
		<-done
	}
}

func TestChromeVersionIsUpToFourDottedIntegers(t *testing.T) {
	for tofu, chrome := range map[string]string{
		"0.5.0-rc-fix17":    "0.5.0.17",
		"0.5.0":             "0.5.0",
		"0.4.9-fix1":        "0.4.9.1",
		"0.5.0-rc-fix07":    "0.5.0.7",
		"0.4.5+dev":         "0.4.5",
		"0.5.0-beta":        "0.5.0",
		"0.5.0-rc-fixes":    "0.5.0",
		"0.5.0-rc-fix":      "0.5.0",
		"0.5.0-rc-fix-3":    "0.5.0",
		"0.5.0-rc-fix70000": "0.5.0",
		"0.5.0-rc-fix17+x":  "0.5.0",
	} {
		if got := chromeVersion(tofu); got != chrome {
			t.Errorf("chromeVersion(%q) is %q; want %q", tofu, got, chrome)
		}
	}
}

func TestHostOnTheSameBuildNeitherRewritesNorReloads(t *testing.T) {
	home := shortHome(t)
	installFor(t, home, testOrigin)
	stale := staleInstall(t, home)
	tofu, err := Build()
	if err != nil {
		t.Fatal(err)
	}
	ext, _ := startHost(t, home)
	helloFrom(ext, tofu)
	client := dial(t, home)
	snapshotGoesThrough(t, ext, client)
	if _, err := os.Stat(stale); err != nil {
		t.Fatalf("the install directory was rewritten on the same build: %v", err)
	}
	if got, want := buildsOf(t, client), (Builds{Extension: tofu, Tofu: tofu}); got != want {
		t.Fatalf("the host reports %+v; want %+v", got, want)
	}
}

func TestHostWithAnUnwritableInstallSaysSoAndNeverReloads(t *testing.T) {
	home := shortHome(t)
	installFor(t, home, testOrigin)
	stale := staleInstall(t, home)
	extensionDir := filepath.Dir(stale)
	kept := filepath.Join(extensionDir, "kept.js")
	if err := os.WriteFile(kept, []byte("kept"), 0o644); err != nil {
		t.Fatal(err)
	}
	held, err := os.Open(stale)
	if err != nil {
		t.Fatal(err)
	}
	ext, done := startHost(t, home)
	browserDir := filepath.Dir(extensionDir)
	if err := os.Chmod(browserDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = held.Close()
		_ = os.Chmod(browserDir, 0o700)
	})
	helloFrom(ext, "0ld")
	client := dial(t, home)
	var refusals []string
	for range 2 {
		_, err := client.Call(7, "snapshot", nil)
		if err == nil || !strings.Contains(err.Error(), extensionDir) || !strings.Contains(err.Error(), "tofu browser install") {
			t.Fatalf("a snapshot on a stale extension tofu could not rewrite returned %v", err)
		}
		refusals = append(refusals, err.Error())
	}
	if builds := buildsOf(t, client); refusals[0] != refusals[1] || builds.Problem != refusals[0] || builds.Extension != "0ld" {
		t.Fatalf("the refusals are %q and the host reports %+v; want one error, the same everywhere", refusals, builds)
	}
	for path, want := range map[string]string{stale: "old", kept: "kept"} {
		if got, err := os.ReadFile(path); err != nil || string(got) != want {
			t.Errorf("after a failed rewrite %s reads %q, %v; want the old %q", path, got, err, want)
		}
	}
	if entries, err := os.ReadDir(extensionDir); err != nil || len(entries) != 2 {
		t.Errorf("after a failed rewrite the extension directory holds %v, %v; want only the two old files", entries, err)
	}
	if left, _ := filepath.Glob(extensionDir + "?*"); len(left) != 0 {
		t.Errorf("a failed rewrite left %v beside the extension directory", left)
	}
	select {
	case heard := <-ext.heard:
		t.Fatalf("the host wrote %+v to an extension it could not update", heard)
	case err := <-done:
		t.Fatalf("the host exited with %v, so Chrome would relaunch it in a loop", err)
	default:
	}
}

func TestTheScreencastAndEmulationMethodsPassAsReadingAndOthersStillStop(t *testing.T) {
	for _, method := range []string{"Page.startScreencast", "Page.stopScreencast", "Page.screencastFrameAck", "Emulation.setDeviceMetricsOverride", "Emulation.clearDeviceMetricsOverride", "Emulation.setEmulatedMedia"} {
		if now, err := cdpStatus(json.RawMessage(`{"calls":[{"method":"` + method + `"}]}`)); err != nil || now != statusReading {
			t.Errorf("%s is %q, %v; want reading", method, now, err)
		}
	}
	for _, method := range []string{"Page.navigate", "Emulation.setUserAgentOverride", "Page.captureScreenshot"} {
		if now, err := cdpStatus(json.RawMessage(`{"calls":[{"method":"` + method + `"}]}`)); err == nil {
			t.Errorf("%s passed as %q", method, now)
		}
	}
}

func TestThreeChunksOfScreencastFramesReachTheClientAsOneOrderedList(t *testing.T) {
	home := shortHome(t)
	installFor(t, home, testOrigin)
	ext, done := startHost(t, home)
	ext.send(`{"t":"hello","version":2,"tabs":[{"id":7,"url":"https://a.test/","title":"A"}]}`)
	client := dial(t, home)

	started := make(chan error, 1)
	go func() { started <- client.StartScreencast(7) }()
	call := ext.call()
	if call.Op != opScreencast || call.TabID != 7 || string(call.Args) != `{"action":"start"}` {
		t.Fatalf("the start reached the extension as %+v", call)
	}
	ext.answer(call.ID, `"ok":true`)
	if err := <-started; err != nil {
		t.Fatalf("the start answered %v", err)
	}

	type stop struct {
		frames []ScreencastFrame
		err    error
	}
	stopped := make(chan stop, 1)
	go func() {
		frames, err := client.StopScreencast(7)
		stopped <- stop{frames, err}
	}()
	call = ext.call()
	if call.Op != opScreencast || string(call.Args) != `{"action":"stop"}` {
		t.Fatalf("the stop reached the extension as %+v", call)
	}
	id := strconv.FormatInt(call.ID, 10)
	ext.send(`{"t":"frames","id":` + id + `,"frames":[{"data":"AAE=","timestamp":1000.001},{"data":"AgM=","timestamp":1000.017}]}`)
	ext.send(`{"t":"frames","id":999,"frames":[{"data":"/w==","timestamp":5}]}`)
	ext.send(`{"t":"frames","id":` + id + `,"frames":[{"data":"BA==","timestamp":1000.034}]}`)
	ext.answer(call.ID, `"ok":true,"value":[{"data":"BQY=","timestamp":1000.05}]`)
	var got stop
	select {
	case got = <-stopped:
	case err := <-done:
		t.Fatalf("the host ended on a frames message: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("the stop never answered")
	}
	want := []ScreencastFrame{{[]byte{0, 1}, 1000.001}, {[]byte{2, 3}, 1000.017}, {[]byte{4}, 1000.034}, {[]byte{5, 6}, 1000.05}}
	if got.err != nil || !slices.EqualFunc(got.frames, want, func(a, b ScreencastFrame) bool {
		return string(a.Data) == string(b.Data) && a.ChromeSeconds == b.ChromeSeconds
	}) {
		t.Fatalf("the stop returned %v, %v; want %v", got.frames, got.err, want)
	}
}

func TestATimedOutCallDropsTheConnectionSoTheCallerRedials(t *testing.T) {
	home := shortHome(t)
	installFor(t, home, testOrigin)
	ext, _ := startHost(t, home)
	helloFrom(ext, "")
	client := dial(t, home)
	_, err := client.callBy(time.Now().Add(200*time.Millisecond), 7, "snapshot", nil)
	saysItTimedOut(t, err)
	ext.call()
	started := time.Now()
	if _, err := client.Call(7, "snapshot", nil); !errors.Is(err, ErrNotConnected) || time.Since(started) > time.Second {
		t.Fatalf("the next call on the timed-out client returned %v after %v; want ErrNotConnected at once", err, time.Since(started))
	}
	select {
	case heard := <-ext.heard:
		if heard.T == messageCall {
			t.Fatalf("a timed-out client still sent %+v to the extension", heard)
		}
	case <-time.After(100 * time.Millisecond):
	}
	if tabs, err := dial(t, home).Tabs(); err != nil || len(tabs) != 1 {
		t.Fatalf("the redial serves %v, %v", tabs, err)
	}
}

func saysItTimedOut(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, ErrNotConnected) || !errors.Is(err, os.ErrDeadlineExceeded) || strings.Contains(err.Error(), InstallHint) || !strings.Contains(err.Error(), "the next call reconnects") {
		t.Fatalf("a call past its deadline returned %v; want it to match ErrNotConnected and the deadline, say the next call reconnects, and never say %q", err, InstallHint)
	}
}

func TestADriverTimeoutMatchesNotConnectedWithoutTheInstallHint(t *testing.T) {
	for name, overdue := range map[string]func(*Driver) error{
		"Do":      func(d *Driver) error { _, err := d.Do(Move{Kind: MoveScroll, Value: "down"}); return err },
		"Observe": func(d *Driver) error { _, err := d.Observe(true); return err },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			home := shortHome(t)
			installFor(t, home, testOrigin)
			ext, _ := startHost(t, home)
			helloFrom(ext, "")
			saysItTimedOut(t, overdue(&Driver{Client: dial(t, home), Tab: 7}))
		})
	}
}

func TestAClientThatStopsReadingNeverHoldsTheRelay(t *testing.T) {
	home := shortHome(t)
	installFor(t, home, testOrigin)
	ext, _ := startHost(t, home)
	helloFrom(ext, "")
	stuck, err := net.Dial("unix", socketOf(t, home))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stuck.Close() })
	asks := json.NewEncoder(stuck)
	var ids []int64
	for id := range int64(4) {
		if err := asks.Encode(request{ID: id + 1, Op: "snapshot", Tab: 7}); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, ext.call().ID)
	}
	large := strings.Repeat("x", konst.BrowserHostMessageBytes-100)
	answered := make(chan struct{})
	go func() {
		defer close(answered)
		for _, id := range ids {
			_ = WriteMessage(ext.toHost, []byte(`{"t":"result","id":`+strconv.FormatInt(id, 10)+`,"ok":true,"value":"`+large+`"}`))
		}
	}()
	time.Sleep(200 * time.Millisecond)

	served := make(chan error, 1)
	started := time.Now()
	go func() {
		client, err := Dial(home)
		if err == nil {
			_, err = client.Tabs()
			_ = client.Close()
		}
		served <- err
	}()
	select {
	case err := <-served:
		if err != nil || time.Since(started) > 2*time.Second {
			t.Fatalf("a second client's hello and tabs returned %v after %v", err, time.Since(started))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a second client's hello and tabs waited over 2 s behind a client that stopped reading")
	}
	select {
	case <-answered:
	case <-time.After(3 * konst.BrowserDialTimeoutMillis * time.Millisecond):
		t.Fatal("the relay stopped reading the extension while it wrote to a client that stopped reading")
	}
}
