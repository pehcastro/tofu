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
	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	done := make(chan error, 1)
	go func() {
		err := Host(testOrigin, stdinR, stdoutW, home)
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
