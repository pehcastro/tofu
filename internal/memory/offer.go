package memory

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"tofu/internal/konst"
)

const (
	rememberWord = "remember"
	thatWord     = "that "
	answersFile  = "answers.jsonl"
)

type Offer struct {
	Text  string
	Said  string
	Kind  Kind
	Scope Scope
}

func OfferFor(typed string) (Offer, bool) {
	trimmed := strings.TrimSpace(typed)
	if len(trimmed) < len(rememberWord) || !strings.EqualFold(trimmed[:len(rememberWord)], rememberWord) {
		return Offer{}, false
	}
	rest := trimmed[len(rememberWord):]
	if rest != "" && unicode.IsLetter([]rune(rest)[0]) {
		return Offer{}, false
	}
	rest = strings.TrimLeft(rest, " \t\r\n:,;-")
	if len(rest) >= len(thatWord) && strings.EqualFold(rest[:len(thatWord)], thatWord) {
		rest = rest[len(thatWord):]
	}
	text := strings.Join(strings.Fields(rest), " ")
	if text == "" {
		return Offer{}, false
	}
	return Offer{Text: text, Said: typed, Kind: KindPerson, Scope: Global}, true
}

type Answer string

const (
	AnswerGlobal  Answer = "global"
	AnswerProject Answer = "project"
	AnswerNo      Answer = "no"
)

type answerLine struct {
	At     time.Time `json:"at"`
	Answer Answer    `json:"answer"`
}

func Record(globalDir string, answer Answer) error {
	line, err := json.Marshal(answerLine{At: time.Now(), Answer: answer})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(globalDir, answersFile), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, err = file.Write(append(line, '\n'))
	return errors.Join(err, file.Close())
}

func AsksFirst(globalDir string) (bool, error) {
	file, err := os.Open(filepath.Join(globalDir, answersFile))
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return true, err
	}
	defer func() { _ = file.Close() }()
	answered, accepted := 0, 0
	for lines := bufio.NewScanner(file); answered < konst.MemoryOffersAskedFirst && lines.Scan(); answered++ {
		var one answerLine
		if err := json.Unmarshal(lines.Bytes(), &one); err != nil {
			return true, err
		}
		if one.Answer != AnswerNo {
			accepted++
		}
	}
	return answered < konst.MemoryOffersAskedFirst || accepted <= konst.MemoryAcceptedToTrustAt, nil
}
