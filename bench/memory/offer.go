package memory

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"regexp"

	imemory "tofu/internal/memory"
)

const (
	CorpusFile    = "testdata/sample-chain.json"
	JevAnswerFile = "testdata/jev-memory_offer@1.json"
	JevArm        = "jev memory_offer@1, shadow"
	LabelSource   = "made-up messages, labelled by the agent that wrote them, at the time it wrote them: offer means the message states something that should hold beyond the task it was typed in. no second labeller."
)

type Message struct {
	Session string `json:"session"`
	Origin  string `json:"origin"`
	Text    string `json:"text"`
	Offer   bool   `json:"offer"`
}

type JevAnswer struct {
	Standing float64 `json:"standing"`
	Corrects float64 `json:"corrects"`
	About    string  `json:"about"`
}

type Score struct {
	Arm                         string
	TruePos, FalsePos, FalseNeg int
}

func (s Score) Precision() float64 { return ratio(s.TruePos, s.TruePos+s.FalsePos) }

func (s Score) Recall() float64 { return ratio(s.TruePos, s.TruePos+s.FalseNeg) }

func ratio(part, whole int) float64 {
	if whole == 0 {
		return 0
	}
	return float64(part) / float64(whole)
}

func readJSON(file string, into any) error {
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, into)
}

func Load() ([]Message, []JevAnswer, error) {
	var corpus []Message
	if err := readJSON(CorpusFile, &corpus); err != nil {
		return nil, nil, err
	}
	var answers []JevAnswer
	if err := readJSON(JevAnswerFile, &answers); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, nil, err
	}
	return corpus, answers, nil
}

func Arms(answers []JevAnswer) map[string]func(i int, m Message) bool {
	correctionWords := regexp.MustCompile(`(?is)\bagain\b|\bi said\b|\bwhy\b.*\blol\b|\bremember\b|\byou should be\b`)
	prefixOnly := regexp.MustCompile(`(?i)^\s*remember\b`)
	sentenceOnly := regexp.MustCompile(`(?i)(?:^|[.!?\n])\s*remember\b`)
	anywhere := regexp.MustCompile(`(?i)\bremember\b`)
	arms := map[string]func(int, Message) bool{
		"trigger, in the app now":    func(_ int, m Message) bool { _, offered := imemory.OfferFor(m.Text); return offered },
		"trigger, message prefix":    func(_ int, m Message) bool { return prefixOnly.MatchString(m.Text) },
		"trigger, sentence start":    func(_ int, m Message) bool { return sentenceOnly.MatchString(m.Text) },
		"trigger, remember anywhere": func(_ int, m Message) bool { return anywhere.MatchString(m.Text) },
		"regular expression":         func(_ int, m Message) bool { return correctionWords.MatchString(m.Text) },
	}
	if len(answers) > 0 {
		arms[JevArm] = func(i int, _ Message) bool {
			return answers[i].Standing > 1-answers[i].Standing && answers[i].About != "task"
		}
	}
	return arms
}

func Measure(corpus []Message, arm string, offers func(int, Message) bool) Score {
	score := Score{Arm: arm}
	for i, m := range corpus {
		switch said := offers(i, m); {
		case said && m.Offer:
			score.TruePos++
		case said:
			score.FalsePos++
		case m.Offer:
			score.FalseNeg++
		}
	}
	return score
}
