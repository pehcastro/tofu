package sift

import (
	"fmt"
	"io/fs"

	"tofu/internal/judge/policy"
)

type foreignPolicy struct {
	Name             string
	Schema           string
	PolicyVersion    int
	Questions        string
	QuestionsVersion int
	KeepAt           float64
	Mode             Mode
	ModeDeclared     bool
	SampleFloor      int
	Notes            string
	File             string
}

func loadForeignPolicy(shipped fs.FS, name, schema string) (foreignPolicy, error) {
	pol, err := policy.LoadFS(shipped, name)
	if err != nil {
		return foreignPolicy{}, err
	}
	if pol.Schema != schema {
		return foreignPolicy{}, fmt.Errorf("%s: schema is %q and this loader reads %q, which is what keeps the gate's own policies away from it", pol.File, pol.Schema, schema)
	}
	keepAt := pol.ForeignThresholds["keep_at"]
	if keepAt <= 0 || keepAt > 1 {
		return foreignPolicy{}, fmt.Errorf("%s: keep_at is %g and a noul threshold sits above 0 and at or below 1", pol.File, keepAt)
	}
	return foreignPolicy{
		Name:             pol.Name,
		Schema:           pol.Schema,
		PolicyVersion:    pol.PolicyVersion,
		Questions:        pol.Questions,
		QuestionsVersion: pol.QuestionsVersion,
		KeepAt:           keepAt,
		Mode:             Mode(pol.Mode),
		ModeDeclared:     pol.ModeDeclared,
		SampleFloor:      pol.SampleFloor,
		Notes:            pol.Notes,
		File:             pol.File,
	}, nil
}
