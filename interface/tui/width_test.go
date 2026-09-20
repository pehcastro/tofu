package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/crew"
	"tofu/interface/tui/frame"
	"tofu/interface/tui/settings"
)

const (
	wideColumns = 80
	wideRows    = 24
	cjkRepo     = "~/仕事/日本語のリポジトリ"
	cjkIntent   = "内部/判断/方針/道具門.go を読んでから、方針を読む"
	cjkResult   = "412 行、11.8 KB、うち 84 行だけを残した"
	cjkProse    = "門は方針より先に読まれます。理由はこれです。"
)

const (
	zeroWidthJoiner = 0x200d
	man             = 0x1f468
	woman           = 0x1f469
	girl            = 0x1f467
)

func emojiBranch() string {
	return "release/" + string([]rune{man, zeroWidthJoiner, woman, zeroWidthJoiner, girl}) + "-家族"
}

func cjkProviders() []settings.Provider {
	return []settings.Provider{
		{Name: "anthropic", State: "oauth  7日ウィンドウの 62%、18:00 に戻る", Source: "資格情報ストア"},
		{Name: "openrouter", Key: openRouterKey, State: "ok", Source: "~/.boji/.env の設定"},
	}
}

func cjkChildren() []crew.Child {
	return []crew.Child{
		{
			Name:  "日本語エージェント",
			Owns:  []string{"内部/判断/**", "内部/地点/**"},
			Doing: "方針/道具門.go を書いている",
			Since: 2*time.Minute + 14*time.Second,
			Steps: 5,
			Total: 7,
			State: crew.Running,
			Calls: []crew.Call{{Tool: "edit", Text: "内部/判断/方針/道具門.go", Result: "+18 -4"}},
		},
		{
			Name:   "go-docs",
			Owns:   []string{"docs/**"},
			Doing:  "done, 12 files read",
			Since:  6*time.Minute + 41*time.Second,
			State:  crew.Done,
			Report: "五つの実装の名前を変えました。地点の呼び出しが一つ、別名で古い名前に届いています。",
		},
	}
}

func wideFrames(t *testing.T) map[string]string {
	t.Helper()
	app := newTestApp(Options{
		Repo:      cjkRepo,
		Branch:    emojiBranch(),
		Now:       fixedClock(),
		Wires:     bothWires,
		Providers: cjkProviders(),
	})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: wideColumns, Height: wideRows})
	app.Update(frame.Quota{Label: "codex 7日", Fraction: 0.62, Reported: true, ResetsAt: fixedClock()().Add(time.Hour)})
	for _, event := range []Event{
		{Kind: EventContext, Context: frame.Context{Used: 118000, Budget: 250000}},
		{Kind: EventStats, Model: "gpt-5.6-sol-2026-09-01", TokensIn: 284000, TokensOut: 61000, Decisions: 3},
		{Kind: EventText, Text: cjkProse},
		{Kind: EventToolCall, ID: "c1", Tool: "read", Text: cjkIntent, Detail: cjkIntent},
		{Kind: EventDecision, Decision: asked()},
		{Kind: EventToolResult, ID: "c1", Text: cjkResult},
	} {
		app.Update(event)
	}
	app.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	frames := map[string]string{"session": app.View().Content}

	app.Update(Event{Kind: EventCrew, Children: cjkChildren()})
	app.Update(tea.KeyPressMsg{Code: '2', Mod: tea.ModAlt})
	app.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	frames["crew"] = app.View().Content

	app.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	frames["activity"] = app.View().Content

	app.Update(tea.KeyPressMsg{Code: '6', Mod: tea.ModAlt})
	frames["settings"] = app.View().Content

	setup := newTestApp(Options{
		Repo:         cjkRepo,
		Branch:       emojiBranch(),
		Now:          fixedClock(),
		Requirements: []Requirement{{What: "契約が無いので、どのモデルも答えられません", Fix: "tofu login anthropic"}},
	})
	setup.Update(tea.WindowSizeMsg{Width: wideColumns, Height: wideRows})
	frames["setup"] = setup.View().Content
	return frames
}

func TestNoRowRunsPastTheFrameWithACJKRepoAndAnEmojiBranch(t *testing.T) {
	for name, content := range wideFrames(t) {
		t.Run(name, func(t *testing.T) {
			if !strings.Contains(ansi.Strip(content), "日") {
				t.Fatalf("the %s view carries no wide text, so it proves nothing:\n%s", name, ansi.Strip(content))
			}
			for row, line := range strings.Split(content, "\n") {
				if cells := ansi.StringWidth(line); cells > wideColumns {
					t.Errorf("row %d is %d cells wide: %q", row, cells, ansi.Strip(line))
				}
			}
		})
	}
}

func TestTheStripPlacesItsZonesInCellsSoAClickLandsOnTheNameItHits(t *testing.T) {
	strip := frame.Strip{Views: []frame.View{{Digit: '1', Name: "session"}, {Digit: '2', Name: "crew"}}}
	plain := ansi.Strip(strip.Render(wideColumns))
	for index, view := range strip.Views {
		label := string(view.Digit) + " " + view.Name
		start := strings.Index(plain, label)
		if start < 0 {
			t.Fatalf("the strip does not draw %q at all:\n%s", label, plain)
		}
		at := ansi.StringWidth(plain[:start])
		hit, found := strip.Hit(at)
		if !found || hit != index {
			t.Errorf("a click at column %d hit view %d (%v), want view %d", at, hit, found, index)
		}
	}
}
