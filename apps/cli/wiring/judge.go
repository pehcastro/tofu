package wiring

import (
	"fmt"
	"strings"

	"tofu/internal/judge/gate"
	"tofu/internal/judge/ledger"
	question "tofu/internal/judge/questions"
	"tofu/internal/judge/state"
	"tofu/internal/models/classifiers/core"
	"tofu/internal/sys"
	shipped "tofu/library"
	"tofu/library/questions"
)

type Battery struct {
	Questions        []core.Question
	Kinds            map[string]question.Kind
	SetName          string
	QuestionsVersion int
	Rule             *gate.Rule
	Mode             gate.Mode
	ModeReason       string
}

func ResolveRuleMode(r gate.Rule) (gate.Resolution, error) {
	calibDir, err := sys.CalibrationDir()
	if err != nil {
		return gate.Resolution{}, err
	}
	lookup := lookupLock(gate.LockPath(calibDir, r))
	ledgerDir, err := sys.LogDir()
	if err != nil {
		return gate.Resolution{}, err
	}
	current, err := CurrentBuild(ledgerDir, r.Questions)
	if err != nil {
		return gate.Resolution{}, err
	}
	return gate.Resolve(r, lookup, current), nil
}

func lookupLock(path string) gate.LockLookup {
	present, err := sys.Exists(path)
	if err != nil {
		return gate.LockLookup{Present: true, Err: err}
	}
	if !present {
		return gate.LockLookup{}
	}
	lock, err := gate.LoadLock(path)
	return gate.LockLookup{Present: true, Lock: lock, Err: err}
}

func CurrentBuild(ledgerDir, point string) (gate.Current, error) {
	row, found, err := ledger.NewReader(ledgerDir).Newest(ledger.Filter{Point: point})
	if err != nil || !found {
		return gate.Current{}, err
	}
	return gate.Current{Build: row.Build, QuestionsVersion: row.Version, Known: true}, nil
}

func ResolveLibrary(ref, dir string) (Battery, error) {
	layers, err := question.Layers(questions.Files(), dir)
	if err != nil {
		return Battery{}, err
	}
	set, _, err := question.Resolve(ref, layers)
	if err != nil {
		return Battery{}, err
	}
	jevQuestions := make([]core.Question, 0, len(set.Questions))
	kinds := make(map[string]question.Kind, len(set.Questions))
	for _, q := range set.Questions {
		jevQuestions = append(jevQuestions, q.ToJev())
		kinds[q.Name] = q.Kind
	}
	return Battery{Questions: jevQuestions, Kinds: kinds, SetName: set.Name, QuestionsVersion: set.QuestionsVersion}, nil
}

func ResolveRule(ref string, set Battery) (gate.Rule, error) {
	if !strings.ContainsRune(ref, '@') {
		return gate.Rule{}, fmt.Errorf("the rule %q names no version; a rule is named name@version, so it never resolves silently to the wrong one", ref)
	}
	r, _, err := LoadRulePoint(ref, "")
	if err != nil {
		return gate.Rule{}, err
	}
	if r.Questions != set.SetName || r.QuestionsVersion != set.QuestionsVersion {
		return gate.Rule{}, fmt.Errorf("rule %s names the question set %s@%d and the battery resolved %s@%d",
			ref, r.Questions, r.QuestionsVersion, set.SetName, set.QuestionsVersion)
	}
	return r, nil
}

func LoadRulePoint(ref, dir string) (gate.Rule, gate.Origin, error) {
	if ref == state.StopCheckRuleRef {
		return state.StopCheckRule()
	}
	layers, err := question.Layers(questions.Files(), dir)
	if err != nil {
		return gate.Rule{}, "", err
	}
	set, _, err := question.Resolve(ref, layers)
	if err != nil {
		return gate.Rule{}, "", err
	}
	return gate.LoadPoint(shipped.Files(), ref, set, dir)
}
