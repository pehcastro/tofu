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
	calls  *io.PipeReader
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

func (f fakeExtension) call() extensionCall {
	f.t.Helper()
	raw, err := ReadMessage(f.calls)
	if err != nil {
		f.t.Fatalf("no call reached the extension: %v", err)
	}
	var call extensionCall
	if err := json.Unmarshal(raw, &call); err != nil || call.T != messageCall {
		f.t.Fatalf("the extension read %s, %v", raw, err)
	}
	return call
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
	t.Cleanup(func() {
		_ = stdinW.Close()
		_ = stdoutR.Close()
	})
	return fakeExtension{t, stdinW, stdoutR}, done
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
	ext.send(`{"t":"hello","version":1,"tabs":[{"id":7,"url":"https://a.test/","title":"A","mode":"drive"}]}`)
	ext.send(`{"t":"shared","tab":{"id":9,"url":"https://b.test/","title":"B"},"mode":"read"}`)
	ext.send(`{"t":"tabUpdated","tab":{"id":9,"url":"https://b.test/next","title":"B next"}}`)
	ext.send(`{"t":"result","id":999,"ok":true,"value":"nobody asked"}`)

	first, second := dial(t, home), dial(t, home)
	tabs, err := first.Tabs()
	want := []Tab{{7, "https://a.test/", "A", ModeDrive, false}, {9, "https://b.test/next", "B next", ModeRead, false}}
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
	if _, err := first.Call(9, "fill", nil); err == nil || !strings.Contains(err.Error(), "reading") {
		t.Fatalf("a drive op on a tab shared for reading returned %v", err)
	}
	if _, err := first.Call(4, "snapshot", nil); err == nil || !strings.Contains(err.Error(), "not shared") {
		t.Fatalf("an op on a tab nobody shared returned %v", err)
	}
	if _, err := first.Call(7, "navigate", nil); err == nil || !strings.Contains(err.Error(), "unknown browser op") {
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
	reached := make(chan extensionCall, 1)
	go func() {
		var call extensionCall
		if raw, err := ReadMessage(ext.calls); err == nil {
			_ = json.Unmarshal(raw, &call)
		}
		reached <- call
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

	ext.send(`{"t":"unshared","tabId":9}`)
	ext.send(`{"t":"result","id":998,"ok":true}`)
	if tabs, err := second.Tabs(); err != nil || !slices.Equal(tabs, want[:1]) {
		t.Fatalf("after unshared, Tabs is %v, %v", tabs, err)
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
	ext.send(`{"t":"hello","version":1,"tabs":[{"id":7,"url":"https://mine.test/","title":"Mine","mode":"drive"},{"id":5,"url":"https://kept.test/","title":"Kept","mode":"drive","opened":true}]}`)
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
	ext.send(`{"t":"shared","tab":{"id":12,"url":"","title":"","opened":true},"mode":"drive"}`)
	ext.answer(create.ID, `"ok":true,"value":12`)
	if a := <-opened; a.err != nil || string(a.value) != "12" {
		t.Fatalf("open returned tab %s, %v", a.value, a.err)
	}
	tabs, err := client.Tabs()
	if err != nil || len(tabs) != 3 || tabs[2] != (Tab{12, "", "", ModeDrive, true}) {
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
		"unknown mode": `{"t":"shared","tab":{"id":1},"mode":"write"}`,
		"old protocol": `{"t":"hello","version":0,"tabs":[]}`,
		"no mode":      `{"t":"hello","version":1,"tabs":[{"id":1}]}`,
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
	ext.send(`{"t":"hello","version":1,"tabs":[{"id":3,"url":"https://a.test/","title":"A","mode":"read"}]}`)
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
	ext.send(`{"t":"hello","version":1,"tabs":[]}`)
	if tabs, err := dial(t, home).Tabs(); err != nil || len(tabs) != 0 {
		t.Fatalf("a host over a stale socket serves %v, %v", tabs, err)
	}
}
