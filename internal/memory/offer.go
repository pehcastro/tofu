package memory

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"tofu/internal/konst"
)

const (
	answersFile = "answers.jsonl"
)

type Offer struct {
	Text  string
	Said  string
	Kind  Kind
	Scope Scope
}

func OfferFor(typed string) (Offer, bool) {
	sentenceStart := regexp.MustCompile(`(?i)(?:^|[.!?\n])\s*(?:(?:also|and|but|so|ok|okay|btw|pls|please)[\s,]+)*remember\b[\s:,;-]*(?:that\s+)?`)
	at := sentenceStart.FindStringIndex(typed)
	if at == nil {
		return Offer{}, false
	}
	text := strings.Join(strings.Fields(typed[at[1]:]), " ")
	if text == "" {
		return Offer{}, false
	}
	return Offer{Text: text, Said: typed, Kind: KindPerson, Scope: Global}, true
}

func PersonIn(statement string) (string, bool) {
	described := regexp.MustCompile(`(?i)\b(?:the\s+(?:person|user|owner|human|developer)|(?:person|user|owner)'s)\b`)
	named := regexp.MustCompile(`(?:(?i:\b(?:he|she|they))|^\s*\p{Lu}\pL*)\s+(?i:wants?|prefers?|likes?|asks?|asked|said|says|needs?|expects?|hates?|told|keeps)\b`)
	found := described.FindString(statement) + named.FindString(statement)
	return found, found != ""
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

func Trusts(globalDir string) (bool, error) {
	file, err := os.Open(filepath.Join(globalDir, answersFile))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer func() { _ = file.Close() }()
	answered, accepted := 0, 0
	for lines := bufio.NewScanner(file); answered < konst.MemoryOffersAskedFirst && lines.Scan(); answered++ {
		var one answerLine
		if err := json.Unmarshal(lines.Bytes(), &one); err != nil {
			return false, err
		}
		if one.Answer != AnswerNo {
			accepted++
		}
	}
	return answered == konst.MemoryOffersAskedFirst && accepted >= konst.MemoryAcceptedToTrustAt, nil
}
