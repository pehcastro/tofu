package cost

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"tofu/bench/corpus"
	"tofu/internal/judge/question"
)

const leakWindow = 24

var labelWord = regexp.MustCompile(`(?i)\b(` + regexp.QuoteMeta(string(corpus.Block)) + `|` + regexp.QuoteMeta(string(corpus.Proceed)) + `)\b`)

func leaksIn(set question.Set, records []corpus.Record) []string {
	var found []string
	for _, q := range set.Questions {
		text := strings.Join(q.Words(), "\n")
		if label := labelWord.FindString(text); label != "" {
			found = append(found, fmt.Sprintf("%s names the case label %q", q.Name, label))
		}
		for _, record := range records {
			if strings.Contains(text, record.ID) {
				found = append(found, fmt.Sprintf("%s names the case %s", q.Name, record.ID))
			}
			for _, window := range windowsOf(record.Command(), leakWindow) {
				if strings.Contains(text, window) {
					found = append(found, fmt.Sprintf("%s carries %q out of case %s", q.Name, window, record.ID))
					break
				}
			}
		}
	}
	return found
}

func plantedSet(instructions, whenTrue string) question.Set {
	return question.Set{Name: "planted", Questions: []question.Question{{
		Name:         "planted",
		Kind:         question.KindNoul,
		Instructions: instructions,
		True:         question.Criteria{Kind: question.CriteriaText, Text: whenTrue},
		False:        question.Criteria{Kind: question.CriteriaText, Text: "No."},
	}}}
}

func TestAPlantedQuestionThatNamesItsOwnCaseLabelIsALeak(t *testing.T) {
	_, heldOut := halves(t)
	var aCommand string
	for _, record := range heldOut {
		if command := record.Command(); len(command) >= leakWindow {
			aCommand = command[:leakWindow]
			break
		}
	}
	for _, planted := range []struct {
		name string
		set  question.Set
	}{
		{"the block label in the instructions", plantedSet("Answer true when this case is labelled block.", "Yes.")},
		{"the proceed label in a criterion", plantedSet("Is the call safe?", "The label is proceed.")},
		{"the label in another case", plantedSet("Is this a BLOCK case?", "Yes.")},
		{"a held-out case id", plantedSet("Is this tx-113 or anything like it?", "Yes.")},
		{"a run of a held-out command", plantedSet("Does the call look like "+aCommand+"?", "Yes.")},
	} {
		if found := leaksIn(planted.set, heldOut); len(found) == 0 {
			t.Errorf("%s: the leak check found nothing", planted.name)
		}
	}
	if found := leaksIn(plantedSet("Would this call be blocked or unblocked by a reviewer who proceeds with care?", "Yes."), heldOut); len(found) != 0 {
		t.Errorf("a word that only contains a label is not the label: %v", found)
	}
}

func TestNoQuestionAJevArmAsksNamesACaseLabel(t *testing.T) {
	_, heldOut := halves(t)
	shipped, err := shippedQuestions()
	if err != nil {
		t.Fatalf("the shipped set: %v", err)
	}
	_, hazards, err := loadHazards()
	if err != nil {
		t.Fatalf("the decomposed set: %v", err)
	}
	for _, set := range []question.Set{shipped, hazards} {
		for _, leak := range leaksIn(set, heldOut) {
			t.Errorf("%s: %s", set.Name, leak)
		}
	}
}
