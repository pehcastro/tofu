package cron

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"tofu/internal/konst"
)

var monday = time.Date(2026, 10, 5, 14, 30, 0, 0, time.Local)

func TestCronScheduleRefusesWhatWouldMisfire(t *testing.T) {
	for _, line := range []string{"*/0 * * * *", "5-1 * * * *", "60 * * * *", "0 0 31 2 *", "every 10s", "0 9 * *", "nonsense"} {
		if _, err := Parse(line, monday); err == nil {
			t.Errorf("%q parsed, want a refusal", line)
		}
	}
	cases := []struct {
		line string
		want time.Time
	}{
		{"@hourly", time.Date(2026, 10, 5, 15, 0, 0, 0, time.Local)},
		{"@daily", time.Date(2026, 10, 6, 0, 0, 0, 0, time.Local)},
		{"0 9 1 * 1", time.Date(2026, 10, 12, 9, 0, 0, 0, time.Local)},
		{"*/20 14 * * *", time.Date(2026, 10, 5, 14, 40, 0, 0, time.Local)},
		{"0 0 * * 7", time.Date(2026, 10, 11, 0, 0, 0, 0, time.Local)},
		{"once at 09:00", time.Date(2026, 10, 6, 9, 0, 0, 0, time.Local)},
		{"every 1m", monday.Add(time.Minute)},
	}
	for _, one := range cases {
		schedule, err := Parse(one.line, monday)
		if err != nil {
			t.Errorf("%q: %v", one.line, err)
			continue
		}
		if got := schedule.Next(monday); !got.Equal(one.want) {
			t.Errorf("%q next after %s = %s, want %s", one.line, monday, got, one.want)
		}
	}
}

func TestCronBookKeepsItsGuards(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s", "cron.json")
	checks := map[string]int{}
	book := &Book{Check: func(_ context.Context, command string) (int, string, error) {
		checks[command]++
		if command == "broken" {
			return 0, "", errors.New("no shell")
		}
		if checks[command] < 3 {
			return 1, "FAIL TestAdd", nil
		}
		return 0, "ok", nil
	}}
	if err := book.Keep(path); err != nil {
		t.Fatal(err)
	}
	loop, err := book.Create(Spec{Schedule: "every 1m", Prompt: "check the build"}, Person, "made by /loop", monday)
	if err != nil || !loop.Spec().Expires.Equal(monday.Add(konst.CronExpiryHours*time.Hour)) {
		t.Fatalf("a loop with no expiry: %v, expires %s, want the default", err, loop.Spec().Expires)
	}
	hourLater := monday.Add(time.Hour)
	if fires := book.Due(context.Background(), hourLater, Tick); len(fires) != 1 || fires[0].Prompt == "" {
		t.Fatalf("an hour of missed minutes fired %d times, want once: %+v", len(fires), fires)
	}
	if fires := book.Due(context.Background(), hourLater.Add(2*time.Minute), Tick); len(fires) != 0 {
		t.Errorf("a job fired again while its turn still ran: %+v", fires)
	}
	refusals := map[string]error{}
	_, refusals["no reason"] = book.Update(loop.ID, Change{Schedule: "every 5m"}, Person, "", monday)
	_, refusals["agent writes a check"] = book.Update(loop.ID, Change{Until: "rm -rf ."}, Agent, "faster", monday)
	_, refusals["agent stretches twice"] = book.Update(loop.ID, Change{Expires: monday.Add(3 * konst.CronExpiryHours * time.Hour)}, Agent, "longer", monday)
	_, refusals["goal without a check"] = book.Create(Spec{Schedule: string(KindAfterTurn), Prompt: "pass"}, Person, "made by /goal", monday)
	_, refusals["nothing changed"] = book.Update(loop.ID, Change{Schedule: "every 1m"}, Person, "same", monday)
	for name, err := range refusals {
		if err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	stretched, err := book.Update(loop.ID, Change{Schedule: "every 5m", Expires: monday.Add(2 * konst.CronExpiryHours * time.Hour)}, Agent, "nothing changed in an hour", monday)
	if err != nil || stretched.Version() != 2 || stretched.Versions[1].By != Agent {
		t.Fatalf("an agent's one-step stretch: %v, %+v", err, stretched.Versions)
	}
	for range konst.CronUnchangedStop {
		book.Finished(loop.ID, "the build passes")
	}
	if note := book.Finished(loop.ID, "the build passes"); !strings.Contains(note, "changed nothing") {
		t.Errorf("the same answer %d times in a row left the job open: %q", konst.CronUnchangedStop+1, note)
	}
	goal, _ := book.Create(Spec{Schedule: string(KindAfterTurn), Prompt: "make the test pass", Until: "go test ./..."}, Person, "made by /goal", monday)
	for turn := 1; turn <= 3; turn++ {
		fires := book.Due(context.Background(), monday, TurnEnded)
		met := turn == 3
		if len(fires) != 1 || (fires[0].Prompt == "") != met {
			t.Fatalf("goal after turn %d: %+v, want a fire until the check exits 0 on the third", turn, fires)
		}
		book.Finished(goal.ID, "turn "+strconv.Itoa(turn))
	}
	broken, _ := book.Create(Spec{Schedule: string(KindAfterTurn), Prompt: "x", Until: "broken"}, Person, "made by /goal", monday)
	if fires := book.Due(context.Background(), monday, TurnEnded); len(fires) != 1 || !strings.Contains(fires[0].Line, "could not run") {
		t.Errorf("a check that cannot run: %+v, want the job ended saying so", fires)
	}
	if job, _ := book.Job(broken.ID); job.Ended == "" {
		t.Error("a check that cannot run was read as not met, and the goal stays open")
	}
	reread := &Book{}
	if err := reread.Load(path); err != nil || len(reread.Jobs()) != 3 {
		t.Fatalf("the session's jobs read back: %v, %d jobs", err, len(reread.Jobs()))
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := reread.Load(path); err == nil {
		t.Error("a corrupt file loaded")
	}
	if _, err := reread.Create(Spec{Schedule: "every 1m", Prompt: "after"}, Person, "made after a failed load", monday); err != nil {
		t.Fatal(err)
	}
	if body, _ := os.ReadFile(path); string(body) != "{not json" {
		t.Errorf("a failed load let the next save overwrite the file: %q", body)
	}
}

func TestCronBookWritesNothingEmptyAndNeverOverAFileItCannotRead(t *testing.T) {
	dir := t.TempDir()
	corrupt, fork := filepath.Join(dir, "a", "cron.json"), filepath.Join(dir, "b", "cron.json")
	book := &Book{}
	if err := book.Keep(fork); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(fork); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("an empty book wrote %s: %v", fork, err)
	}
	if err := os.MkdirAll(filepath.Dir(corrupt), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(corrupt, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := book.Load(corrupt); err == nil {
		t.Fatal("a corrupt file loaded")
	}
	if _, err := book.Create(Spec{Schedule: "every 1m", Prompt: "after"}, Person, "made after a failed load", monday); err != nil {
		t.Fatal(err)
	}
	for name, keeping := range map[string]*Book{"a book holding a job": book, "an empty book": {}} {
		if err := keeping.Keep(corrupt); err == nil || !strings.Contains(err.Error(), corrupt) {
			t.Errorf("%s kept over the unreadable file: %v, want a refusal naming it", name, err)
		}
	}
	if body, _ := os.ReadFile(corrupt); string(body) != "{not json" {
		t.Fatalf("the unreadable file was overwritten: %q", body)
	}
	if err := book.Keep(fork); err != nil {
		t.Fatal(err)
	}
	if err := book.Delete("c1"); err != nil {
		t.Fatal(err)
	}
	reread := &Book{}
	if err := reread.Load(fork); err != nil || len(reread.Jobs()) != 0 || reread.made != 1 {
		t.Fatalf("a fork after deleting its only job reads back %v, %d jobs, made %d, want no jobs and made 1", err, len(reread.Jobs()), reread.made)
	}
}

func TestCronOnceFiresOnceAndNotAgainTomorrow(t *testing.T) {
	book := &Book{}
	once, err := book.Create(Spec{Schedule: "once at 15:00", Prompt: "remind me to push"}, Person, "made by /cron", monday)
	if err != nil {
		t.Fatal(err)
	}
	at := monday.Add(30 * time.Minute)
	if fires := book.Due(context.Background(), at, Tick); len(fires) != 1 || fires[0].Prompt == "" {
		t.Fatalf("once at 15:00 at 15:00: %+v, want one fire", fires)
	}
	if note := book.Finished(once.ID, "reminded"); !strings.Contains(note, "fired once") {
		job, _ := book.Job(once.ID)
		t.Errorf("after its one fire the job stays open, next %s: %q", job.Next, note)
	}
}

func TestCronCommandsAreThinOverTheBook(t *testing.T) {
	book := &Book{}
	if _, err := book.Command(`/cron "*/5 * * * *" check the "build"`, monday); err != nil {
		t.Fatalf("a quoted 5-field line: %v", err)
	}
	if job, _ := book.Job("c1"); job.Spec().Schedule != "*/5 * * * *" || job.Spec().Prompt != "check the build" {
		t.Errorf("the quoted line split on its spaces: %+v", job.Spec())
	}
	for _, line := range []string{"/loop", "/loop 1m", "/goal make it pass", "/cron edit c1 --schedule \"every 2m\"", `/cron "every 2m`, "/cron history c9"} {
		if _, err := book.Command(line, monday); err == nil {
			t.Errorf("%q was taken, want a usage or a refusal", line)
		}
	}
	reply, err := book.Command(`/cron edit c1 --schedule "every 2m" --reason "the build keeps passing"`, monday)
	if err != nil || !strings.Contains(reply.Note, "v2") {
		t.Errorf("an edit with a reason: %v, %q", err, reply.Note)
	}
	if IsCommand("/goals do it") || IsCommand("/cronx") || !IsCommand("/loop 5m x") {
		t.Error("only /cron, /loop and /goal are cron commands")
	}
}
