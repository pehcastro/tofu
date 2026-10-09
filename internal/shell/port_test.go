package shell

import (
	"context"
	"errors"
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

func listenOnIPv6Loopback(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Skipf("this host cannot listen on ::1: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	return listener.Addr().(*net.TCPAddr).Port
}

func testBinaryName() string {
	return strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
}

func TestAListenerOnIPv6LoopbackIsReportedHeldWithItsHolder(t *testing.T) {
	port := listenOnIPv6Loopback(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	addresses := Probe(ctx, port, konst.PortCheckTimeoutMillis*time.Millisecond)
	at := slices.IndexFunc(addresses, func(one Address) bool { return one.Host == "::1" })
	if at < 0 || !addresses[at].Open || addresses[at].PID != os.Getpid() || !strings.Contains(addresses[at].Command, testBinaryName()) {
		t.Fatalf("a listener on [::1]:%d came back %+v, want ::1 open, held by pid %d running %s", port, addresses, os.Getpid(), testBinaryName())
	}
}

func TestADefaultPortIsGuessedOnlyForADevServer(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"scripts":{"dev":"bun --hot src/index.ts","serve":"bun run --hot server.ts","start":"bun src/server.ts","web":"vite","site":"next dev","docs":"astro dev","test":"bun test --watch","unit":"vitest","spec":"jest --watch","types":"tsc --watch","build":"vite build"}}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	for command, want := range map[string]int{
		"bun run dev": 3000, "bun run serve": 3000, "bun run start": 3000, "npm run web": 5173, "vite": 5173, "npx vite": 5173,
		"bun run site": 3000, "next dev": 3000, "bun run docs": 4321, "astro dev": 4321,
		"bun test --watch": 0, "bun run test": 0, "vitest": 0, "bun run unit": 0, "jest --watch": 0, "bun run spec": 0,
		"tsc --watch": 0, "bun run types": 0, "bun run build": 0, "vite build": 0, "bun install": 0,
	} {
		if got := NamedPort(dir, command, nil); got != want {
			t.Errorf("%q guessed port %d, want %d", command, got, want)
		}
	}
}

func TestALocalAddressInOutputNamesThePortWhateverTheColours(t *testing.T) {
	for printed, want := range map[string]int{
		"\x1b[32mhttp://localhost:8765\x1b[0m\n":                       8765,
		"  Local:   http://localhost:\x1b[1m5173\x1b[22m/\n":           5173,
		"listening on 127.0.0.1:4000":                                  4000,
		"bound [::1]:4001 and more":                                    4001,
		"0.0.0.0:4002":                                                 4002,
		"\x1b]8;;http://localhost:3001/\x1b\\open\x1b]8;;\x1b\\":       3001,
		"first http://localhost:3002 then http://localhost:3003":       3002,
		"Network: http://192.168.1.5:3000/ and https://example.com:80": 0,
		"mylocalhost:3000":                                             0,
		"localhost:99999 and localhost:0":                              0,
		"compiled in 12:30":                                            0,
	} {
		if got := PortInOutput(printed); got != want {
			t.Errorf("%q named port %d, want %d", printed, got, want)
		}
	}
}

func TestABackgroundStartOnAHeldPortIsRefusedNamingTheHolder(t *testing.T) {
	port := listenOnIPv6Loopback(t)
	command := "PORT=" + strconv.Itoa(port) + " bun run dev"
	registry := OpenAt(filepath.Join(t.TempDir(), "shells"))
	wait := Wait{Within: 5 * time.Second, Poll: konst.PortCheckTimeoutMillis * time.Millisecond, Port: NamedPort(t.TempDir(), command, nil)}
	_, err := registry.YieldReady(context.Background(), shellCommand(t, t.TempDir(), command), command, "", wait)
	if !errors.Is(err, ErrPortHeld) || !strings.Contains(err.Error(), strconv.Itoa(os.Getpid())) || !strings.Contains(err.Error(), testBinaryName()) {
		t.Fatalf("a start on held port %d came back %v, want ErrPortHeld naming pid %d and %s", port, err, os.Getpid(), testBinaryName())
	}
	t.Log(err)
	if listed, _ := registry.List(); len(listed) != 0 {
		t.Fatalf("the refused start still registered %+v", listed)
	}
}
