package gate

import (
	"os"
	"testing"

	"tofu/internal/judge/jev"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

func scoreAnswerFixture(score float64) jev.Answer {
	return jev.Answer{Kind: jev.QuestionScore, Score: score}
}

func noulAnswerFixture(value float64) jev.Answer {
	return jev.Answer{Kind: jev.QuestionNoul, Noul: value}
}

func neutralAnswers(r Rule) map[string]jev.Answer {
	return map[string]jev.Answer{
		r.RiskQuestion:          scoreAnswerFixture(0),
		r.ApprovalQuestion:      noulAnswerFixture(0.5),
		r.UserRequestedQuestion: noulAnswerFixture(0.5),
		r.FromUntrustedQuestion: noulAnswerFixture(0),
	}
}
