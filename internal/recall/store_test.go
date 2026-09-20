package recall_test

import (
	"os"
	"testing"

	"tofu/internal/recall"
)

func TestFetchOfAnUnknownIDFails(t *testing.T) {
	store := recall.NewStore(t.TempDir())
	if _, err := store.Fetch("nope"); err == nil {
		t.Fatal("fetch of an id that was never put succeeded")
	}
}

func TestStoreWritesUnderItsDir(t *testing.T) {
	dir := t.TempDir()
	store := recall.NewStore(dir)

	got, err := recall.Elide(store, recall.Config{ElideAboveBytes: 4, HeadBytes: 2, TailBytes: 2}, []byte("hello world"), true)
	if err != nil {
		t.Fatalf("elide: %v", err)
	}
	if got.Reference == nil {
		t.Fatal("expected a reference")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("store dir has %d entries, want 1", len(entries))
	}
	if entries[0].Name() != got.Reference.ID+".bin" {
		t.Fatalf("stored file %q does not name the reference id %q", entries[0].Name(), got.Reference.ID)
	}
}
