package method_test

import (
	"context"
	"errors"
	"os"
	"path"
	"slices"
	"strings"
	"testing"

	"tofu/internal/judge/method"
)

const libraryDir = "../../../library"

func shipped(t *testing.T) method.Table {
	t.Helper()
	table, err := method.Load(os.DirFS(libraryDir))
	if err != nil {
		t.Fatalf("loading the shipped method table: %v", err)
	}
	return table
}

func armThatRan(t *testing.T, table method.Table, point string) string {
	t.Helper()
	ran, chosen, err := method.Run(t.Context(), table, point, method.Arms[string]{
		Cheap:  func(context.Context) (string, error) { return "cheap", nil },
		Judged: func(context.Context) (string, error) { return "judged", nil },
	})
	if err != nil {
		t.Fatalf("%s: %v", point, err)
	}
	if ran != string(chosen) {
		t.Fatalf("%s: the %s arm ran and the table named %s", point, ran, chosen)
	}
	return ran
}

func TestTheTableNamesAMethodForEveryDecisionPointOnDisk(t *testing.T) {
	table := shipped(t)
	questions, err := os.ReadDir(path.Join(libraryDir, "questions"))
	if err != nil {
		t.Fatalf("reading the shipped questions: %v", err)
	}
	named := 0
	for _, entry := range questions {
		file := entry.Name()
		if !strings.HasSuffix(file, ".yaml") {
			continue
		}
		point, _, _ := strings.Cut(strings.TrimSuffix(file, ".yaml"), "@")
		if _, err := table.Of(point); err != nil {
			t.Errorf("%v", err)
			continue
		}
		named++
	}
	if named == 0 {
		t.Fatal("no question file was read, so this test proves nothing")
	}
	for _, coming := range []string{"file_shortlist", "instruction_trust", "spawn_gate"} {
		if _, err := table.Of(coming); err != nil {
			t.Errorf("%v", err)
		}
	}
}

func TestShellSiftAndStopCheckAreDecidedByTheJudgedMethod(t *testing.T) {
	table := shipped(t)
	for _, point := range []string{"shell_sift", "stop_check"} {
		if ran := armThatRan(t, table, point); ran != "judged" {
			t.Errorf("%s ran the %s arm, and the table has it on the judged one", point, ran)
		}
	}
}

func TestReadWorthIsDecidedByTheCheapMethod(t *testing.T) {
	if ran := armThatRan(t, shipped(t), "read_worth"); ran != "cheap" {
		t.Errorf("read_worth ran the %s arm, and the table has it on the cheap one", ran)
	}
}

func TestAnUnwiredPointRunsNeitherArmAndSaysWhy(t *testing.T) {
	table := shipped(t)
	for _, point := range []string{"page_sift", "ask", "file_shortlist", "tool_gate", "instruction_trust", "spawn_gate"} {
		_, chosen, err := method.Run(t.Context(), table, point, method.Arms[bool]{
			Cheap:  func(context.Context) (bool, error) { t.Fatalf("%s ran the cheap arm", point); return false, nil },
			Judged: func(context.Context) (bool, error) { t.Fatalf("%s ran the judged arm", point); return false, nil },
		})
		var unwired method.UnwiredError
		if !errors.As(err, &unwired) {
			t.Fatalf("%s: err = %v, and an unwired point refuses rather than picks", point, err)
		}
		if chosen != method.Unwired {
			t.Errorf("%s: chose %s", point, chosen)
		}
		if unwired.Choice.Why == "" {
			t.Errorf("%s: unwired and no reason given", point)
		}
	}
}

func TestChangingOnePointsMethodIsOneEdit(t *testing.T) {
	file := path.Join(libraryDir, method.TableFile)
	before, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("reading %s: %v", file, err)
	}
	original := strings.Split(strings.ReplaceAll(string(before), "\r\n", "\n"), "\n")
	lines := slices.Clone(original)
	editedLine := 0
	for i, line := range lines {
		if strings.TrimSpace(line) != "read_worth:" {
			continue
		}
		for j := i + 1; j < len(lines); j++ {
			if key, _, _ := strings.Cut(strings.TrimSpace(lines[j]), ":"); key == "method" {
				lines[j] = "    method: judged"
				editedLine = j + 1
				break
			}
		}
		break
	}
	if editedLine == 0 {
		t.Fatalf("%s names no method line under read_worth", file)
	}
	differing := 0
	for i, line := range original {
		if line != lines[i] {
			differing++
		}
	}
	if differing != 1 {
		t.Fatalf("%d lines differ, and changing a point's method is one edit", differing)
	}
	table, err := method.Parse([]byte(strings.Join(lines, "\n")), file)
	if err != nil {
		t.Fatalf("parsing the edited table: %v", err)
	}
	if ran := armThatRan(t, table, "read_worth"); ran != "judged" {
		t.Fatalf("one edit at line %d and read_worth still ran the %s arm", editedLine, ran)
	}
	if ran := armThatRan(t, shipped(t), "read_worth"); ran != "cheap" {
		t.Fatalf("the shipped table changed on disk: read_worth ran the %s arm", ran)
	}
}

func TestAWiredPointStatingNoCostIsRefused(t *testing.T) {
	_, err := method.Parse([]byte("kind: method_table\ntable_version: 1\nmethods:\n  shell_sift:\n    method: judged\n    why: jev keeps more needles\n"), "in the test")
	if err == nil || !strings.Contains(err.Error(), "states no cost") {
		t.Fatalf("err = %v, and a point that spends money without a figure is refused", err)
	}
}
