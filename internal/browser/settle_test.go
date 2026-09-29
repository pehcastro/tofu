package browser

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"tofu/internal/konst"
)

func settleIn(t *testing.T, quietAfter string) (settled, wall int) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("no node on the PATH to run the settle script")
	}
	expression := filepath.Join(t.TempDir(), "settle.js")
	if err := os.WriteFile(expression, []byte(settleExpression(konst.BrowserDOMQuietMillis)), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, "extension/testdata/settle.js", expression, quietAfter).Output()
	var measured struct {
		Settled any `json:"settled"`
		Wall    int `json:"wall"`
	}
	if err == nil {
		err = json.Unmarshal(out, &measured)
	}
	if err != nil {
		t.Fatalf("the settle script in a stubbed page: %v\n%s", err, out)
	}
	settledMS, isNumber := measured.Settled.(float64)
	if !isNumber {
		t.Fatalf("the settle script answered %v, not the milliseconds it took", measured.Settled)
	}
	return int(settledMS), measured.Wall
}

func TestAPageQuietAfter200MsSettlesWithin500AndABusyOneAtTheCap(t *testing.T) {
	settled, wall := settleIn(t, "200")
	t.Logf("a page quiet after 200 ms settled in %d ms, %d ms of wall time", settled, wall)
	if settled > 550 || wall > 600 {
		t.Errorf("a page quiet after 200 ms settled in %d ms, %d ms of wall time; want within 500", settled, wall)
	}
	settled, wall = settleIn(t, "never")
	t.Logf("a page that never goes quiet settled in %d ms, %d ms of wall time", settled, wall)
	if settled < konst.BrowserDOMQuietMaxMillis || settled > konst.BrowserDOMQuietMaxMillis+150 {
		t.Errorf("a page that never goes quiet settled in %d ms; want the %d ms cap", settled, konst.BrowserDOMQuietMaxMillis)
	}
}
