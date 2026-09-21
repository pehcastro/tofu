package testquality

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"tofu/internal/rule"
)

const qaSkillSource = ".local/sources/qa-skills/"

func QARuleFaults(rules fs.FS, root string) ([]string, error) {
	loaded, err := rule.LoadFS(rules, root)
	if err != nil {
		return nil, err
	}
	if len(loaded) == 0 {
		return nil, errors.New("the rules directory ships nothing")
	}
	var faults []string
	for _, one := range loaded {
		if one.Kind != rule.KindMeasured && one.Source == "" {
			continue
		}
		if !strings.HasPrefix(one.Source, qaSkillSource) {
			faults = append(faults, fmt.Sprintf("%s cites source %q, not a skill under %s", one.ID, one.Source, qaSkillSource))
		}
		if one.Evidence == "" {
			faults = append(faults, fmt.Sprintf("%s carries no evidence", one.ID))
		}
	}
	return faults, nil
}
