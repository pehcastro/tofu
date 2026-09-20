package recall_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/recall"
)

func walkStored(dir string, fn func([]byte)) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return err
		}
		fn(data)
	}
	return nil
}

func testConfig() recall.Config {
	return recall.Config{ElideAboveBytes: 64, HeadBytes: 16, TailBytes: 16}
}

func TestUnderThresholdPassesThroughByIdentity(t *testing.T) {
	store := recall.NewStore(t.TempDir())
	body := []byte("small result")

	got, err := recall.Elide(store, testConfig(), body, true)
	if err != nil {
		t.Fatalf("elide: %v", err)
	}
	if got.Reference != nil {
		t.Fatalf("a result under the threshold got a reference: %+v", got.Reference)
	}
	if &got.Body[0] != &body[0] {
		t.Fatal("a result under the threshold was copied, not passed through by identity")
	}
}

func TestOverThresholdIsStoredWholeAndReturnsAReference(t *testing.T) {
	store := recall.NewStore(t.TempDir())
	body := []byte(strings.Repeat("x", 40) + "MIDDLE" + strings.Repeat("y", 40))

	got, err := recall.Elide(store, testConfig(), body, true)
	if err != nil {
		t.Fatalf("elide: %v", err)
	}
	if got.Reference == nil {
		t.Fatal("a result over the threshold got no reference")
	}
	ref := got.Reference
	if ref.ID == "" {
		t.Fatal("reference carries no id")
	}
	if ref.Bytes != len(body) {
		t.Fatalf("reference byte count = %d, want %d", ref.Bytes, len(body))
	}
	if ref.Head != string(body[:16]) {
		t.Fatalf("reference head = %q, want %q", ref.Head, string(body[:16]))
	}
	if ref.Tail != string(body[len(body)-16:]) {
		t.Fatalf("reference tail = %q, want %q", ref.Tail, string(body[len(body)-16:]))
	}
	t.Logf("head=%q tail=%q", ref.Head, ref.Tail)
}

func TestFetchByReferenceIDReturnsTheOriginalBytes(t *testing.T) {
	store := recall.NewStore(t.TempDir())
	body := []byte(strings.Repeat("z", 200))

	got, err := recall.Elide(store, testConfig(), body, true)
	if err != nil {
		t.Fatalf("elide: %v", err)
	}
	back, err := store.Fetch(got.Reference.ID)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if string(back) != string(body) {
		t.Fatalf("fetched bytes differ from the original: got %d bytes, want %d", len(back), len(body))
	}
}

func TestArmOffPassesThroughWholeButTheStoreStillHoldsIt(t *testing.T) {
	store := recall.NewStore(t.TempDir())
	body := []byte(strings.Repeat("q", 200))

	got, err := recall.Elide(store, testConfig(), body, false)
	if err != nil {
		t.Fatalf("elide: %v", err)
	}
	if got.Reference != nil {
		t.Fatalf("the arm was off but a reference was returned: %+v", got.Reference)
	}
	if string(got.Body) != string(body) {
		t.Fatal("the arm was off but the body was not passed through whole")
	}

	found := false
	err = walkStored(store.Dir(), func(stored []byte) {
		if string(stored) == string(body) {
			found = true
		}
	})
	if err != nil {
		t.Fatalf("walk stored: %v", err)
	}
	if !found {
		t.Fatal("the arm was off and the store does not hold the result")
	}
}

func TestNothingIsLostAfterElision(t *testing.T) {
	store := recall.NewStore(t.TempDir())
	cfg := recall.Config{ElideAboveBytes: 200, HeadBytes: 32, TailBytes: 32}

	corpus := []struct {
		name   string
		target string
	}{
		{"stack_trace", "goroutine 1 [running]:\nmain.crash()\n\t/app/main.go:42 +0x1a"},
		{"test_failure", "--- FAIL: TestWidget (0.00s)\n    widget_test.go:88: got 3, want 4"},
		{"request_id", "req_8f3a1c9e0b2d4467"},
	}

	for _, item := range corpus {
		body := padMiddle(item.target, 500)
		got, err := recall.Elide(store, cfg, []byte(body), true)
		if err != nil {
			t.Fatalf("%s: elide: %v", item.name, err)
		}
		if got.Reference == nil {
			t.Fatalf("%s: the padded body did not cross the threshold", item.name)
		}
		back, err := store.Fetch(got.Reference.ID)
		if err != nil {
			t.Fatalf("%s: fetch: %v", item.name, err)
		}
		if !strings.Contains(string(back), item.target) {
			t.Fatalf("%s: %q did not survive elision", item.name, item.target)
		}
	}
}

func padMiddle(target string, total int) string {
	pad := (total - len(target)) / 2
	return strings.Repeat("a", pad) + target + strings.Repeat("b", pad)
}
