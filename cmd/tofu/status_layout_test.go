package main

import (
	"bytes"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/colorprofile"

	"tofu/interface/cli"
	"tofu/internal/golden"
	"tofu/internal/llm/cred"
	"tofu/internal/sys"
)

const (
	fixtureHome      = "/home/sample"
	fixtureMaxCells  = 100
	madeUpGateKey    = "made-up-typesafe-for-the-receipt-7Qx2"
	madeUpVendorBody = `{"error":{"message":"No auth credentials found for sk-or-v1-made-up"}}`
)

func fixtureMoment() time.Time { return time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC) }

func statusFixture() statusData {
	at := fixtureMoment()
	return statusData{
		Subscriptions: []subscriptionStatus{
			{Source: "claude-sub", Accounts: []accountStatus{
				{ID: 1, Account: "ada@example.com", State: stateInUse, Login: "oauth", ReloginBy: time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC), Windows: []windowStatus{
					{ID: "5h", ResetsAt: at.Add(4*time.Hour + 28*time.Minute)},
					{ID: "7d", Used: 0.67, ResetsAt: at.Add(22 * time.Hour)},
					{ID: "7d:opus", Only: []string{"opus-5", "opus-5-5"}},
				}},
				{ID: 2, Account: "lin.marsh@example.org", State: stateStandby, Login: "oauth", ReloginBy: time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC), Windows: []windowStatus{
					{ID: "5h", Used: 0.92, ResetsAt: at.Add(51 * time.Minute)},
				}},
				{ID: 3, Account: "ops@example.net", State: stateRefreshFailed, Login: "oauth"},
			}},
			{Source: "codex-sub", Accounts: []accountStatus{
				{ID: 4, Account: "11111111-2222-3333-4444-555555555555", State: stateSpent, Login: "oauth", Plan: "pro", Windows: []windowStatus{
					{ID: "5h", Used: 1, ResetsAt: at.Add(2 * time.Hour)},
				}},
			}},
		},
		Keys: keyStatuses(func(variable string) string {
			if variable == sys.OpenRouterKeyName {
				return "sk-or-v1-made-up-000000003498"
			}
			return ""
		}),
	}
}

func printedStatus(t *testing.T, page cli.Page) string {
	t.Helper()
	var out bytes.Buffer
	if err := page.Print(&out, statusLines(page, statusFixture(), fixtureMoment())); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestTheStatusGoldensFollowTheAccountsMockAtEightyAndOneTwenty(t *testing.T) {
	for _, columns := range []int{80, 120} {
		page := cli.Page{Profile: colorprofile.NoTTY, Width: min(columns, fixtureMaxCells), Home: fixtureHome}
		golden.Assert(t, "status-"+strconv.Itoa(columns)+".golden", printedStatus(t, page))
	}
}

func TestNoColourStatusWritesNoEscapeAndAColourTerminalDoes(t *testing.T) {
	terminal := []string{"TTY_FORCE=1", "COLORTERM=truecolor", "TERM=xterm-256color"}
	for _, c := range []struct {
		environ []string
		escape  bool
	}{{terminal, true}, {append(terminal, "NO_COLOR=1"), false}} {
		printed := printedStatus(t, cli.Detect(&bytes.Buffer{}, c.environ))
		if got := strings.IndexByte(printed, 0x1b) >= 0; got != c.escape {
			t.Errorf("%v: an ESC byte written %v, want %v", c.environ, got, c.escape)
		}
	}
}

func TestLoginTypeSafeAgainstAStubPrintsOneReceiptLine(t *testing.T) {
	scratchProject(t)
	jevStub(t, http.StatusOK, loginReachableReply, nil)
	var out, errOut bytes.Buffer
	if code := loginVerb([]string{"classifier", typeSafeName}, strings.NewReader(madeUpGateKey+"\n"), &out, &errOut); code != exitOK {
		t.Fatalf("login typesafe exited %d:\n%s", code, errOut.String())
	}
	if want := "✓ TypeSafe key ····7Qx2 checked and stored  ~/.tofu/agent.db\n"; out.String() != want {
		t.Fatalf("login typesafe printed\n%q\nwant\n%q", out.String(), want)
	}
}

func TestLoginTypeSafeRefusedWithA401PrintsOneErrorLineAndNoVendorBody(t *testing.T) {
	scratchProject(t)
	jevStub(t, http.StatusUnauthorized, madeUpVendorBody, nil)
	var out, errOut bytes.Buffer
	if code := loginVerb([]string{"classifier", typeSafeName}, strings.NewReader(madeUpGateKey+"\n"), &out, &errOut); code != exitVerdict {
		t.Fatalf("a refused key exited %d, want %d", code, exitVerdict)
	}
	lines := strings.Split(strings.TrimSuffix(errOut.String(), "\n"), "\n")
	want := []string{"paste the TypeSafe key, then press enter:", "› made…7Qx2  37 chars  prefix not checked", "✗ TypeSafe refused the key (401)", "  → tofu login classifier typesafe"}
	if out.String() != "" || strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("a 401 printed out %q and err\n%s\nwant err\n%s", out.String(), errOut.String(), strings.Join(want, "\n"))
	}
}

func TestDisableAndEnablePrintOneReceiptLineEach(t *testing.T) {
	scratchProject(t)
	savedAccount(t)
	for _, c := range []struct{ flag, want string }{
		{"--disable", "✓ #1 claude-sub ada@example.com set aside  ~/.tofu/agent.db\n"},
		{"--enable", "✓ #1 claude-sub ada@example.com enabled  ~/.tofu/agent.db\n"},
	} {
		var out, errOut bytes.Buffer
		if code := loginVerb([]string{c.flag, "1"}, nil, &out, &errOut); code != exitOK {
			t.Fatalf("login %s 1 exited %d:\n%s", c.flag, code, errOut.String())
		}
		if out.String() != c.want {
			t.Errorf("login %s 1 printed\n%q\nwant\n%q", c.flag, out.String(), c.want)
		}
	}
}

func TestLogoutWithTwoAccountsNamesBothAndRemovesOnlyTheOneNumbered(t *testing.T) {
	scratchProject(t)
	savedAccount(t)
	savedAccountAs(t, "lin.marsh@example.org")
	var out, errOut bytes.Buffer
	if code := logoutVerb([]string{"llm", "claude-sub"}, &out, &errOut); code != exitUsage || !strings.Contains(strings.Join(strings.Fields(errOut.String()), " "), "#1 ada@example.com, #2 lin.marsh@example.org") {
		t.Fatalf("a logout with two accounts and no number exited %d:\n%s", code, errOut.String())
	}
	errOut.Reset()
	if code := logoutVerb([]string{"llm", "codex-sub", "2"}, &out, &errOut); code != exitVerdict {
		t.Fatalf("a logout naming a claude-sub number under codex-sub exited %d:\n%s", code, errOut.String())
	}
	if code := logoutVerb([]string{"llm", "claude-sub", "2"}, &out, &errOut); code != exitOK {
		t.Fatalf("logout llm claude-sub 2 exited %d:\n%s", code, errOut.String())
	}
	if want := "✓ #2 claude-sub lin.marsh@example.org signed out  ~/.tofu/agent.db\n"; out.String() != want {
		t.Fatalf("logout printed %q, want %q", out.String(), want)
	}
	path, _ := cred.Path()
	store, err := cred.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	if rows, err := store.List(); err != nil || len(rows) != 1 || rows[0].Credential.Identity.Email != "ada@example.com" {
		t.Fatalf("after the logout the store holds %+v (%v), want ada alone", rows, err)
	}
}

func savedAccount(t *testing.T) {
	t.Helper()
	savedAccountAs(t, "ada@example.com")
}

func savedAccountAs(t *testing.T, email string) {
	t.Helper()
	path, err := cred.Path()
	if err != nil {
		t.Fatal(err)
	}
	store, err := cred.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	credential := cred.Credential{Provider: cred.ClaudeSub, Kind: "oauth", Access: "made-up-access", Refresh: "made-up-refresh",
		Expires: fixtureMoment().Add(time.Hour), Authorized: fixtureMoment(), Identity: cred.Identity{Email: email}}
	if err := store.Save(credential, fixtureMoment()); err != nil {
		t.Fatal(err)
	}
}
