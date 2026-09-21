package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"tofu/bench/readworth"
	"tofu/internal/judge/jev"
	"tofu/internal/konst"
	"tofu/internal/sift"
)

const liveEnvar = "TOFU_SIFT_LIVE"

func testdataDir() string {
	return filepath.Join("..", "..", "internal", "sift", "testdata")
}

func readCorpusFile(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(testdataDir(), "corpus", name+".txt"))
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func readTasks(t *testing.T) map[string]string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(testdataDir(), "tasks.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, line := range strings.Split(string(body), "\n") {
		name, task, ok := strings.Cut(strings.TrimRight(line, "\r"), "\t")
		if ok {
			out[name] = task
		}
	}
	return out
}

func readLabels(t *testing.T) map[string]bool {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(testdataDir(), "labels.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Split(strings.TrimRight(line, "\r"), "\t")
		if len(fields) < 3 || strings.HasPrefix(line, "#") {
			continue
		}
		out[fields[0]+":"+fields[1]] = fields[2] == "keep"
	}
	return out
}

func runSift(t *testing.T, args []string, in string) (string, string, int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := siftVerb(args, strings.NewReader(in), &out, &errOut)
	return out.String(), errOut.String(), code
}

func TestSiftOffArmMarksEveryParagraphAndStaysReversible(t *testing.T) {
	original := readCorpusFile(t, "control-t10")
	rendered, summary, code := runSift(t, []string{"--arm", "brevity"}, original)
	if code != exitOK {
		t.Fatalf("tofu sift --arm brevity exited %d: %s", code, summary)
	}
	if !strings.Contains(rendered, "kept ") {
		t.Fatalf("the output carries no kept fraction:\n%s", rendered)
	}
	t.Logf("%s", strings.TrimSpace(summary))
	for _, line := range strings.Split(rendered, "\n") {
		if strings.HasPrefix(line, "[sift:") {
			t.Logf("%s", line)
		}
	}
	restored, _, code := runSift(t, []string{"--restore"}, rendered)
	if code != exitOK {
		t.Fatalf("tofu sift --restore exited %d", code)
	}
	if restored != original {
		t.Fatal("the original is not recoverable from what tofu sift emits")
	}
}

func TestSiftDefaultsToTheLengthArmAndMakesNoCall(t *testing.T) {
	short := "just four words here"
	long := "this paragraph carries the real answer and runs well past the fifteen word floor on its own"
	rendered, summary, code := runSift(t, nil, short+"\n\n"+long+"\n")
	if code != exitOK {
		t.Fatalf("tofu sift exited %d: %s", code, summary)
	}
	if !strings.HasPrefix(summary, "length: ") {
		t.Fatalf("the default arm is not length: %q", summary)
	}
	if !strings.Contains(rendered, "[sift:0 under the word floor]") {
		t.Fatalf("the length arm did not remove the short paragraph:\n%s", rendered)
	}
	if !strings.Contains(rendered, long) {
		t.Fatalf("the length arm dropped the paragraph over the floor:\n%s", rendered)
	}
	if !strings.Contains(summary, "$0.000000") {
		t.Fatalf("the default arm spent money: %q", summary)
	}
}

func TestSiftArmJevStillReachesTheJudgment(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("OPENROUTER_KEY", "")

	long := "one two three four five six seven eight nine ten eleven twelve thirteen fourteen fifteen"
	_, errOut, code := runSift(t, []string{"--arm", armJev}, long+"\n")
	if code != exitUsage {
		t.Fatalf("tofu sift --arm jev exited %d, want %d: %s", code, exitUsage, errOut)
	}
	if !strings.Contains(errOut, "OPENROUTER_KEY") {
		t.Fatalf("the jev arm did not reach key resolution, so it did not run the judgment: %q", errOut)
	}
}

func TestTheLengthFloorMatchesTheBenchArm(t *testing.T) {
	if konst.SiftReadWorthWordFloor != 15 {
		t.Fatalf("bench/readworth/report-2026-09-21.md measured the winning floor at 15 words, konst carries %d", konst.SiftReadWorthWordFloor)
	}
	cases := []string{
		"",
		"one two three four five six seven eight nine ten eleven twelve thirteen fourteen",
		"one two three four five six seven eight nine ten eleven twelve thirteen fourteen fifteen",
		`I'll write a file called hello.txt with the word "hello".`,
		"**`src/store.ts`** -- `done` -> `completed`, added `priority: Priority` (`'low' | 'normal' | 'high'`, defaulting to `'normal'` in `create`), added a `TaskFilter` type, and `list(filter)` now does the filtering itself. Every caller passes a filter.",
	}
	for _, text := range cases {
		bench := readworth.ArmLength(readworth.Row{Paragraph: text}, konst.SiftReadWorthWordFloor)
		binary := sift.Length(sift.Part{Text: text})
		if bench.Keep != binary.Keep {
			t.Fatalf("%q: bench arm kept=%v, binary arm kept=%v", text, bench.Keep, binary.Keep)
		}
	}
}

func TestSiftRefusesAnUnknownArm(t *testing.T) {
	_, errOut, code := runSift(t, []string{"--arm", "vibes"}, "text\n")
	if code != exitUsage || !strings.Contains(errOut, "vibes") {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
}

func TestSiftRefusesAnUnknownArgument(t *testing.T) {
	_, errOut, code := runSift(t, []string{"--loud"}, "text\n")
	if code != exitUsage || !strings.Contains(errOut, "--loud") {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
}

func TestSiftLiveAgainstTheHandLabels(t *testing.T) {
	if os.Getenv(liveEnvar) == "" {
		t.Skipf("set %s=1 to spend real Jev calls", liveEnvar)
	}
	jev.AllowLiveCredential(t)
	tasks := readTasks(t)
	labels := readLabels(t)
	names, err := filepath.Glob(filepath.Join(testdataDir(), "corpus", "*.txt"))
	if err != nil {
		t.Fatal(err)
	}

	var agree, keptWhenDrop, droppedWhenKeep, judged, armsDiffer int
	var cost float64
	started := time.Now()
	for _, path := range names {
		name := strings.TrimSuffix(filepath.Base(path), ".txt")
		parts, err := sift.Split(readCorpusFile(t, name))
		if err != nil {
			t.Fatal(err)
		}
		marks, spent, err := siftMarks(parts, siftOpts{arm: armJev, task: tasks[name], noLog: true})
		if err != nil {
			t.Fatal(err)
		}
		cost += spent
		for i, mark := range marks {
			want, ok := labels[name+":"+strconv.Itoa(i)]
			if !ok {
				t.Fatalf("%s paragraph %d carries no hand label", name, i)
			}
			judged++
			if cheap := sift.Cheap(parts[i]); cheap.Keep != mark.Keep {
				armsDiffer++
			}
			switch {
			case mark.Keep == want:
				agree++
			case want:
				droppedWhenKeep++
				t.Logf("dropped an answer: %s:%d %s", name, i, mark.Reason)
			default:
				keptWhenDrop++
				t.Logf("kept padding: %s:%d", name, i)
			}
		}
	}
	elapsed := time.Since(started)
	t.Logf("jev arm: %d/%d agree (%.0f%%), %d padding kept, %d answers dropped",
		agree, judged, 100*float64(agree)/float64(judged), keptWhenDrop, droppedWhenKeep)
	t.Logf("%d paragraphs over %d messages in %s, $%.6f, $%.6f per paragraph",
		judged, len(names), elapsed.Round(time.Millisecond), cost, cost/float64(judged))
	t.Logf("the two arms disagree on %d of %d paragraphs", armsDiffer, judged)
}

func TestSiftLiveOneMessage(t *testing.T) {
	if os.Getenv(liveEnvar) == "" {
		t.Skipf("set %s=1 to spend real Jev calls", liveEnvar)
	}
	jev.AllowLiveCredential(t)
	name := "control-t10"
	original := readCorpusFile(t, name)
	rendered, summary, code := runSift(t, []string{"--arm", armJev, "--task", readTasks(t)[name]}, original)
	if code != exitOK {
		t.Fatalf("tofu sift exited %d: %s", code, summary)
	}
	t.Logf("%s", summary)
	t.Logf("\n%s", rendered)
	restored, _, code := runSift(t, []string{"--restore"}, rendered)
	if code != exitOK || restored != original {
		t.Fatal("the original is not recoverable from a live run")
	}
}
