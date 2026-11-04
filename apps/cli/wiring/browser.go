package wiring

import (
	"tofu/internal/judge/ledger"
	question "tofu/internal/judge/questions"
	"tofu/internal/sys"
	"tofu/internal/tools/web/browser/loop"
	"tofu/library/questions"
)

const browserStepPoint = "browser_step@1"

func BrowserJudge(dir string) (loop.Jev, error) {
	layers, err := question.Layers(questions.Files(), dir)
	if err != nil {
		return loop.Jev{}, err
	}
	set, _, err := question.Resolve(browserStepPoint, layers)
	if err != nil {
		return loop.Jev{}, err
	}
	client, err := NewJevClient()
	if err != nil {
		return loop.Jev{}, err
	}
	logDir, err := sys.LogDir()
	if err != nil {
		return loop.Jev{}, err
	}
	return loop.Jev{Client: client, Set: set, Ledger: ledger.NewWriter(logDir)}, nil
}
