package shell

import (
	"net"
	"testing"
	"time"
)

const portCheckTestTimeout = 200 * time.Millisecond

func TestPortOpenFindsAListeningPortWithoutAnHTTPRequest(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	port := listener.Addr().(*net.TCPAddr).Port

	started := time.Now()
	open := PortOpen("127.0.0.1", port, portCheckTestTimeout)
	took := time.Since(started)

	if !open {
		t.Fatalf("port %d is listening and PortOpen said it was not", port)
	}
	if took > 50*time.Millisecond {
		t.Fatalf("checking a local port took %v, want well under the %v timeout", took, portCheckTestTimeout)
	}
	t.Logf("port open check took %v", took)
}

func TestPortOpenOnANothingListeningPortReturnsFalseQuickly(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	started := time.Now()
	open := PortOpen("127.0.0.1", port, portCheckTestTimeout)
	took := time.Since(started)

	if open {
		t.Fatalf("port %d has nothing listening and PortOpen said it was open", port)
	}
	if took > portCheckTestTimeout {
		t.Fatalf("a refused connection took %v, want under the %v timeout", took, portCheckTestTimeout)
	}
	t.Logf("port closed check took %v", took)
}
