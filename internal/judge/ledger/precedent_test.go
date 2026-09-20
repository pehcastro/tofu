package ledger

import (
	"fmt"
	"testing"
	"time"
)

var precedentStart = time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

func gateRow(id string, minutesBefore int, risk float64, approval float64) Row {
	return Row{
		ID:    id,
		Point: "tool_gate",
		At:    precedentStart.Add(-time.Duration(minutesBefore) * time.Minute),
		Answers: []Answer{
			{Question: "risk", Kind: AnswerScore, Score: risk, Dist: []Slice{{Option: "0"}, {Option: "1"}, {Option: "2"}, {Option: "3"}}},
			{Question: "approval", Kind: AnswerNoul, Noul: approval},
		},
		Verdict: VerdictAsk,
	}
}

func TestTheShortlistIsOrderedByDistance(t *testing.T) {
	target := gateRow("target", 0, 2, 0.50)
	near := gateRow("near", 10, 2, 0.52)
	middling := gateRow("middling", 20, 2, 0.58)
	far := gateRow("far", 30, 3, 0.90)

	found := Shortlist(target, []Row{far, middling, near})
	if len(found) != 2 {
		t.Fatalf("shortlist returned %d rows, want 2: %s", len(found), shortlistIDs(found))
	}
	if found[0].Row.ID != "near" || found[1].Row.ID != "middling" {
		t.Fatalf("shortlist order = %s, want near then middling", shortlistIDs(found))
	}
	if found[0].Distance >= found[1].Distance {
		t.Fatalf("distances are not ascending: %.4f then %.4f", found[0].Distance, found[1].Distance)
	}
	t.Logf("near %.4f, middling %.4f, and far was dropped", found[0].Distance, found[1].Distance)
}

func TestARowCarryingAnOutcomeOutranksAnEquallyDistantRowWithout(t *testing.T) {
	target := gateRow("target", 0, 2, 0.50)
	labelled := gateRow("labelled", 90, 2, 0.54)
	labelled.Outcome = &Outcome{At: precedentStart.Add(-time.Hour), Kind: "human_allowed"}
	recent := gateRow("recent", 5, 2, 0.54)

	found := Shortlist(target, []Row{recent, labelled})
	if len(found) != 2 {
		t.Fatalf("shortlist returned %d rows, want 2: %s", len(found), shortlistIDs(found))
	}
	if found[0].Distance != found[1].Distance {
		t.Fatalf("the two rows must be equally distant to test the tie, got %.4f and %.4f", found[0].Distance, found[1].Distance)
	}
	if found[0].Row.ID != "labelled" {
		t.Fatalf("shortlist order = %s, want the row carrying an outcome first", shortlistIDs(found))
	}
	t.Logf("equal distance %.4f, the labelled row from 90 minutes ago beat the unlabelled row from 5", found[0].Distance)
}

func TestAFingerprintMatchIsACandidateEvenWhenTheAnswersAreFar(t *testing.T) {
	target := gateRow("target", 0, 0, 0.10)
	target.Fingerprint = "bash.0123456789abcdef"
	sameKind := gateRow("same-kind", 40, 3, 0.95)
	sameKind.Fingerprint = target.Fingerprint
	unrelated := gateRow("unrelated", 41, 3, 0.95)

	found := Shortlist(target, []Row{sameKind, unrelated})
	if len(found) != 1 || found[0].Row.ID != "same-kind" {
		t.Fatalf("shortlist = %s, want the fingerprint match alone", shortlistIDs(found))
	}
	if !found[0].SameFingerprint {
		t.Fatal("the candidate came back without SameFingerprint set, so a reader cannot tell why it was returned")
	}
	t.Logf("distance %.4f, far past the near ceiling of %.2f, kept because the fingerprint matched", found[0].Distance, precedentNearCeiling)
}

func TestTheSameCallTakesTheShortlistAheadOfACloserStranger(t *testing.T) {
	target := gateRow("target", 0, 2, 0.50)
	target.Fingerprint = "bash.0123456789abcdef"
	sameCall := gateRow("same-call", 30, 2, 0.55)
	sameCall.Fingerprint = target.Fingerprint
	stranger := gateRow("stranger", 5, 2, 0.50)

	found := Shortlist(target, []Row{stranger, sameCall})
	if found[0].Row.ID != "same-call" {
		t.Fatalf("shortlist order = %s, want the same call first even though the stranger sits %.4f away",
			shortlistIDs(found), found[1].Distance)
	}
	if found[0].Distance <= found[1].Distance {
		t.Fatalf("this case only proves something when the stranger is strictly closer, got %.4f and %.4f", found[1].Distance, found[0].Distance)
	}
	t.Logf("the same call at %.4f outranked a stranger at %.4f, because 417 of 580 real rows fall inside the answer ceiling and the fingerprint is the only thing that narrows them",
		found[0].Distance, found[1].Distance)
}

func TestAnEmptyFingerprintNeverMatchesAnotherEmptyOne(t *testing.T) {
	target := gateRow("target", 0, 0, 0.10)
	old := gateRow("old", 40, 3, 0.95)
	if found := Shortlist(target, []Row{old}); len(found) != 0 {
		t.Fatalf("two rows with no fingerprint matched each other: %s", shortlistIDs(found))
	}
}

func TestTheShortlistStopsAtTen(t *testing.T) {
	target := gateRow("target", 0, 2, 0.50)
	var candidates []Row
	for i := 0; i < 25; i++ {
		candidates = append(candidates, gateRow(fmt.Sprintf("row-%02d", i), i+1, 2, 0.50))
	}
	found := Shortlist(target, candidates)
	if len(found) != precedentShortlistMax {
		t.Fatalf("25 equally close candidates produced %d rows, want %d", len(found), precedentShortlistMax)
	}
	if found[0].Row.ID != "row-00" {
		t.Fatalf("the most recent of the tied rows is %s, want row-00", found[0].Row.ID)
	}
}

func TestANewerRowAndAReplayAreNeverPrecedents(t *testing.T) {
	target := gateRow("target", 0, 2, 0.50)
	later := gateRow("later", -10, 2, 0.50)
	simultaneous := gateRow("simultaneous", 0, 2, 0.50)
	replay := gateRow("replay", 10, 2, 0.50)
	replay.ReplayOf = "some-earlier-row"

	if found := Shortlist(target, []Row{later, simultaneous, replay, target}); len(found) != 0 {
		t.Fatalf("shortlist = %s, want nothing: later, simultaneous, replayed and self are all disqualified", shortlistIDs(found))
	}
}

func TestAnswerDistanceReadsEachKind(t *testing.T) {
	levels := []Slice{{Option: "0"}, {Option: "1"}, {Option: "2"}, {Option: "3"}}
	cases := []struct {
		what  string
		left  []Answer
		right []Answer
		want  float64
	}{
		{
			what:  "a noul moves by its own difference",
			left:  []Answer{{Question: "approval", Kind: AnswerNoul, Noul: 0.20}},
			right: []Answer{{Question: "approval", Kind: AnswerNoul, Noul: 0.50}},
			want:  0.30,
		},
		{
			what:  "a score is divided by its levels",
			left:  []Answer{{Question: "risk", Kind: AnswerScore, Score: 0, Dist: levels}},
			right: []Answer{{Question: "risk", Kind: AnswerScore, Score: 3, Dist: levels}},
			want:  1,
		},
		{
			what:  "a choice is all or nothing",
			left:  []Answer{{Question: "kind", Kind: AnswerChoice, Choice: "refactor"}},
			right: []Answer{{Question: "kind", Kind: AnswerChoice, Choice: "behaviour"}},
			want:  1,
		},
		{
			what:  "the shared questions are averaged and the unshared ignored",
			left:  []Answer{{Question: "approval", Kind: AnswerNoul, Noul: 0.20}, {Question: "risk", Kind: AnswerScore, Score: 0, Dist: levels}, {Question: "gone", Kind: AnswerNoul, Noul: 1}},
			right: []Answer{{Question: "approval", Kind: AnswerNoul, Noul: 0.60}, {Question: "risk", Kind: AnswerScore, Score: 3, Dist: levels}},
			want:  0.70,
		},
	}
	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			got, comparable := AnswerDistance(c.left, c.right)
			if !comparable {
				t.Fatal("the two answer sets share a question and came back incomparable")
			}
			if diff := got - c.want; diff > 1e-9 || diff < -1e-9 {
				t.Fatalf("distance = %.6f, want %.6f", got, c.want)
			}
		})
	}
}

func TestAnswerSetsWithNoQuestionInCommonAreIncomparable(t *testing.T) {
	left := []Answer{{Question: "risk", Kind: AnswerScore, Score: 1}}
	right := []Answer{{Question: "stalled", Kind: AnswerNoul, Noul: 0.5}}
	got, comparable := AnswerDistance(left, right)
	if comparable {
		t.Fatalf("two disjoint answer sets reported comparable at distance %.4f", got)
	}
	if got != answersUnrelated {
		t.Fatalf("distance = %.4f, want %.4f so an incomparable pair can never look close", got, answersUnrelated)
	}
	sameName := []Answer{{Question: "risk", Kind: AnswerNoul, Noul: 0.5}}
	if _, comparable := AnswerDistance(left, sameName); comparable {
		t.Fatal("one question answered as a score and as a noul was compared anyway")
	}
}

func TestPrecedentsReadTheLedgerAndCarryTheOutcome(t *testing.T) {
	dir := t.TempDir()
	writer := NewWriter(dir)
	shadow := &Reason{Question: "risk", Comparison: "risk_ask_at", Mode: ModeShadow}

	var labelledID string
	for _, row := range []Row{gateRow("", 30, 2, 0.52), gateRow("", 20, 3, 0.99), gateRow("", 10, 2, 0.50)} {
		row.Reason = shadow
		stored, err := writer.Append(row)
		if err != nil {
			t.Fatalf("append: %v", err)
		}
		if labelledID == "" {
			labelledID = stored.ID
		}
	}
	if err := writer.Backfill(labelledID, Outcome{Kind: "human_allowed", Detail: "you allowed it after asking"}); err != nil {
		t.Fatalf("backfill: %v", err)
	}

	target := gateRow("2026-09-20-ffffffffffffffffffffffffffffffff", 0, 2, 0.51)
	found, err := NewReader(dir).Precedents(target)
	if err != nil {
		t.Fatalf("precedents: %v", err)
	}
	if len(found) != 2 {
		t.Fatalf("precedents returned %d rows, want 2: %s", len(found), shortlistIDs(found))
	}
	if found[0].Row.ID != labelledID {
		t.Fatalf("first precedent is %s, want the outcome-bearing row %s", found[0].Row.ID, labelledID)
	}
	if found[0].Row.Outcome == nil || found[0].Row.Outcome.Kind != "human_allowed" {
		t.Fatalf("the outcome did not come back on the row: %+v", found[0].Row.Outcome)
	}
	t.Logf("%s at %.4f carrying %s, then %s at %.4f",
		found[0].Row.ID, found[0].Distance, found[0].Row.Outcome.Kind, found[1].Row.ID, found[1].Distance)
}

func shortlistIDs(found []Precedent) string {
	if len(found) == 0 {
		return "nothing"
	}
	out := ""
	for i, one := range found {
		if i > 0 {
			out += ", "
		}
		out += fmt.Sprintf("%s(%.4f)", one.Row.ID, one.Distance)
	}
	return out
}
