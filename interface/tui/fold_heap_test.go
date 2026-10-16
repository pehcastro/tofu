package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/subagent"
	"tofu/internal/host"
	"tofu/internal/konst"
	"tofu/internal/session"
	roster "tofu/internal/subagent"
)

const (
	heapAgents        = 450
	heapCalls         = 200
	heapThoughtChunks = 4
	heapChunk         = 256
	heapSettledFor    = 20 * time.Minute
	heapHostTick      = 250 * time.Millisecond
	heapSearched      = 317
	heapWidth         = 160
	heapHeight        = 45
	heapFlatPercent   = 5
)

func heapName(agent int) string { return fmt.Sprintf("sub-%d", agent) }

func heapCallID(agent, call int) string { return fmt.Sprintf("call-%d-%d", agent, call) }

func heapThought(agent, call int) string {
	return strings.Repeat(fmt.Sprintf("ponders%dx%d weighing the note before reading it ", agent, call), heapThoughtChunks*heapChunk/48+1)[:heapThoughtChunks*heapChunk]
}

func heapPath(agent, call int) string { return fmt.Sprintf("m%03d/file%d.go", agent, call) }

func heapAlloc() uint64 {
	runtime.GC()
	runtime.GC()
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return stats.HeapAlloc
}

func mb(bytes uint64) float64 { return float64(bytes) / (1 << 20) }

func recordedCrowd(t *testing.T, project string, at time.Time) string {
	t.Helper()
	store, err := session.OpenIn(project)
	if err != nil {
		t.Fatalf("OpenIn: %v", err)
	}
	log, err := store.Open(session.Header{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	appended := func(event session.Event, body any) {
		if _, err := log.Append(event, body); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	for agent := 1; agent <= heapAgents; agent++ {
		name := heapName(agent)
		for call := range heapCalls {
			callID, when := heapCallID(agent, call), at.Add(time.Duration(call)*time.Second)
			args, _ := json.Marshal(map[string]string{"path": heapPath(agent, call)})
			appended(session.Event{Agent: name, At: when, Kind: session.EventMessage}, session.MessageBody{Role: session.RoleAssistant, Thinking: heapThought(agent, call)})
			appended(session.Event{Agent: name, At: when, Kind: session.EventToolCall, Call: callID}, session.CallBody{Tool: "read", Args: args})
			appended(session.Event{Agent: name, At: when.Add(time.Second), Kind: session.EventToolResult, Call: callID}, session.ResultBody{Content: "84 lines, 2.1 KB", ResultBytes: 2148})
		}
	}
	if err := log.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return log.ID()
}

func crowdApp(t *testing.T, at *time.Time) (*App, []subagent.Row) {
	t.Helper()
	home, project := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	id := recordedCrowd(t, project, *at)
	now := func() time.Time { return *at }
	playing, troubles := host.New(host.Config{Dir: project, Now: now, Resumed: host.Carry{Session: id}, Play: func(context.Context, Pick, string, host.Live) {}})
	if len(troubles) > 0 {
		t.Fatalf("the host did not open the recorded session: %v", troubles)
	}
	t.Cleanup(playing.Close)
	app := newTestApp(Options{Repo: testRepo, Now: now, Wires: anthropicAlone, Host: playing})
	app.Update(tea.WindowSizeMsg{Width: heapWidth, Height: heapHeight})
	rows := make([]subagent.Row, heapAgents)
	for agent := 1; agent <= heapAgents; agent++ {
		name, row := heapName(agent), &rows[agent-1]
		*row = subagent.Row{Name: name, Agent: "go-dev", Model: "claude-sub/claude-opus-5", Owns: []string{fmt.Sprintf("m%03d/**", agent)},
			Doing: fmt.Sprintf("mission%03d: read the note", agent), Total: konst.TurnMaxSteps, State: roster.Finished, Started: *at, Report: "done with " + name}
		for call := range heapCalls {
			live, thinking, thought := session.EventIDFor(name, heapCallID(agent, call)), session.EventIDFor(name, "thinking "+heapCallID(agent, call)), heapThought(agent, call)
			for chunk := range heapThoughtChunks {
				app.Update(Event{Kind: EventThinking, ID: thinking, Agent: name, Text: thought[chunk*heapChunk : (chunk+1)*heapChunk]})
			}
			app.Update(Event{Kind: EventToolCall, ID: live, Agent: name, Tool: "read", Text: heapPath(agent, call)})
			app.Update(Event{Kind: EventToolResult, ID: live, Agent: name, Text: "84 lines, 2.1 KB"})
			row.Calls = append(row.Calls, subagent.Call{ID: live, At: at.Add(time.Duration(call) * time.Second), Tool: "read", Text: heapPath(agent, call), Result: "2.1 KB"})
		}
		row.Steps, row.Ended = heapCalls, at.Add(heapCalls*time.Second)
	}
	return app, rows
}

func TestFoldedSubAgentsLeaveTheHeapAndOpenFromTheSession(t *testing.T) {
	at := fixedStart()
	app, rows := crowdApp(t, &at)
	at = at.Add(konst.SettledSubAgentFoldMinutes*time.Minute + time.Hour)
	app.Update(Event{Kind: EventSubAgent, SubAgents: slices.Clone(rows)})
	app.show(screenAgents)
	app.View()
	if screen := ansi.Strip(app.View().Content); !strings.Contains(screen, fmt.Sprintf("%d folded", heapAgents)) {
		t.Fatalf("the crowd did not fold\n%s", screen)
	}
	folded, thought := heapAlloc(), uint64(heapAgents*heapCalls*heapThoughtChunks*heapChunk)
	t.Logf("folded, at once: heap %.1f MB, %d feed events held, %.1f MB of thinking streamed", mb(folded), len(app.happened), mb(thought))
	if folded > thought/3 {
		t.Errorf("the folded heap is %.1f MB, over a third of the %.1f MB of thinking the crowd streamed", mb(folded), mb(thought))
	}
	samples := []uint64{folded}
	for tick := time.Duration(0); tick < heapSettledFor; tick += heapHostTick {
		at = at.Add(heapHostTick)
		app.Update(Event{Kind: EventSubAgent, SubAgents: slices.Clone(rows)})
		app.Update(pulseMsg{})
		if tick%time.Second == 0 {
			app.View()
		}
		if (tick+heapHostTick)%(5*time.Minute) == 0 {
			samples = append(samples, heapAlloc())
			t.Logf("folded, %v later: heap %.1f MB, %d feed events held", tick+heapHostTick, mb(samples[len(samples)-1]), len(app.happened))
		}
	}
	if last := samples[len(samples)-1]; last > folded+folded*heapFlatPercent/100 {
		t.Errorf("the folded heap grew from %.1f MB to %.1f MB over %v of host updates", mb(folded), mb(last), heapSettledFor)
	}

	searched := heapName(heapSearched)
	app.Update(tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl})
	typeText(app, fmt.Sprintf("mission%03d", heapSearched))
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	opened := ansi.Strip(app.View().Content)
	t.Logf("opened %s by search at %dx%d:\n%s", searched, heapWidth, heapHeight, opened)
	if !strings.Contains(opened, "[&"+searched+"]") || !strings.Contains(opened, heapPath(heapSearched, heapCalls-1)) {
		t.Fatalf("the search for mission%03d did not open %s with its calls\n%s", heapSearched, searched, opened)
	}
	t.Logf("opened: heap %.1f MB, %d feed events held", mb(heapAlloc()), len(app.happened))
}

func TestACrowdOf180000FeedEventsSharesNoShortID(t *testing.T) {
	at := fixedStart()
	app, _ := crowdApp(t, &at)
	seen := make(map[string]bool, len(app.happened))
	for _, event := range app.happened {
		if seen[event.ID] {
			t.Errorf("two feed events share the short id %s", event.ID)
		}
		seen[event.ID] = true
	}
	t.Logf("%d feed events held for %d streamed, %d short ids lengthened", len(app.happened), 2*heapAgents*heapCalls, len(app.lengthenedIDs))
	if len(app.happened) != 2*heapAgents*heapCalls {
		t.Errorf("the feed holds %d events, not the %d the crowd streamed", len(app.happened), 2*heapAgents*heapCalls)
	}
}

func TestAFoldedSubAgentsThinkingOpensBySearch(t *testing.T) {
	at := fixedStart()
	app, rows := crowdApp(t, &at)
	at = at.Add(konst.SettledSubAgentFoldMinutes*time.Minute + time.Hour)
	app.Update(Event{Kind: EventSubAgent, SubAgents: slices.Clone(rows)})
	app.show(screenAgents)
	app.Update(tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl})
	typeText(app, fmt.Sprintf("ponders%dx%d ", heapSearched, heapCalls-1))
	listed := ansi.Strip(app.View().Content)
	if !strings.Contains(listed, "thinking") {
		t.Fatalf("the search lists no thinking of the folded %s\n%s", heapName(heapSearched), listed)
	}
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	opened := ansi.Strip(app.View().Content)
	t.Logf("opened the thinking of %s by search at %dx%d:\n%s", heapName(heapSearched), heapWidth, heapHeight, opened)
	if !strings.Contains(opened, fmt.Sprintf("ponders%dx%d", heapSearched, heapCalls-1)) {
		t.Fatalf("the thinking found by search is not on screen\n%s", opened)
	}
}

func TestOneMegabyteOfThinkingIn10000DeltasCostsLinearTime(t *testing.T) {
	at := fixedStart()
	app := newTestApp(Options{Repo: testRepo, Now: func() time.Time { return at }, Wires: anthropicAlone})
	app.Update(tea.WindowSizeMsg{Width: heapWidth, Height: heapHeight})
	delta := strings.Repeat("x", 100)
	streamed := func(deltas int) time.Duration {
		id := fmt.Sprintf("thinking-%d", deltas)
		began := time.Now()
		for range deltas {
			app.Update(Event{Kind: EventThinking, ID: id, Agent: "sub-1", Text: delta})
		}
		return time.Since(began)
	}
	half, whole := streamed(5000), streamed(10000)
	if at := app.happenedAt(short("thinking-10000")); at < 0 || len(app.happened[at].Body) != 10000*len(delta) {
		t.Fatalf("the streamed thinking is not 1 MB long")
	}
	t.Logf("0.5 MB in 5,000 deltas %v, 1 MB in 10,000 deltas %v, ratio %.2f", half, whole, float64(whole)/float64(half))
	if whole > 3*half {
		t.Errorf("twice the deltas took %.1f times as long, so the thinking does not grow in linear time", float64(whole)/float64(half))
	}
}
