package session_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"tofu/internal/host"
	"tofu/internal/memory"
	"tofu/internal/session"
	"tofu/internal/turn"
)

const (
	followHolderVariable = "TOFU_TEST_FOLLOW_HOLDER"
	followSoakVariable   = "TOFU_FOLLOW_SOAK"
	streamTask           = "stream "
	slowTask             = "slow"
	ranIn                = "ran in "
	latencyEvents        = 100
	tokenGap             = 10 * time.Millisecond
	latencyCeiling       = 20 * time.Millisecond
	takeOverCeiling      = 2 * time.Second
	slowPause            = 2 * time.Second
	eventWait            = 10 * time.Second
	pollGap              = 50 * time.Millisecond
)

type holder struct {
	cmd   *exec.Cmd
	id    string
	in    io.WriteCloser
	out   *bufio.Reader
	ended chan struct{}
	exit  error
}

func (h *holder) pid() int { return h.cmd.Process.Pid }

func (h *holder) asked(t *testing.T, what string) int {
	t.Helper()
	if _, err := io.WriteString(h.in, what+"\n"); err != nil {
		t.Fatal(err)
	}
	said, err := h.out.ReadString('\n')
	count, err2 := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(said), what+" "))
	if err != nil || err2 != nil {
		t.Fatalf("the holder answered %s with %q, %v", what, said, err)
	}
	return count
}

func play(_ context.Context, _ host.Pick, task string, live host.Live) {
	say := func(text string) { live.Emit(host.Event{Kind: host.EventTextDelta, Text: text}) }
	count, streams := strings.CutPrefix(task, streamTask)
	switch {
	case streams:
		n, _ := strconv.Atoi(count)
		for range n {
			say(strconv.FormatInt(time.Now().UnixNano(), 10))
			time.Sleep(tokenGap)
		}
	case task == slowTask:
		say("first half, ")
		time.Sleep(slowPause)
		say("second half")
	default:
		live.Emit(host.Event{Kind: host.EventText, Text: ranIn + strconv.Itoa(os.Getpid()) + ": " + task})
	}
}

func TestFollowHolderProcess(t *testing.T) {
	project := os.Getenv(followHolderVariable)
	if project == "" {
		t.Skip("runs only as the holding process of another Follow test")
	}
	h, _ := host.New(host.Config{Dir: project, Play: play})
	id, err := h.OpenFresh()
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for range h.Events() {
		}
	}()
	_, _ = os.Stdout.WriteString("held " + id + "\n")
	for lines := bufio.NewScanner(os.Stdin); lines.Scan(); {
		switch lines.Text() {
		case "goroutines":
			_, _ = os.Stdout.WriteString("goroutines " + strconv.Itoa(runtime.NumGoroutine()) + "\n")
		case "heap":
			runtime.GC()
			var stats runtime.MemStats
			runtime.ReadMemStats(&stats)
			_, _ = os.Stdout.WriteString("heap " + strconv.FormatUint(stats.HeapInuse/1024, 10) + "\n")
		}
	}
	h.Close()
}

func startHolder(t *testing.T) (*holder, string) {
	t.Helper()
	home, project := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	cmd := exec.Command(os.Args[0], "-test.run=^TestFollowHolderProcess$")
	cmd.Env = append(os.Environ(), followHolderVariable+"="+project)
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	h := &holder{cmd: cmd, in: in, out: bufio.NewReader(out), ended: make(chan struct{})}
	go func() {
		h.exit = cmd.Wait()
		close(h.ended)
	}()
	t.Cleanup(func() {
		_ = in.Close()
		select {
		case <-h.ended:
		case <-time.After(eventWait):
			_ = cmd.Process.Kill()
			<-h.ended
		}
	})
	said, err := h.out.ReadString('\n')
	id, held := strings.CutPrefix(strings.TrimSpace(said), "held ")
	if !held {
		t.Fatalf("the holding process said %q, %v", said, err)
	}
	h.id = id
	return h, project
}

func follow(t *testing.T, project string, h *holder) *host.Host {
	t.Helper()
	follower, troubles := host.New(host.Config{Dir: project, Play: play, Resumed: host.Carry{Session: h.id}})
	t.Cleanup(follower.Close)
	if !slices.ContainsFunc(troubles, func(one string) bool {
		return strings.Contains(one, "process "+strconv.Itoa(h.pid())+" has held session "+h.id) && strings.Contains(one, "follows it")
	}) {
		t.Fatalf("the follower said %q, want it following %s in process %d", troubles, h.id, h.pid())
	}
	return follower
}

func next(t *testing.T, h *host.Host, kind host.EventKind, saying string) host.Event {
	t.Helper()
	gaveUp := time.After(eventWait)
	for {
		select {
		case event := <-h.Events():
			if event.Kind == kind && strings.Contains(event.Text, saying) {
				return event
			}
		case <-gaveUp:
			t.Fatalf("no event of kind %d saying %q in %s", kind, saying, eventWait)
		}
	}
}

func until(t *testing.T, what string, done func() bool) {
	t.Helper()
	for gaveUp := time.Now().Add(eventWait); !done(); time.Sleep(pollGap) {
		if time.Now().After(gaveUp) {
			t.Fatalf("%s did not happen in %s", what, eventWait)
		}
	}
}

func TestFollowStreamsTheHoldersTurnAndRunsAForwardedPromptThere(t *testing.T) {
	held, project := startHolder(t)
	follower := follow(t, project, held)

	if !follower.Send(host.Pick{}, "say hello") {
		t.Fatal("the holder refused a prompt forwarded while it was idle")
	}
	next(t, follower, host.EventText, ranIn+strconv.Itoa(held.pid())+": say hello")
	next(t, follower, host.EventTurnEnded, "")

	trips := make([]time.Duration, latencyEvents)
	for at := range trips {
		sent := time.Now()
		if !follower.Send(host.Pick{}, "ping "+strconv.Itoa(at)) {
			t.Fatal("the holder refused a ping")
		}
		next(t, follower, host.EventText, ": ping "+strconv.Itoa(at))
		trips[at] = time.Since(sent)
		next(t, follower, host.EventTurnEnded, "")
	}
	slices.Sort(trips)
	t.Logf("prompt forwarded to process %d and its answer shown here, %d round trips on one clock: median %s, p90 %s, max %s", held.pid(), len(trips),
		trips[len(trips)/2], trips[len(trips)*9/10], trips[len(trips)-1])

	if !follower.Send(host.Pick{}, streamTask+strconv.Itoa(latencyEvents)) {
		t.Fatal("the holder refused the streaming prompt")
	}
	next(t, follower, host.EventTurnStarted, streamTask)
	if _, running := follower.Turn(); !running {
		t.Fatal("the follower does not see the holder's turn as running")
	}
	took := make([]time.Duration, 0, latencyEvents)
	for len(took) < latencyEvents {
		sent, err := strconv.ParseInt(next(t, follower, host.EventTextDelta, "").Text, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		took = append(took, time.Since(time.Unix(0, sent)))
	}
	next(t, follower, host.EventTurnEnded, "")
	slices.Sort(took)
	median := took[len(took)/2]
	t.Logf("appended by process %d, read by process %d, %d events: min %s, median %s, p90 %s, max %s", held.pid(), os.Getpid(), len(took),
		took[0], median, took[len(took)*9/10], took[len(took)-1])
	if median >= latencyCeiling {
		t.Errorf("the median from append to follower is %s, want under %s", median, latencyCeiling)
	}
}

func TestFollowAttachMidTurnSeesTheTurnAndItsPartialText(t *testing.T) {
	held, project := startHolder(t)
	first := follow(t, project, held)
	if !first.Send(host.Pick{}, slowTask) {
		t.Fatal("the holder refused the slow prompt")
	}
	next(t, first, host.EventTextDelta, "first half")
	late := follow(t, project, held)
	next(t, late, host.EventTurnStarted, slowTask)
	if partial := next(t, late, host.EventTextDelta, ""); partial.Text != "first half, " {
		t.Fatalf("a follower attached mid-turn saw the partial text as %q", partial.Text)
	}
	if rest := next(t, late, host.EventTextDelta, ""); rest.Text != "second half" {
		t.Fatalf("the rest of the stream arrived as %q", rest.Text)
	}
	next(t, late, host.EventTurnEnded, "")
}

func TestFollowTakesTheLockWhenTheHolderIsKilled(t *testing.T) {
	held, project := startHolder(t)
	follower := follow(t, project, held)
	killed := time.Now()
	if err := held.cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	took := next(t, follower, host.EventNote, "holds it now")
	since := time.Since(killed)
	t.Logf("killed process %d; process %d took %s %s later and said: %s", held.pid(), os.Getpid(), held.id, since.Round(time.Millisecond), took.Text)
	if since > takeOverCeiling {
		t.Errorf("the follower held the session %s after the kill, want within %s", since, takeOverCeiling)
	}
	store, err := session.OpenIn(project)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Busy(held.id); err != nil {
		t.Fatalf("after the takeover the lock probe says %v, want this process holding it", err)
	}
	if !follower.Send(host.Pick{}, "after the kill") {
		t.Fatal("the new writer refused a prompt")
	}
	next(t, follower, host.EventText, ranIn+strconv.Itoa(os.Getpid())+": after the kill")
}

func TestFollowLeavesNothingListeningWhenEitherSideEnds(t *testing.T) {
	held, project := startHolder(t)
	store, err := session.OpenIn(project)
	if err != nil {
		t.Fatal(err)
	}
	channel := store.Channel(held.id)
	if _, err := os.Stat(channel); err != nil {
		t.Fatalf("the holder opened no channel at %s: %v", channel, err)
	}
	alone := held.asked(t, "goroutines")
	follower, _ := host.New(host.Config{Dir: project, Play: play, Resumed: host.Carry{Session: held.id}})
	until(t, "the holder seating its follower", func() bool { return held.asked(t, "goroutines") > alone })
	seated := held.asked(t, "goroutines")
	follower.Close()
	until(t, "the holder dropping the follower that quit", func() bool { return held.asked(t, "goroutines") == alone })
	t.Logf("holder goroutines alone %d, with a follower %d, after it quit %d", alone, seated, held.asked(t, "goroutines"))

	before := runtime.NumGoroutine()
	second := follow(t, project, held)
	if err := held.in.Close(); err != nil {
		t.Fatal(err)
	}
	<-held.ended
	if held.exit != nil {
		t.Fatalf("the holder did not end cleanly: %v", held.exit)
	}
	next(t, second, host.EventNote, "holds it now")
	second.Close()
	if _, err := os.Stat(channel); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the channel at %s is still there after both sides ended: %v", channel, err)
	}
	if conn, err := net.DialTimeout("unix", channel, time.Second); err == nil {
		_ = conn.Close()
		t.Fatalf("something still listens at %s", channel)
	}
	until(t, "every follow goroutine here ending", func() bool { return runtime.NumGoroutine() <= before })
	t.Logf("here: goroutines before following %d, after the holder quit and this side closed %d; %s is gone and nothing answers a dial", before, runtime.NumGoroutine(), channel)
}

func TestFollowEventsCrossTheChannelUnchanged(t *testing.T) {
	exit := 0
	sent := []host.Event{
		{Kind: host.EventTextDelta, Text: "a token"},
		{Kind: host.EventToolCall, ID: "call-1", Tool: "bash", Args: json.RawMessage(`{"command":"go test"}`)},
		{Kind: host.EventToolResult, ID: "call-1", Text: "ok", ExitCode: &exit},
		{Kind: host.EventDecision, ID: "call-1", Decision: &host.Decision{Tool: "bash", Verdict: host.Ask, Answers: []host.GateAnswer{{Question: "q", Value: 0.4, Max: 1}}}},
		{Kind: host.EventDecision, ID: "call-2", Decision: &host.Decision{Tool: "remember", MemoryScope: memory.Project}},
		{Kind: host.EventAwaitPerson, ID: "ask-1", Questions: []turn.PersonQuestion{{ID: "q1", Question: "which?", Options: []turn.PersonOption{{Label: "this"}}}}, Accepts: []host.ApprovalDecision{"accept"}},
		{Kind: host.EventPersisted, Logged: &session.Event{Seq: 3, Kind: session.EventMessage, Body: json.RawMessage(`{"role":"assistant"}`)}},
		{Kind: host.EventTurnStarted, ID: "turn-1", Text: "the task", Origin: host.Origin{Kind: host.OriginPerson}, LastAt: time.Date(2026, 10, 16, 1, 2, 3, 0, time.UTC)},
	}
	for _, event := range sent {
		line, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		var got host.Event
		_ = json.Unmarshal(line, &got)
		if string(got.Args) == "null" {
			got.Args = nil
		}
		if !reflect.DeepEqual(got, event) {
			t.Errorf("kind %d crossed as\n%+v\nwant\n%+v", event.Kind, got, event)
		}
	}
}

func TestFollowSoakHolderMemory(t *testing.T) {
	soak, err := time.ParseDuration(os.Getenv(followSoakVariable))
	if err != nil {
		t.Skip("set " + followSoakVariable + " to a duration, such as 10m, to soak a holder with a follower attached")
	}
	held, project := startHolder(t)
	follower := follow(t, project, held)
	first := held.asked(t, "heap")
	t.Logf("holder heap in use at attach: %d KiB", first)
	turns, tokens, began, minute := 0, 0, time.Now(), 1
	for time.Since(began) < soak {
		if !follower.Send(host.Pick{}, streamTask+strconv.Itoa(latencyEvents)) {
			t.Fatal("the holder refused a soak turn")
		}
		for event := next(t, follower, host.EventTurnStarted, ""); event.Kind != host.EventTurnEnded; event = <-follower.Events() {
			if event.Kind == host.EventTextDelta {
				tokens++
			}
		}
		turns++
		if time.Since(began) >= time.Duration(minute)*time.Minute {
			t.Logf("minute %d: holder heap in use %d KiB, holder goroutines %d, %d turns and %d tokens followed", minute, held.asked(t, "heap"), held.asked(t, "goroutines"), turns, tokens)
			minute++
		}
	}
	last := held.asked(t, "heap")
	t.Logf("holder heap in use at attach %d KiB, after %s %d KiB; the follower read %d tokens over %d turns", first, soak, last, tokens, turns)
	if last > first*2 {
		t.Errorf("the holder heap grew from %d to %d KiB with a follower attached", first, last)
	}
}
