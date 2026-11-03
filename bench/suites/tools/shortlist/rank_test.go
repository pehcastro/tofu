package shortlist

import "testing"

func TestBM25RanksTheFileThatUsesTheQueryWordsHighest(t *testing.T) {
	files := []File{
		{Path: "src/store.ts", Content: "the filter belongs in the storage code, the list method takes a filter argument"},
		{Path: "README.md", Content: "install and run instructions"},
		{Path: "package.json", Content: "scripts and dependencies"},
	}
	ranked := BM25Rank("give GET /tasks a filter argument in the storage code", files)
	if ranked[0].Path != "src/store.ts" {
		t.Fatalf("BM25 picked %q first, want src/store.ts: %+v", ranked[0].Path, ranked)
	}
}

func TestGrepRanksOnLiteralWordCountAlone(t *testing.T) {
	files := []File{
		{Path: "README.md", Content: "readme readme readme"},
		{Path: "src/store.ts", Content: "the filter belongs in the storage code"},
	}
	ranked := GrepRank("readme readme readme", files)
	if ranked[0].Path != "README.md" {
		t.Fatalf("grep picked %q first, want README.md: %+v", ranked[0].Path, ranked)
	}
}

func TestHitAtCountsALabelInsideTheWindowAndNotOutsideIt(t *testing.T) {
	ranked := []Ranked{{Path: "a"}, {Path: "b"}, {Path: "c"}}
	if !HitAt(ranked, []string{"b"}, 2) {
		t.Fatal("b is at rank 2 and HitAt(2) missed it")
	}
	if HitAt(ranked, []string{"c"}, 2) {
		t.Fatal("c is at rank 3 and HitAt(2) should not have counted it")
	}
	if !HitAt(ranked, []string{"c"}, 5) {
		t.Fatal("HitAt clamps k to len(ranked) and should still find c")
	}
}

func TestShortlistKeepsTopRankedFilesInOrder(t *testing.T) {
	files := []File{{Path: "a", Content: "x"}, {Path: "b", Content: "y"}, {Path: "c", Content: "z"}}
	ranked := []Ranked{{Path: "c", Score: 3}, {Path: "a", Score: 2}, {Path: "b", Score: 1}}
	got := Shortlist(files, ranked, 2)
	if len(got) != 2 || got[0].Path != "c" || got[1].Path != "a" {
		t.Fatalf("Shortlist(2) = %+v, want [c a]", got)
	}
}

func TestReadCorpusLoadsTheRecordedRows(t *testing.T) {
	rows, err := ReadCorpus()
	if err != nil {
		t.Fatalf("ReadCorpus: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("the corpus is empty")
	}
	for _, row := range rows {
		if row.TurnID == "" || row.Task == "" || len(row.Label) == 0 || len(row.Files) == 0 {
			t.Fatalf("row %q is missing a field it needs to be scored", row.TurnID)
		}
		labelExists := false
		for _, f := range row.Files {
			if row.Hit(f.Path) {
				labelExists = true
			}
		}
		if !labelExists {
			t.Fatalf("row %q labels %v but none of its files carry that path", row.TurnID, row.Label)
		}
	}
}
