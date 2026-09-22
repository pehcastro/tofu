package session

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"tofu/internal/sys"
)

func TestAHandleThatClimbsOutOfTheStoreIsRefusedByBothReaders(t *testing.T) {
	base := t.TempDir()
	store := NewStore(filepath.Join(base, "sessions"))
	outside := filepath.Join(base, "elsewhere")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("make the directory outside the store: %v", err)
	}
	bait := []byte(`{"id":"elsewhere","root":"elsewhere","task":"a header no store should reach"}`)
	if err := os.WriteFile(filepath.Join(outside, headerName), bait, 0o600); err != nil {
		t.Fatalf("write the bait header: %v", err)
	}

	refused := func(reader, handle string, err error) {
		t.Helper()
		var escaping EscapingHandleError
		if !errors.As(err, &escaping) {
			t.Fatalf("%s(%s) returned %v, want a typed refusal", reader, handle, err)
		}
		if escaping.Handle != handle {
			t.Fatalf("%s refused %q while it was asked about %q", reader, escaping.Handle, handle)
		}
		if errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("%s(%s) refuses as a missing session, and a caller prints that as nothing being there", reader, handle)
		}
	}

	for _, handle := range []string{"../elsewhere", `..\elsewhere`, "a/b", outside, ".."} {
		header, err := store.Header(handle)
		refused("Header", handle, err)
		matched, err := store.Resolve(handle)
		refused("Resolve", handle, err)
		if header.ID != "" || len(matched) != 0 {
			t.Fatalf("the refusal of %s still returned %+v and %d headers", handle, header, len(matched))
		}
	}
}

func TestARecordedSessionOnDiskStillResolves(t *testing.T) {
	dir := filepath.Join(sys.SourceRoot(), ".tofu", "sessions")
	if _, err := os.Stat(dir); err != nil {
		t.Skip("skip: this checkout has no .tofu/sessions, so there is no recorded session to read")
	}
	store := NewStore(dir)
	listing, err := store.Listing()
	if err != nil {
		t.Fatalf("listing the recorded sessions: %v", err)
	}
	if len(listing.Sessions) == 0 {
		t.Skip("skip: the recorded sessions directory holds no session")
	}
	id := listing.Sessions[0].ID
	matched, err := store.Resolve(id)
	if err != nil {
		t.Fatalf("a recorded session is refused by the new guard: %v", err)
	}
	if len(matched) != 1 || matched[0].ID != id {
		t.Fatalf("resolving %s returned %d headers", id, len(matched))
	}
	header, err := store.Header(id)
	if err != nil || header.ID != id {
		t.Fatalf("Header(%s) = %+v, %v", id, header, err)
	}
}
