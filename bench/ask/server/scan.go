package server

import (
	"regexp"

	"tofu/bench/corpus"
)

var taskIntent = regexp.MustCompile(`(?i)\bstart\b.{0,20}\bserver\b|\brun\b.{0,20}\b(server|dev server)\b`)

const BobSessionsDirFromPackage = "../../../.tofu/sessions"

type Share struct {
	TotalTurns int
	InDomain   int
	Fires      int
	DomainIDs  []string
}

func Scan(dirs ...string) (Share, error) {
	var share Share
	for _, dir := range dirs {
		walked, err := corpus.WalkSessions(dir)
		if err != nil {
			return share, err
		}
		share.TotalTurns += len(walked.Turns)
		for _, t := range walked.Turns {
			if !taskIntent.MatchString(t.Task) {
				continue
			}
			share.InDomain++
			share.DomainIDs = append(share.DomainIDs, t.ID)
			scripts, err := declaredScripts(t.RecordedTurn)
			if err != nil || Decide(CandidatesByShape(scripts)) == FitAsk {
				share.Fires++
			}
		}
	}
	return share, nil
}
