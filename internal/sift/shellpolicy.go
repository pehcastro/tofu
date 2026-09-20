package sift

import (
	"fmt"
	"io/fs"

	"tofu/internal/judge/policy"
)

const ShellSchema = "shell_sift"

type ShellPolicy struct {
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

func LoadShellPolicy(shipped fs.FS, name string) (ShellPolicy, error) {
	pol, err := policy.LoadFS(shipped, name)
	if err != nil {
		return ShellPolicy{}, err
	}
	if pol.Schema != ShellSchema {
		return ShellPolicy{}, fmt.Errorf("%s: schema is %q and this loader reads %q, which is what keeps the gate's own policies away from it", pol.File, pol.Schema, ShellSchema)
	}
	keepAt := pol.ForeignThresholds["keep_at"]
	if keepAt <= 0 || keepAt > 1 {
		return ShellPolicy{}, fmt.Errorf("%s: keep_at is %g and a noul threshold sits above 0 and at or below 1", pol.File, keepAt)
	}
	return ShellPolicy{
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
