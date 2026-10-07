package memory

import (
	"slices"
	"testing"
)

func TestOfferPrecisionAndRecallOnTheSampleChain(t *testing.T) {
	corpus, answers, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	wanted := 0
	for _, m := range corpus {
		if m.Offer {
			wanted++
		}
	}
	if len(corpus) != 54 || (len(answers) > 0 && len(answers) != len(corpus)) || wanted == 0 || wanted == len(corpus) {
		t.Fatalf("%d messages, %d jev answers, %d labelled offer: the corpus and its answers do not line up", len(corpus), len(answers), wanted)
	}
	t.Logf("%d messages, %d labelled offer. labels: %s", len(corpus), wanted, LabelSource)
	if len(answers) == 0 {
		t.Logf("%-28s owes a run: %s holds no answers for this corpus", JevArm, JevAnswerFile)
	}
	arms := Arms(answers)
	names := make([]string, 0, len(arms))
	for name := range arms {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		s := Measure(corpus, name, arms[name])
		if s.TruePos+s.FalsePos+s.FalseNeg == 0 {
			t.Errorf("%s decided nothing on %d messages", name, len(corpus))
		}
		t.Logf("%-28s offered %2d  right %2d  wrong %2d  missed %2d  precision %.2f  recall %.2f", name, s.TruePos+s.FalsePos, s.TruePos, s.FalsePos, s.FalseNeg, s.Precision(), s.Recall())
	}
}
