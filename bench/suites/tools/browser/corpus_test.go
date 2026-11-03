package browserbench

import "testing"

func corpus(t *testing.T) []Case {
	t.Helper()
	cases, err := Load("pages")
	if err != nil {
		t.Fatal(err)
	}
	return cases
}

func TestNoGoalInTheCorpusLeaksItsAnswer(t *testing.T) {
	cases := corpus(t)
	for _, c := range cases {
		for _, leak := range Leaks(c) {
			t.Error(leak)
		}
	}
	t.Logf("%d pages checked for leakage", len(cases))
}

func TestAPlantedLeakingGoalIsCaught(t *testing.T) {
	byID := map[string]Case{}
	for _, c := range corpus(t) {
		byID[c.ID] = c
	}
	for _, planted := range []struct{ id, goal string }{
		{"drafts", "Click Delete beside my Holiday post."},
		{"drafts", "Press element 1 to remove the Holiday post."},
		{"drafts", "Remove the Holiday post with [1]."},
		{"results", "Open result 2 and tell me its price."},
		{"contact", "Tell support my kettle arrived broken. Use the damaged item subject."},
		{"hotel", "Pick the stay category that suits me."},
	} {
		c := byID[planted.id]
		c.Goal = planted.goal
		if leaks := Leaks(c); len(leaks) == 0 {
			t.Errorf("%s: the planted goal %q passed the leakage check", planted.id, planted.goal)
		}
	}
}
