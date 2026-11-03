package judge

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"tofu/internal/judge/gate"
	"tofu/internal/models/classifiers/core"
)

type calibrationFake struct {
	risk, rerunRisk map[string]float64
	rerunBuild      string
	calls           int
}

func (f *calibrationFake) ask(_ context.Context, c calibrationCase) (askedCase, error) {
	f.calls++
	risk, build := f.risk[c.ID], "jev-test"
	if f.calls > len(f.risk) {
		if rerun, ok := f.rerunRisk[c.ID]; ok {
			risk = rerun
		}
		if f.rerunBuild != "" {
			build = f.rerunBuild
		}
	}
	return askedCase{Build: build, Answers: map[string]jev.Answer{
		"risk":           {Kind: jev.QuestionScore, Score: risk, Probabilities: map[string]float64{strconv.FormatFloat(risk, 'g', -1, 64): 1}},
		"approval":       {Kind: jev.QuestionNoul, Noul: 0.5},
		"user_requested": {Kind: jev.QuestionNoul},
		"from_untrusted": {Kind: jev.QuestionNoul},
	}}, nil
}

func calibrationHalf(half string, blocks int, sources ...string) []calibrationCase {
	var cases []calibrationCase
	for i := range blocks + 20 {
		c := calibrationCase{ID: half + "-p-" + strconv.Itoa(i), Label: caseProceed, Origin: sources[i%len(sources)], State: json.RawMessage(`{}`)}
		if i < blocks {
			c.ID, c.Label = half+"-b-"+strconv.Itoa(i), caseBlock
		}
		cases = append(cases, c)
	}
	return cases
}

func separableRisk(corpus calibrationCorpus, blockRisk, proceedRisk float64) map[string]float64 {
	risk := map[string]float64{}
	for _, c := range append(slices.Clone(corpus.Fit), corpus.Verify...) {
		risk[c.ID] = proceedRisk
		if c.Label == caseBlock {
			risk[c.ID] = blockRisk
		}
	}
	return risk
}

func calibrationRule(t *testing.T) gate.Rule {
	t.Helper()
	rule, _, err := LoadRulePoint(runGatePoint, "")
	if err != nil {
		t.Fatal(err)
	}
	return rule
}

func TestCalibrateRefusesEveryCorpusItCannotStandBehind(t *testing.T) {
	rule := calibrationRule(t)
	balanced := calibrationCorpus{Fit: calibrationHalf("fit", 6, "a", "b"), Verify: calibrationHalf("verify", 6, "a", "b")}
	separable := separableRisk(balanced, 2.0, 0.5)
	leaky := maps.Clone(separable)
	for i := range 5 {
		leaky["verify-p-"+strconv.Itoa(6+i)] = 2.0
	}
	blocksForgotten := map[string]float64{}
	for id := range separable {
		if strings.Contains(id, "-b-") {
			blocksForgotten[id] = 0.5
		}
	}
	for _, tc := range []struct {
		name   string
		corpus calibrationCorpus
		fake   calibrationFake
		says   string
		calls  int
	}{
		{"a half with too few of the rarer label", calibrationCorpus{Fit: calibrationHalf("fit", 4, "a", "b"), Verify: balanced.Verify}, calibrationFake{risk: separable}, "rarer label", 0},
		{"every case from one source", calibrationCorpus{Fit: calibrationHalf("fit", 6, "a"), Verify: calibrationHalf("verify", 6, "a")}, calibrationFake{risk: separable}, "one source", 0},
		{"a cut no better than switching the gate off", balanced, calibrationFake{risk: separableRisk(balanced, 0.5, 0.5)}, "switching the gate off", 52},
		{"a fit the held-out half does not hold", balanced, calibrationFake{risk: leaky}, "held-out half", 52},
		{"a rerun that agrees less than the fit", balanced, calibrationFake{risk: separable, rerunRisk: blocksForgotten}, "rerun", 104},
		{"a rerun answered by another build", balanced, calibrationFake{risk: separable, rerunBuild: "jev-other"}, "build", 104},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := calibrate(t.Context(), rule, tc.corpus, tc.fake.ask, time.Now)
			var refused calibrationRefusal
			if !errors.As(err, &refused) || !strings.Contains(err.Error(), tc.says) {
				t.Fatalf("calibrate returned %v, want a refusal saying %q", err, tc.says)
			}
			if tc.fake.calls != tc.calls {
				t.Errorf("calibrate asked %d times before refusing, want %d", tc.fake.calls, tc.calls)
			}
		})
	}
}

func TestACalibratedLockReadsBackAndLiftsADeclaredEnforcedPointOutOfShadow(t *testing.T) {
	rule := calibrationRule(t)
	corpus := calibrationCorpus{Fit: calibrationHalf("fit", 6, "a", "b"), Verify: calibrationHalf("verify", 6, "a", "b"), Digest: "sha256:test"}
	fake := calibrationFake{risk: separableRisk(corpus, 2.0, 0.5)}
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	result, err := calibrate(t.Context(), rule, corpus, fake.ask, func() time.Time { return at })
	if err != nil {
		t.Fatal(err)
	}
	if fake.calls != 104 {
		t.Errorf("calibrate asked %d times, want every one of 52 cases asked twice", fake.calls)
	}
	path := filepath.Join(t.TempDir(), runGatePoint+".lock")
	if err := gate.WriteLock(path, result.Lock); err != nil {
		t.Fatal(err)
	}
	read, err := gate.LoadLock(path)
	if err != nil {
		t.Fatal(err)
	}
	want := result.Lock
	want.File = path
	if read != want {
		t.Fatalf("the lock read back as %+v, want %+v", read, want)
	}
	if read.Map != gate.MapIdentity || read.RerunAgreement != 1 || read.AgreementVerify != 1 || read.CalibrationError != 0 || read.Corpus != "sha256:test" || read.NFit != 26 || read.NVerify != 26 {
		t.Errorf("the lock carries %+v, want the identity map, full agreement, no calibration error, the corpus digest and 26 per half", read)
	}
	if cut := read.Thresholds.RiskAskAt; cut <= 0.5 || cut >= 2.0 {
		t.Errorf("the fitted risk_ask_at is %g, want a cut between the proceed risk 0.5 and the block risk 2.0", cut)
	}
	current := gate.Current{Build: "jev-test", QuestionsVersion: rule.QuestionsVersion, Known: true}
	rule.Mode, rule.ModeDeclared, rule.SampleFloor = gate.ModeShadow, true, 300
	if held := gate.Resolve(rule, gate.LockLookup{Present: true, Lock: read}, current); held.Mode != gate.ModeShadow || !strings.Contains(held.Reason, "sample size 26 is below the floor 300, see tofu docs calibration") || !strings.Contains(held.Reason, "risk_ask_at") || strings.Contains(held.Reason, ".local") {
		t.Errorf("a declared shadow point with a lock under its floor resolved %s (%s), want shadow naming the lock and the floor", held.Mode, held.Reason)
	}
	rule.Mode, rule.SampleFloor = gate.ModeEnforced, 26
	resolved := gate.Resolve(rule, gate.LockLookup{Present: true, Lock: read}, current)
	if resolved.Mode != gate.ModeEnforced || !resolved.Pinned {
		t.Errorf("the point resolved %s pinned %v (%s), want enforced on the lock's thresholds", resolved.Mode, resolved.Pinned, resolved.Reason)
	}
}
