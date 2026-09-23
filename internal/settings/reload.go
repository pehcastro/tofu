package settings

import (
	"tofu/internal/rule"
	"tofu/internal/sys"
)

type ReloadResult struct {
	Rules int
}

func Reload(shipped []rule.Rule, projectRuleDir string) (ReloadResult, error) {
	isDir, err := sys.IsDir(projectRuleDir)
	if err != nil {
		return ReloadResult{}, err
	}
	var project []rule.Rule
	if isDir {
		if project, err = rule.LoadDir(projectRuleDir); err != nil {
			return ReloadResult{}, err
		}
	}
	return ReloadResult{Rules: len(rule.Layer(shipped, project))}, nil
}
