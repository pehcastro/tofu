package policy

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

func neutralAnswers(pol Policy) map[string]jev.Answer {
	return map[string]jev.Answer{
		pol.RiskQuestion:          scoreAnswerFixture(0),
		pol.ApprovalQuestion:      noulAnswerFixture(0.5),
		pol.UserRequestedQuestion: noulAnswerFixture(0.5),
		pol.FromUntrustedQuestion: noulAnswerFixture(0),
	}
}
