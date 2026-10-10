package session

import (
	"cmp"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestSessionJSONHoldsUnderTwentyWritersAndAReaderInAnotherStore(t *testing.T) {
	state := t.TempDir()
	writing, reading := OpenAt(state), OpenAt(state)
	log, err := writing.Open(Header{})
	if err != nil {
		t.Fatal(err)
	}
	id := log.ID()
	done := make(chan struct{})
	var held sync.Mutex
	failures, first := map[string]int{}, map[string]string{}
	failed := func(kind string, err error) {
		held.Lock()
		defer held.Unlock()
		failures[kind]++
		first[kind] = cmp.Or(first[kind], err.Error())
	}
	var readers, writers sync.WaitGroup
	agents, edits := 20, 10
	for range 2 {
		readers.Go(func() {
			for {
				select {
				case <-done:
					return
				default:
				}
				if _, err := reading.Header(id); err != nil {
					failed("read", err)
				}
				if listing, _ := reading.Listing(); len(listing.Skipped) > 0 {
					failed("listing", listing.Skipped[0].Reason)
				}
			}
		})
	}
	for agent := range agents {
		writers.Go(func() {
			for range edits {
				row := AgentRun{Agent: string(rune('a' + agent))}
				if err := log.Edit(func(header *Header) { header.Agents = append(header.Agents, row) }); err != nil {
					failed("write", err)
				}
			}
		})
	}
	writers.Wait()
	close(done)
	readers.Wait()
	if err := log.Close(); err != nil {
		failed("close", err)
	}
	for kind, count := range failures {
		t.Errorf("%d %s failures, the first: %s", count, kind, first[kind])
	}
	header, err := reading.Header(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(header.Agents) != agents*edits {
		t.Errorf("session.json holds %d agent rows after %d edits", len(header.Agents), agents*edits)
	}
}

func TestARenameThatNeverSucceedsIsTheCallersErrorAndLeavesNoTemporaryFile(t *testing.T) {
	store := OpenAt(t.TempDir())
	log, err := store.Open(Header{})
	if err != nil {
		t.Fatal(err)
	}
	refused := errors.New("held by another process")
	store.rename = func(string, string) error { return refused }
	defer func() {
		store.rename = os.Rename
		_ = log.Close()
	}()
	if err := log.Edit(func(header *Header) { header.Task = "lost?" }); !errors.Is(err, refused) {
		t.Errorf("an edit whose rename always fails answered %v, want the rename's error", err)
	}
	entries, err := os.ReadDir(store.Dir(log.ID()))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".tofu-") {
			t.Errorf("%s was left beside session.json", entry.Name())
		}
	}
}

func TestAReadOfAnAbsentSessionIsNotExistOnTheFirstTry(t *testing.T) {
	store := OpenAt(t.TempDir())
	tries := 0
	store.readFile = func(path string) ([]byte, error) {
		if filepath.Base(path) == headerName {
			tries++
		}
		return os.ReadFile(path)
	}
	if _, err := store.read("absent"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("reading an absent session answered %v, want not exist", err)
	}
	if tries != 1 {
		t.Errorf("the session.json of an absent session was tried %d times, want once", tries)
	}
}
