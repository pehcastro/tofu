package sift

import (
	"fmt"
	"io/fs"

	"tofu/internal/judge/gate"
)

type foreignRule struct {
	Name             string
	Schema           string
	RuleVersion      int
	Questions        string
	QuestionsVersion int
	KeepAt           float64
	Mode             Mode
	ModeDeclared     bool
	SampleFloor      int
	Notes            string
	File             string
}

func loadForeignRule(shipped fs.FS, ref, schema string) (foreignRule, error) {
	r, err := gate.LoadFS(shipped, ref)
	if err != nil {
		return foreignRule{}, err
	}
	if r.Schema != schema {
		return foreignRule{}, fmt.Errorf("%s: schema is %q and this loader reads %q, which is what keeps the gate's own rules away from it", r.File, r.Schema, schema)
	}
	keepAt := r.ForeignThresholds["keep_at"]
	if keepAt <= 0 || keepAt > 1 {
		return foreignRule{}, fmt.Errorf("%s: keep_at is %g and a noul threshold sits above 0 and at or below 1", r.File, keepAt)
	}
	return foreignRule{
		Name:             r.Name,
		Schema:           r.Schema,
		RuleVersion:      r.RuleVersion,
		Questions:        r.Questions,
		QuestionsVersion: r.QuestionsVersion,
		KeepAt:           keepAt,
		Mode:             Mode(r.Mode),
		ModeDeclared:     r.ModeDeclared,
		SampleFloor:      r.SampleFloor,
		Notes:            r.Notes,
		File:             r.File,
	}, nil
}
