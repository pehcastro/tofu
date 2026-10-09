package host

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tofu/internal/memory"
	"tofu/internal/turn/tools"
)

func TestTheDeskReadsEveryScopeOfMemoryAndItsTree(t *testing.T) {
	c, _, _ := serving(t, nil, ServeConfig{})
	c.ask("1", "initialize", `{"client":"desk"}`)
	c.until(func(line wireLine) bool { return string(line.ID) == `"1"` }, "initialize")
	refused := func(id, method, params, says string) {
		t.Helper()
		c.ask(id, method, params)
		line := c.until(func(line wireLine) bool { return string(line.ID) == `"`+id+`"` }, "the answer to "+id)
		if line.Error == nil || !strings.Contains(line.Error.Message, says) {
			t.Errorf("%s %s answered %s %v, want a refusal saying %q", method, params, line.Result, line.Error, says)
		}
	}

	refused("2", "memory.add", `{"text":"no scope given"}`, "scope")
	c.ask("3", "memory.add", `{"text":"tickets before code | always","scope":"user-local"}`)
	var added memory.Entry
	c.answer("3", &added)
	if added.Scope != memory.UserLocal || added.Kind != memory.KindPerson {
		t.Errorf("memory.add on user-local kept %+v, want a person entry in user-local", added)
	}

	c.ask("4", "memory.view", `{"scope":"user-local"}`)
	var view MemoryView
	c.answer("4", &view)
	if view.Store != string(memory.UserLocal) || len(view.Lines) != 1 || view.Lines[0] != (MemoryLine{ID: 0, N: 1, Text: "person: [memory#" + added.ID + "] tickets before code | always"}) {
		t.Errorf("memory.view of user-local answered %+v, want the one entry as line 0+1 with its text whole", view)
	}

	home := os.Getenv("HOME")
	c.ask("5", "memory.view", `{"scope":"global"}`)
	c.answer("5", &view)
	if view.Store != string(memory.Global) || view.Lines == nil || len(view.Lines) != 0 {
		t.Errorf("memory.view of global answered %+v, want user-global with no lines", view)
	}
	c.ask("6", "memory.view", `{"scope":"episodes"}`)
	c.answer("6", &view)
	if view.Store != memory.EpisodesStore || len(view.Lines) != 0 {
		t.Errorf("memory.view of episodes answered %+v, want the episodes store with no lines", view)
	}
	_ = filepath.WalkDir(home, func(path string, entry fs.DirEntry, _ error) error {
		empty := strings.HasPrefix(path, filepath.Join(home, ".tofu", "memory")) || strings.Contains(path, memory.EpisodesStore)
		if empty && strings.HasSuffix(path, ".jsonl") {
			t.Errorf("a view wrote %s", path)
		}
		return nil
	})

	c.ask("7", "memory.zoom", `{"store":"user-local","id":0,"n":1}`)
	var zoomed MemoryView
	c.answer("7", &zoomed)
	if len(zoomed.Lines) != 1 || !strings.Contains(zoomed.Lines[0].Text, "tickets before code | always") {
		t.Errorf("memory.zoom 0+1 answered %+v, want the raw entry", zoomed)
	}
	refused("8", "memory.zoom", `{"store":"user-local","id":0,"n":3}`, "power of two")
	refused("9", "memory.zoom", `{"store":"project-local","id":0,"n":1}`, "holds nothing")
	refused("10", "memory.zoom", `{"store":"diary","id":0,"n":1}`, "diary")

	c.ask("11", "memory.recall", `{"store":"user-local","regex":"tickets"}`)
	var found MemoryView
	c.answer("11", &found)
	if len(found.Lines) != 1 || found.Lines[0].N != 1 {
		t.Errorf("memory.recall answered %+v, want the one entry", found)
	}
	refused("12", "memory.recall", `{"store":"user-local","regex":"("}`, "regex")

	c.ask("13", "memory.remove", `{"scope":"user-local","id":"`+added.ID+`"}`)
	var removed memory.Entry
	c.answer("13", &removed)
	if removed.ID != added.ID {
		t.Errorf("memory.remove on user-local answered %+v", removed)
	}
}

func TestARememberPickSendsMemoryScoped(t *testing.T) {
	book := &replies[memory.Scope]{}
	offer := tools.MemoryOffer{Statement: "tabs, not spaces", Said: "I prefer tabs", Scope: memory.UserLocal, Scopes: []memory.Scope{memory.Global, memory.UserLocal}}
	for _, c := range []struct {
		name   string
		answer *memory.Scope
		picked string
	}{
		{"a scope", new(memory.Scope), string(memory.Global)},
		{"no", new(memory.Scope), MemoryNone},
		{"cancelled", nil, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			if c.answer != nil && c.picked != MemoryNone {
				*c.answer = memory.Scope(c.picked)
			}
			var scoped []Event
			asked := make(chan string, 1)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan struct{})
			go func() {
				defer close(done)
				_, _ = pickMemory(func(event Event) {
					switch event.Kind {
					case EventAwaitPerson:
						asked <- event.ID
					case EventMemoryScoped:
						scoped = append(scoped, event)
					}
				}, book)(ctx, offer)
			}()
			id := <-asked
			if c.answer == nil {
				cancel()
			} else {
				book.answer(id, *c.answer)
			}
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("the pick never returned")
			}
			if c.answer == nil {
				if len(scoped) != 0 {
					t.Errorf("a cancelled ask sent %+v", scoped)
				}
				return
			}
			if len(scoped) != 1 || scoped[0].Scoped == nil || scoped[0].Scoped.Offered != memory.UserLocal || scoped[0].Scoped.Picked != c.picked {
				t.Fatalf("the pick sent %+v, want one memory.scoped offering user-local and picking %s", scoped, c.picked)
			}
			feed := newItems("s")
			lines := feed.translate(scoped[0], time.Now())
			if len(lines) != 1 || lines[0].msg.Method != "memory.scoped" {
				t.Errorf("the scoped event went out as %+v, want one memory.scoped", lines)
			}
		})
	}
}
