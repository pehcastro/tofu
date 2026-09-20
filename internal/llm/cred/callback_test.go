package cred

import (
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

const testCallbackPath = "/callback"

func hitCallback(t *testing.T, host string, port int, query url.Values) (int, string) {
	t.Helper()
	target := "http://" + net.JoinHostPort(host, strconv.Itoa(port)) + testCallbackPath + "?" + query.Encode()
	response, err := (&http.Client{Timeout: 5 * time.Second}).Get(target)
	if err != nil {
		t.Fatalf("get %s: %v", target, err)
	}
	defer func() { _ = response.Body.Close() }()
	page, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return response.StatusCode, string(page)
}

func TestCallbackBindsBothLoopbackAddressesWhenBothExist(t *testing.T) {
	if !ipv6LoopbackAvailable() {
		t.Skip("this host has no IPv6 loopback, the dual bind cannot be observed here")
	}
	server, err := startCallback(testCallbackPath, "state-1", 0, true, true)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer server.close()

	if len(server.addresses) != 2 {
		t.Fatalf("addresses = %v, want one per loopback family", server.addresses)
	}
	for _, host := range []string{ipv4Loopback, ipv6Loopback} {
		hitCallback(t, host, server.port, url.Values{"code": {"code-" + host}, "state": {"state-1"}})
		select {
		case result := <-server.results:
			if result.err != nil || result.code != "code-"+host {
				t.Errorf("callback on %s: code=%q err=%v", host, result.code, result.err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("no callback arrived on %s", host)
		}
	}
}

func TestCallbackServesAloneWhenIPv6IsAbsent(t *testing.T) {
	server, err := startCallback(testCallbackPath, "state-1", 0, true, false)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer server.close()

	if len(server.addresses) != 1 {
		t.Fatalf("addresses = %v, want only the IPv4 loopback", server.addresses)
	}
	hitCallback(t, ipv4Loopback, server.port, url.Values{"code": {"code-1"}, "state": {"state-1"}})
	select {
	case result := <-server.results:
		if result.err != nil || result.code != "code-1" {
			t.Errorf("callback: code=%q err=%v", result.code, result.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no callback arrived")
	}
}

func TestCallbackRefusesAMismatchedState(t *testing.T) {
	server, err := startCallback(testCallbackPath, "state-1", 0, true, false)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer server.close()

	status, page := hitCallback(t, ipv4Loopback, server.port, url.Values{"code": {"code-1"}, "state": {"state-2"}})
	select {
	case result := <-server.results:
		if result.err == nil {
			t.Error("a callback carrying another state was accepted")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no callback arrived")
	}
	if status != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", status, http.StatusBadRequest)
	}
	if !strings.Contains(page, string(reasonStateMismatch)) {
		t.Errorf("the failure page does not name the reason, it reads:\n%s", page)
	}
	if !strings.Contains(page, "Sign-in failed") {
		t.Errorf("the failure page does not say the sign-in failed, it reads:\n%s", page)
	}
}

func TestCallbackPagesFetchNothing(t *testing.T) {
	for name, page := range servedPages(t) {
		for _, forbidden := range []string{"http:", "https:", "//"} {
			if strings.Contains(page, forbidden) {
				t.Errorf("the %s page contains %q, so a browser could reach the network:\n%s", name, forbidden, page)
			}
		}
	}
}

func TestCallbackPagesRepeatNothingFromTheRequest(t *testing.T) {
	for name, page := range servedPages(t) {
		for _, secret := range []string{knownCode, knownState, knownOtherState, knownDetail, knownError} {
			if strings.Contains(page, secret) {
				t.Errorf("the %s page repeats %q from the request:\n%s", name, secret, page)
			}
		}
	}
}

func TestCallbackPagesCarryADarkModeRule(t *testing.T) {
	for name, page := range servedPages(t) {
		if !strings.Contains(page, "prefers-color-scheme: dark") {
			t.Errorf("the %s page has no dark mode rule:\n%s", name, page)
		}
		if !strings.Contains(page, "color-scheme: light dark") {
			t.Errorf("the %s page does not declare a color scheme:\n%s", name, page)
		}
	}
}

const (
	knownCode       = "code-a1b2c3d4"
	knownState      = "state-e5f6g7h8"
	knownOtherState = "state-i9j0k1l2"
	knownDetail     = "detail-m3n4o5p6"
	knownError      = "error-q7r8s9t0"
)

func servedPages(t *testing.T) map[string]string {
	t.Helper()
	server, err := startCallback(testCallbackPath, knownState, 0, true, false)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer server.close()

	_, success := hitCallback(t, ipv4Loopback, server.port, url.Values{
		"code": {knownCode}, "state": {knownState},
	})
	<-server.results
	_, failure := hitCallback(t, ipv4Loopback, server.port, url.Values{
		"code": {knownCode}, "state": {knownOtherState},
		"error": {knownError}, "error_description": {knownDetail},
	})
	<-server.results
	return map[string]string{"success": success, "failure": failure}
}

func TestCallbackWithoutFallbackFailsOnABusyPort(t *testing.T) {
	held, err := startCallback(testCallbackPath, "", 0, true, false)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer held.close()

	if _, err := startCallback(testCallbackPath, "", held.port, false, false); err == nil {
		t.Error("a second server bound a port already held, want a failure")
	}
}
