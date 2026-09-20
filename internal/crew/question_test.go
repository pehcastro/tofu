package crew

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestTheFourRealDeferredQuestionsRoundTripByteForByte(t *testing.T) {
	asked := map[string]string{
		"BOJI-006": "cmd/boji/main.go",
		"BOJI-011": "bench/cost/labels.go",
		"BOJI-015": "internal/konst/konst.go",
		"BOJI-020": "internal/judge/question/load.go",
	}
	for ticket, where := range asked {
		t.Run(ticket, func(t *testing.T) {
			fixture := filepath.Join("testdata", "questions", ticket+".block")
			written, err := os.ReadFile(fixture)
			if err != nil {
				t.Fatal(err)
			}
			question, err := ParseBlock(string(written))
			if err != nil {
				t.Fatalf("%s did not parse: %v", fixture, err)
			}
			if question.Ticket != ticket || question.Kind != Deferred || question.Where != where {
				t.Fatalf("%s parsed to %+v", fixture, question)
			}
			if question.Ask == "" || question.Default == "" || question.Answer != "" {
				t.Fatalf("%s lost a field: %+v", fixture, question)
			}
			if question.Block() != string(written) {
				t.Fatalf("the round trip is not byte identical:\nwant %q\ngot  %q", string(written), question.Block())
			}
		})
	}
}

func TestEveryFieldSurvivesTheRoundTripIncludingAnAnswer(t *testing.T) {
	answered := Question{
		Ticket:  "BOJI-210",
		Kind:    Grant,
		Where:   "internal/konst/konst.go",
		Ask:     "the deletion breaks a test file this ticket does not hold",
		Default: "refused the write and left the path untouched",
		Answer:  "take it, and the test file with it",
	}
	parsed, err := ParseBlock(answered.Block())
	if err != nil {
		t.Fatal(err)
	}
	if parsed != answered {
		t.Fatalf("the round trip changed the row:\nwant %+v\ngot  %+v", answered, parsed)
	}
}

func TestAKindThisBuildDoesNotKnowFailsRatherThanBeingCarried(t *testing.T) {
	_, err := ParseBlock("### question · escalation · BOJI-099\n\nwhere: x\nask: y\ndefault: z\nanswer:\n")
	var unknown UnknownKindError
	if !errors.As(err, &unknown) {
		t.Fatalf("parsing an unknown kind gave %v, want an UnknownKindError", err)
	}
	if unknown.Name != "escalation" {
		t.Fatalf("the error does not name the kind it refused: %q", unknown.Name)
	}
	if _, err := ParseKind("escalation"); !errors.As(err, &unknown) {
		t.Fatalf("ParseKind gave %v, want an UnknownKindError", err)
	}
}

func TestAMalformedBlockIsRefusedRatherThanPartlyRead(t *testing.T) {
	malformed := map[string]string{
		"no_heading":            "where: x\nask: y\ndefault: z\nanswer:\n",
		"heading_has_no_ticket": "### question · deferred\n\nwhere: x\nask: y\ndefault: z\nanswer:\n",
		"fields_out_of_order":   "### question · deferred · BOJI-006\n\nask: y\nwhere: x\ndefault: z\nanswer:\n",
		"answer_missing":        "### question · deferred · BOJI-006\n\nwhere: x\nask: y\ndefault: z\n",
		"a_line_after_answer":   "### question · deferred · BOJI-006\n\nwhere: x\nask: y\ndefault: z\nanswer:\nand one more\n",
	}
	for name, block := range malformed {
		t.Run(name, func(t *testing.T) {
			question, err := ParseBlock(block)
			var target MalformedBlockError
			if !errors.As(err, &target) {
				t.Fatalf("got %+v, %v; want a MalformedBlockError", question, err)
			}
		})
	}
}

func TestEveryKindHasANameAndAnUnknownOneFails(t *testing.T) {
	named := map[Kind]string{Blocking: "blocking", Assumption: "assumption", Deferred: "deferred", Grant: "grant"}
	if len(named) != int(Grant)+1 {
		t.Fatalf("%d kinds are named and the enum runs to %d", len(named), int(Grant))
	}
	for kind, want := range named {
		if kind.String() != want {
			t.Fatalf("kind %d reads %q, want %q", int(kind), kind.String(), want)
		}
	}
	defer func() {
		recovered := recover()
		if message, isText := recovered.(string); !isText || message != "crew: unknown question kind 7" {
			t.Fatalf("rendering an unknown kind gave %v, want a panic naming it", recovered)
		}
	}()
	t.Log(Kind(7).String())
	t.Fatal("an unknown kind rendered instead of failing")
}
