package wiring

import (
	"tofu/internal/judge/ledger"
	"tofu/internal/judge/questions"
	"tofu/internal/sys"
	"tofu/internal/tools/web/browser/loop"
	"tofu/library/questions"
)

const browserStepPoint = "browser_step@1"

func BrowserJudge(dir string) (jevloop.Jev, error) {
	layers, err := question.Layers(questions.Files(), dir)
	if err != nil {
		return jevloop.Jev{}, err
	}
	set, _, err := question.Resolve(browserStepPoint, layers)
	if err != nil {
		return jevloop.Jev{}, err
	}
	client, err := NewJevClient()
	if err != nil {
		return jevloop.Jev{}, err
	}
	logDir, err := sys.LogDir()
	if err != nil {
		return jevloop.Jev{}, err
	}
	return jevloop.Jev{Client: client, Set: set, Ledger: ledger.NewWriter(logDir)}, nil
}
