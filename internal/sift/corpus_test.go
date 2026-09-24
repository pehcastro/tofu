package sift

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type labelled struct {
	File  string
	Index int
	Keep  bool
	Part  Part
}

func readLabels(t *testing.T) map[string]bool {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", "labels.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for n, line := range strings.Split(string(body), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 3 {
			t.Fatalf("labels.tsv:%d: a label is file, index, keep or drop, and a note", n+1)
		}
		switch fields[2] {
		case "keep":
			out[fields[0]+":"+fields[1]] = true
		case "drop":
			out[fields[0]+":"+fields[1]] = false
		default:
			t.Fatalf("labels.tsv:%d: %q is not keep or drop", n+1, fields[2])
		}
	}
	return out
}

func corpus(t *testing.T) []labelled {
	t.Helper()
	labels := readLabels(t)
	var out []labelled
	var missing []string
	for _, path := range corpusFiles(t) {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		parts, err := Split(string(body))
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		name := strings.TrimSuffix(filepath.Base(path), ".txt")
		for i, p := range parts {
			keep, ok := labels[name+":"+strconv.Itoa(i)]
			if !ok {
				missing = append(missing, fmt.Sprintf("%s\t%d\t?\t%s", name, i, oneLine(p.Text)))
				continue
			}
			out = append(out, labelled{File: name, Index: i, Keep: keep, Part: p})
		}
	}
	if len(missing) > 0 {
		t.Fatalf("%d paragraphs carry no hand label:\n%s", len(missing), strings.Join(missing, "\n"))
	}
	return out
}

func oneLine(text string) string {
	flat := strings.Join(strings.Fields(text), " ")
	if len(flat) > 240 {
		return flat[:240]
	}
	return flat
}

const handLabelFloor = 40

func TestEveryCorpusParagraphIsHandLabelled(t *testing.T) {
	set := corpus(t)
	if len(set) < handLabelFloor {
		t.Fatalf("the hand labelled set is %d paragraphs and the ticket asks for %d", len(set), handLabelFloor)
	}
	drops := 0
	for _, item := range set {
		if !item.Keep {
			drops++
		}
	}
	t.Logf("%d paragraphs, %d labelled keep, %d labelled drop", len(set), len(set)-drops, drops)
}

func TestEveryOfflineArmAgainstTheHandLabels(t *testing.T) {
	set := corpus(t)
	report(t, "brevity", set, Cheap)
	report(t, "signpost", set, Signpost)
	report(t, "keep everything", set, func(Part) Mark { return Mark{Keep: true} })
	report(t, "drop a markdown heading only", set, headingOnly)

	fired := map[string]int{}
	for _, item := range set {
		mark := Cheap(item.Part)
		if mark.Keep {
			continue
		}
		for _, flag := range strings.Split(mark.Reason, "; ") {
			fired[checkName(flag)]++
		}
	}
	for name, n := range fired {
		t.Logf("brevity check %q fired on %d of %d paragraphs", name, n, len(set))
	}
}

func checkName(flag string) string {
	switch {
	case strings.HasSuffix(flag, "words past the cap"):
		return "word cap"
	case strings.HasSuffix(flag, "em dashes"):
		return "em dash"
	case strings.HasSuffix(flag, "bold phrases"):
		return "bold cap"
	case strings.HasPrefix(flag, "banned words"):
		return "banned words"
	}
	return flag
}

func headingOnly(p Part) Mark {
	if strings.HasPrefix(strings.TrimSpace(p.Text), "#") {
		return Mark{Reason: "a markdown heading"}
	}
	return Mark{Keep: true}
}

func report(t *testing.T, arm string, set []labelled, judge func(Part) Mark) {
	t.Helper()
	var agree, keptWhenDrop, droppedWhenKeep int
	for _, item := range set {
		switch mark := judge(item.Part); {
		case mark.Keep == item.Keep:
			agree++
		case item.Keep:
			droppedWhenKeep++
		default:
			keptWhenDrop++
		}
	}
	t.Logf("%s arm: %d/%d agree (%.0f%%), %d padding kept, %d answers dropped",
		arm, agree, len(set), 100*float64(agree)/float64(len(set)), keptWhenDrop, droppedWhenKeep)
}

func corpusFiles(t *testing.T) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("testdata", "corpus", "*.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("the corpus is empty, so nothing is proved")
	}
	return paths
}
