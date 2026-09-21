package crew

import (
	"fmt"
	"io/fs"

	"tofu/internal/judge/gate"
)

const AskSchema = "ask"

type AskRule struct {
	Name             string
	Schema           string
	RuleVersion      int
	Questions        string
	QuestionsVersion int
	DeterminedLowAt  float64
	Mode             gate.Mode
	ModeDeclared     bool
	SampleFloor      int
	Notes            string
	File             string
}

func LoadAskRule(shipped fs.FS, ref string) (AskRule, error) {
	r, err := gate.LoadFS(shipped, ref)
	if err != nil {
		return AskRule{}, err
	}
	if r.Schema != AskSchema {
		return AskRule{}, fmt.Errorf("%s: schema is %q and this loader reads %q, which is what keeps the gate's own rules away from it", r.File, r.Schema, AskSchema)
	}
	determinedLowAt := r.ForeignThresholds["determined_low_at"]
	if determinedLowAt <= 0 || determinedLowAt > 1 {
		return AskRule{}, fmt.Errorf("%s: determined_low_at is %g and a noul threshold sits above 0 and at or below 1", r.File, determinedLowAt)
	}
	return AskRule{
		Name:             r.Name,
		Schema:           r.Schema,
		RuleVersion:      r.RuleVersion,
		Questions:        r.Questions,
		QuestionsVersion: r.QuestionsVersion,
		DeterminedLowAt:  determinedLowAt,
		Mode:             r.Mode,
		ModeDeclared:     r.ModeDeclared,
		SampleFloor:      r.SampleFloor,
		Notes:            r.Notes,
		File:             r.File,
	}, nil
}
