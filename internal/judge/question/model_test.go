package question

import (
	"reflect"
	"testing"

	"tofu/internal/judge/jev"
)

func TestToJevCarriesChoiceOptionCriteria(t *testing.T) {
	q := Question{
		Name: "pick",
		Kind: KindChoice,
		Options: []Option{
			{Name: "read", Criteria: Criteria{Kind: CriteriaText, Text: "a file"}},
			{Name: "none"},
		},
		Escape: "none",
	}
	jq := q.ToJev()
	want := []jev.Option{
		{Name: "read", Criteria: "a file"},
		{Name: "none", Criteria: nil},
	}
	if !reflect.DeepEqual(jq.Options, want) {
		t.Fatalf("options = %+v, want %+v", jq.Options, want)
	}
}

func TestToJevCarriesNoulAndScoreShapes(t *testing.T) {
	noul := Question{Name: "ok", Kind: KindNoul, True: Criteria{Kind: CriteriaText, Text: "yes"}, False: Criteria{Kind: CriteriaText, Text: "no"}}
	jn := noul.ToJev()
	if jn.Kind != jev.QuestionNoul || jn.True != "yes" || jn.False != "no" {
		t.Fatalf("noul = %+v", jn)
	}

	score := Question{Name: "risk", Kind: KindScore, Levels: []Criteria{{Kind: CriteriaText, Text: "low"}, {Kind: CriteriaText, Text: "high"}}}
	js := score.ToJev()
	if js.Kind != jev.QuestionScore || len(js.Levels) != 2 || js.Levels[0] != "low" || js.Levels[1] != "high" {
		t.Fatalf("score = %+v", js)
	}
}
