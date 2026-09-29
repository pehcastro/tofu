package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"tofu/interface/tui"
	"tofu/internal/konst"
	"tofu/internal/llm/models"
)

const servedID = "claude-sub/claude-sonnet-5-5"

func catalogHome(t *testing.T) string {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	catalog, err := models.CatalogDir()
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func stubReload(t *testing.T, catalog string, exit int, calls *atomic.Int32, release <-chan struct{}) func(context.Context, io.Writer) int {
	return func(context.Context, io.Writer) int {
		calls.Add(1)
		<-release
		path := filepath.Join(catalog, "models", "anthropic", "claude-sonnet-5-5.yaml")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Error(err)
		}
		if err := os.WriteFile(path, []byte("subscription: claude-sub\nuse: allowed\n"), 0o644); err != nil {
			t.Error(err)
		}
		return exit
	}
}

func deliver(cmd tea.Cmd, into chan<- tea.Msg) {
	if cmd == nil {
		return
	}
	go func() {
		msg := cmd()
		if batch, isBatch := msg.(tea.BatchMsg); isBatch {
			for _, one := range batch {
				deliver(one, into)
			}
			return
		}
		into <- msg
	}()
}

func TestAStaleCatalogReloadsOnceOffTheMainPathAndAFreshOneRunsNothing(t *testing.T) {
	catalog := catalogHome(t)
	dir := t.TempDir()
	now := time.Now()
	if !catalogStale(now) {
		t.Fatal("a home with no catalog reads as fresh")
	}
	var calls atomic.Int32
	release := make(chan struct{})
	app := tui.New(tui.Options{Root: dir, ReloadModels: modelsReload(dir, stubReload(t, catalog, exitOK, &calls, release)), ModelsStale: catalogStale(now)})
	began := time.Now()
	cmd := app.Init()
	if took := time.Since(began); took > 100*time.Millisecond {
		t.Fatalf("Init waited %v for the reload", took)
	}
	into := make(chan tea.Msg, 64)
	deliver(cmd, into)
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	time.Sleep(200 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("a stale start ran the reload %d times before it finished, want 1", got)
	}
	close(release)
	deadline := time.After(3 * time.Second)
	for noted := false; !noted; {
		select {
		case msg := <-into:
			app.Update(msg)
			noted = strings.Contains(app.View().Content, servedID)
		case <-deadline:
			t.Fatalf("the footer never named %s:\n%s", servedID, app.View().Content)
		}
	}
	if catalogStale(time.Now()) {
		t.Fatal("a successful reload left the catalog stale")
	}
	if !catalogStale(time.Now().Add(konst.CatalogStaleHours * time.Hour)) {
		t.Fatalf("the catalog is still fresh after %d hours", konst.CatalogStaleHours)
	}
	var again atomic.Int32
	fresh := tui.New(tui.Options{Root: dir, ReloadModels: modelsReload(dir, stubReload(t, catalog, exitOK, &again, release)), ModelsStale: catalogStale(time.Now())})
	deliver(fresh.Init(), make(chan tea.Msg, 64))
	time.Sleep(200 * time.Millisecond)
	if got := again.Load(); got != 0 {
		t.Fatalf("a fresh catalog ran the reload %d times", got)
	}
}

func TestAFailedReloadRetriesAfterAnHourAndASuccessAfterItStampsFresh(t *testing.T) {
	catalog := catalogHome(t)
	dir := t.TempDir()
	released := make(chan struct{})
	close(released)
	var calls atomic.Int32
	modelsReload(dir, stubReload(t, catalog, exitVerdict, &calls, released))()
	now := time.Now()
	retry := konst.CatalogRetryHours * time.Hour
	if catalogStale(now) || catalogStale(now.Add(retry-time.Minute)) {
		t.Fatal("a failed reload is retried before the retry time")
	}
	if !catalogStale(now.Add(retry)) {
		t.Fatalf("a failed reload is not retried after %v, though it wrote %s", retry, catalog)
	}
	if note := modelsReload(dir, stubReload(t, catalog, exitOK, &calls, released))(); note != "" {
		t.Fatalf("a model already in the catalog was noted as new: %q", note)
	}
	if catalogStale(time.Now().Add(retry + time.Minute)) {
		t.Fatal("a success after a failure still waits the retry time instead of the stale time")
	}
	if err := os.WriteFile(catalog+reloadStampSuffix, []byte("unknown"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !catalogStale(time.Now()) {
		t.Fatal("a stamp with unknown content reads as fresh")
	}
}
