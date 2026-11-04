package judge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/apps/cli/output"
	cli "tofu/apps/cli/page"
	"tofu/apps/cli/wiring"
	"tofu/internal/judge/gate"
	"tofu/internal/konst"
	"tofu/internal/models/classifiers/core"
	"tofu/internal/sys"
)

const CalibrateUsage = "tofu calibrate <rule@version> --cases <cases.jsonl> --split <split.json> [--dry-run] [--json]"

type caseLabel string

const (
	caseProceed caseLabel = "proceed"
	caseBlock   caseLabel = "block"
)

type calibrationCase struct {
	ID     string          `json:"id"`
	Label  caseLabel       `json:"label"`
	Origin string          `json:"origin"`
	State  json.RawMessage `json:"state"`
}

type calibrationCorpus struct {
	Fit, Verify []calibrationCase
	Digest      string
}

type askedCase struct {
	Answers map[string]core.Answer
	Build   string
}

type caseAsker func(context.Context, calibrationCase) (askedCase, error)

type calibrationRefusal string

func (r calibrationRefusal) Error() string { return string(r) }

func refuse(format string, args ...any) calibrationRefusal {
	return calibrationRefusal(fmt.Sprintf(format, args...))
}

type cutScore struct {
	cases, correct, blocks, caught, falseBlock int
}

func (s cutScore) agreement() float64 { return float64(s.correct) / float64(s.cases) }

func (s cutScore) alwaysProceed() float64 { return float64(s.cases-s.blocks) / float64(s.cases) }

type calibration struct {
	Lock          gate.Lock
	AlwaysProceed float64
	SignP         float64
}

type calibrateReport struct {
	Point               string  `json:"point"`
	Lock                string  `json:"lock,omitempty"`
	Fit                 int     `json:"fit"`
	Verify              int     `json:"verify"`
	Calls               int     `json:"calls"`
	Build               string  `json:"build,omitempty"`
	RiskAskAt           float64 `json:"risk_ask_at,omitempty"`
	AgreementFit        float64 `json:"agreement_fit,omitempty"`
	AgreementVerify     float64 `json:"agreement_verify,omitempty"`
	AlwaysProceedVerify float64 `json:"always_proceed_verify,omitempty"`
	SignP               float64 `json:"sign_p,omitempty"`
	RerunAgreement      float64 `json:"rerun_agreement,omitempty"`
	CalibrationError    float64 `json:"calibration_error,omitempty"`
}

func CalibrateVerb(args []string, out, errOut io.Writer) int {
	o := output.Verb{Name: "calibrate", UsageLine: CalibrateUsage, AsJSON: output.JSONAsked(args), Out: out, ErrOut: errOut}
	ref, casesPath, splitPath, dryRun, err := parseCalibrateArgs(output.WithoutJSON(args))
	if err != nil {
		return o.Usage(err)
	}
	set, err := wiring.ResolveLibrary(ref, "")
	if err != nil {
		return o.Fail(err)
	}
	rule, err := wiring.ResolveRule(ref, set)
	if err != nil {
		return o.Fail(err)
	}
	corpus, err := readCalibrationCorpus(casesPath, splitPath)
	if err != nil {
		return o.Fail(err)
	}
	report := calibrateReport{Point: ref, Fit: len(corpus.Fit), Verify: len(corpus.Verify)}
	if dryRun {
		if err := corpus.unfit(); err != nil {
			return o.Fail(err)
		}
		report.Calls = 2 * (report.Fit + report.Verify)
		return o.Done(true, report, func(page cli.Page) []string {
			return []string{page.Glyph(cli.Done) + " " + ref + " would ask " + strconv.Itoa(report.Calls) + " jev calls" + cli.Gap +
				page.Label(fmt.Sprintf("%d fit and %d held-out cases, each asked twice, nothing asked yet", report.Fit, report.Verify))}
		})
	}
	resolved, err := wiring.ResolveRuleMode(rule)
	if err != nil {
		return o.Fail(err)
	}
	set.Rule, set.Mode, set.ModeReason = &resolved.Rule, resolved.Mode, resolved.Reason
	client, err := wiring.NewJevClient()
	if err != nil {
		return wiring.Failed(o, err)
	}
	ask := func(ctx context.Context, c calibrationCase) (askedCase, error) {
		report.Calls++
		outcome, _, err := AskFresh(ctx, client, core.Request{State: c.State, Questions: set.Questions}, set)
		return askedCase{Answers: outcome.Decision.Answers, Build: outcome.Decision.Build}, err
	}
	result, err := calibrate(context.Background(), rule, corpus, ask, time.Now)
	var refused calibrationRefusal
	if errors.As(err, &refused) {
		return o.Fail(fmt.Errorf("%s; refused after %d jev calls, no lock written", refused, report.Calls))
	}
	if err != nil {
		return wiring.Failed(o, err)
	}
	dir, err := sys.CalibrationDir()
	if err != nil {
		return o.Fail(err)
	}
	report.Lock = gate.LockPath(dir, rule)
	if err := gate.WriteLock(report.Lock, result.Lock); err != nil {
		return o.Fail(err)
	}
	if _, err := gate.LoadLock(report.Lock); err != nil {
		return o.Fail(fmt.Errorf("the lock just written does not read back: %w", err))
	}
	lock := result.Lock
	report.Build, report.RiskAskAt, report.SignP = lock.Build, lock.Thresholds.RiskAskAt, result.SignP
	report.AgreementFit, report.AgreementVerify, report.AlwaysProceedVerify, report.RerunAgreement = lock.AgreementFit, lock.AgreementVerify, result.AlwaysProceed, lock.RerunAgreement
	report.CalibrationError = lock.CalibrationError
	return o.Done(true, report, func(page cli.Page) []string {
		return append([]string{page.Receipt(cli.Done, ref+" calibrated", report.Lock)}, page.Facts([]cli.Fact{
			{Label: "cases", Text: fmt.Sprintf("%d fit, %d held out, %d jev calls on %s", report.Fit, report.Verify, report.Calls, report.Build)},
			{Label: "risk_ask_at", Text: fmt.Sprintf("%g, fitted", report.RiskAskAt)},
			{Label: "agreement", Text: fmt.Sprintf("%.4f fit, %.4f held out, %.4f with the gate off", report.AgreementFit, report.AgreementVerify, report.AlwaysProceedVerify)},
			{Label: "against off", Text: fmt.Sprintf("p %.4f", report.SignP)},
			{Label: "rerun", Text: fmt.Sprintf("%.4f of verdicts the same on a second ask", report.RerunAgreement)},
			{Label: "calibration", Text: fmt.Sprintf("error %.4f on the held-out half, %d bins", report.CalibrationError, konst.CalibrateErrorBins)},
		})...)
	})
}

func parseCalibrateArgs(args []string) (ref, casesPath, splitPath string, dryRun bool, err error) {
	paths := map[string]*string{"--cases": &casesPath, "--split": &splitPath}
	for i := 0; i < len(args); i++ {
		switch arg := args[i]; {
		case arg == "--dry-run":
			dryRun = true
		case paths[arg] != nil:
			i++
			if i >= len(args) {
				return "", "", "", false, errors.New(arg + " needs a path")
			}
			*paths[arg] = args[i]
		case strings.HasPrefix(arg, "-") || ref != "":
			return "", "", "", false, fmt.Errorf("unknown argument %q", arg)
		default:
			ref = arg
		}
	}
	if ref == "" || casesPath == "" || splitPath == "" {
		return "", "", "", false, errors.New("a rule@version, --cases and --split")
	}
	return ref, casesPath, splitPath, dryRun, nil
}

func readCalibrationCorpus(casesPath, splitPath string) (calibrationCorpus, error) {
	casesRaw, err := sys.ReadFile(casesPath)
	if err != nil {
		return calibrationCorpus{}, err
	}
	splitRaw, err := sys.ReadFile(splitPath)
	if err != nil {
		return calibrationCorpus{}, err
	}
	var split struct {
		Heldout []string `json:"heldout"`
	}
	if err := json.Unmarshal(splitRaw, &split); err != nil {
		return calibrationCorpus{}, fmt.Errorf("%s: %w", splitPath, err)
	}
	heldout := make(map[string]bool, len(split.Heldout))
	for _, id := range split.Heldout {
		heldout[id] = true
	}
	seen := map[string]bool{}
	var corpus calibrationCorpus
	for i, line := range strings.Split(string(casesRaw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		at := fmt.Sprintf("%s:%d", casesPath, i+1)
		var c calibrationCase
		switch err := json.Unmarshal([]byte(line), &c); {
		case err != nil:
			return calibrationCorpus{}, fmt.Errorf("%s: %w", at, err)
		case c.ID == "" || seen[c.ID]:
			return calibrationCorpus{}, fmt.Errorf("%s: the case id %q is empty or repeated", at, c.ID)
		case c.Label != caseProceed && c.Label != caseBlock:
			return calibrationCorpus{}, fmt.Errorf("%s: label is %q or %q, found %q", at, caseProceed, caseBlock, c.Label)
		case c.Origin == "":
			return calibrationCorpus{}, fmt.Errorf("%s: case %s names no origin", at, c.ID)
		case len(c.State) == 0 || string(c.State) == "null":
			return calibrationCorpus{}, fmt.Errorf("%s: case %s carries no state", at, c.ID)
		}
		seen[c.ID] = true
		if heldout[c.ID] {
			corpus.Verify = append(corpus.Verify, c)
			continue
		}
		corpus.Fit = append(corpus.Fit, c)
	}
	for _, id := range split.Heldout {
		if !seen[id] {
			return calibrationCorpus{}, fmt.Errorf("%s holds out %s, which %s does not carry", splitPath, id, casesPath)
		}
	}
	digest := sha256.Sum256(append(casesRaw, splitRaw...))
	corpus.Digest = "sha256:" + hex.EncodeToString(digest[:])
	return corpus, nil
}

func (c calibrationCorpus) unfit() error {
	need := int(math.Floor(math.Log2(1/konst.CalibrateSignificance))) + 1
	origins := map[string]bool{}
	for _, half := range []struct {
		name  string
		cases []calibrationCase
	}{{"fit", c.Fit}, {"held-out", c.Verify}} {
		blocks := 0
		for _, x := range half.cases {
			origins[x.Origin] = true
			if x.Label == caseBlock {
				blocks++
			}
		}
		if rarer := min(blocks, len(half.cases)-blocks); rarer < need {
			return refuse("the %s half holds %d of %d cases under its rarer label, and a sign test at p %g needs %d", half.name, rarer, len(half.cases), konst.CalibrateSignificance, need)
		}
	}
	if len(origins) < 2 {
		return refuse("every case came from one source, so a fit cannot tell the point from that source")
	}
	return nil
}

func calibrate(ctx context.Context, rule gate.Rule, corpus calibrationCorpus, ask caseAsker, now func() time.Time) (calibration, error) {
	if err := corpus.unfit(); err != nil {
		return calibration{}, err
	}
	cases := append(slices.Clone(corpus.Fit), corpus.Verify...)
	first, build, err := askEvery(ctx, cases, ask)
	if err != nil {
		return calibration{}, err
	}
	fitted, onFit, err := bestCut(rule, corpus.Fit, first[:len(corpus.Fit)])
	if err != nil {
		return calibration{}, err
	}
	fittedAt := now().UTC().Truncate(time.Second)
	onVerify, err := scoreCut(fitted, corpus.Verify, first[len(corpus.Fit):])
	if err != nil {
		return calibration{}, err
	}
	if gap := math.Abs(onFit.agreement() - onVerify.agreement()); gap > konst.CalibrateGapTolerance {
		return calibration{}, refuse("the held-out half agrees on %.4f against the fit half's %.4f, a gap of %.4f over the %g allowed", onVerify.agreement(), onFit.agreement(), gap, konst.CalibrateGapTolerance)
	}
	signP := signTest(onVerify.caught, onVerify.falseBlock)
	if signP >= konst.CalibrateSignificance {
		return calibration{}, refuse("on the held-out half the cut catches %d of %d blocks and falsely blocks %d, p %.4f against switching the gate off, not under %g",
			onVerify.caught, onVerify.blocks, onVerify.falseBlock, signP, konst.CalibrateSignificance)
	}
	calibrationError, err := expectedCalibrationError(fitted, corpus.Verify, first[len(corpus.Fit):])
	if err != nil {
		return calibration{}, err
	}
	verifiedAt := now().UTC().Truncate(time.Second)
	second, rerunBuild, err := askEvery(ctx, cases, ask)
	if err != nil {
		return calibration{}, err
	}
	if rerunBuild != build {
		return calibration{}, refuse("the rerun answered on build %s and the first ask on %s, so the two cannot be compared", rerunBuild, build)
	}
	same := 0
	for i := range cases {
		a, err := proceeds(fitted, first[i])
		if err != nil {
			return calibration{}, err
		}
		b, err := proceeds(fitted, second[i])
		if err != nil {
			return calibration{}, err
		}
		if a == b {
			same++
		}
	}
	rerun := float64(same) / float64(len(cases))
	if rerun < onFit.agreement() {
		return calibration{}, refuse("a rerun gives the same verdict on %.4f of cases, under the fit's own agreement %.4f, so the fit is reading noise", rerun, onFit.agreement())
	}
	return calibration{SignP: signP, AlwaysProceed: onVerify.alwaysProceed(), Lock: gate.Lock{
		Rule:             rule.Name,
		RuleVersion:      rule.RuleVersion,
		Questions:        rule.Questions,
		QuestionsVersion: rule.QuestionsVersion,
		Build:            build,
		Map:              gate.MapIdentity,
		FittedAt:         fittedAt,
		VerifiedAt:       verifiedAt,
		NFit:             len(corpus.Fit),
		NVerify:          len(corpus.Verify),
		AgreementFit:     onFit.agreement(),
		AgreementVerify:  onVerify.agreement(),
		RerunAgreement:   rerun,
		CalibrationError: calibrationError,
		Corpus:           corpus.Digest,
		Thresholds:       fitted.Thresholds,
		PinsThresholds:   true,
	}}, nil
}

func askEvery(ctx context.Context, cases []calibrationCase, ask caseAsker) ([]askedCase, string, error) {
	asked := make([]askedCase, len(cases))
	for i, c := range cases {
		answer, err := ask(ctx, c)
		if err != nil {
			return nil, "", fmt.Errorf("case %s: %w", c.ID, err)
		}
		if i > 0 && answer.Build != asked[0].Build {
			return nil, "", refuse("case %s was answered on build %s and case %s on %s, so one fit cannot cover both", c.ID, answer.Build, cases[0].ID, asked[0].Build)
		}
		asked[i] = answer
	}
	return asked, asked[0].Build, nil
}

func proceeds(rule gate.Rule, asked askedCase) (bool, error) {
	verdict, _, err := gate.Decide(asked.Answers, rule)
	return verdict == gate.VerdictAllow, err
}

func scoreCut(rule gate.Rule, cases []calibrationCase, asked []askedCase) (cutScore, error) {
	score := cutScore{cases: len(cases)}
	for i, c := range cases {
		proceed, err := proceeds(rule, asked[i])
		if err != nil {
			return cutScore{}, fmt.Errorf("case %s: %w", c.ID, err)
		}
		block := c.Label == caseBlock
		if block {
			score.blocks++
		}
		switch {
		case proceed != block:
			score.correct++
			if block {
				score.caught++
			}
		case !block:
			score.falseBlock++
		}
	}
	return score, nil
}

func bestCut(rule gate.Rule, cases []calibrationCase, asked []askedCase) (gate.Rule, cutScore, error) {
	var tied []gate.Rule
	var best cutScore
	for step := range konst.CalibrateCutSteps + 1 {
		candidate := rule
		candidate.Thresholds.RiskAskAt = konst.CalibrateCutCeiling * float64(step) / konst.CalibrateCutSteps
		score, err := scoreCut(candidate, cases, asked)
		if err != nil {
			return gate.Rule{}, cutScore{}, err
		}
		ahead := score.correct > best.correct || score.correct == best.correct && score.falseBlock < best.falseBlock
		if ahead || len(tied) == 0 {
			tied, best = nil, score
		}
		if score == best {
			tied = append(tied, candidate)
		}
	}
	return tied[len(tied)/2], best, nil
}

func expectedCalibrationError(rule gate.Rule, cases []calibrationCase, asked []askedCase) (float64, error) {
	var predicted, blocked [konst.CalibrateErrorBins]float64
	var counts [konst.CalibrateErrorBins]int
	for i, c := range cases {
		p := 0.0
		for level, mass := range asked[i].Answers[rule.RiskQuestion].Probabilities {
			value, err := strconv.ParseFloat(level, 64)
			if err != nil {
				return 0, fmt.Errorf("case %s: the %s level %q is not a number", c.ID, rule.RiskQuestion, level)
			}
			if value > rule.Thresholds.RiskAskAt {
				p += mass
			}
		}
		bin := min(int(p*konst.CalibrateErrorBins), konst.CalibrateErrorBins-1)
		counts[bin]++
		predicted[bin] += p
		if c.Label == caseBlock {
			blocked[bin]++
		}
	}
	ece := 0.0
	for bin, n := range counts {
		if n > 0 {
			ece += math.Abs(predicted[bin]-blocked[bin]) / float64(len(cases))
		}
	}
	return ece, nil
}

func signTest(wins, losses int) float64 {
	n := wins + losses
	lgN, _ := math.Lgamma(float64(n + 1))
	p := 0.0
	for k := wins; k <= n; k++ {
		lgK, _ := math.Lgamma(float64(k + 1))
		lgRest, _ := math.Lgamma(float64(n - k + 1))
		p += math.Exp(lgN - lgK - lgRest - float64(n)*math.Ln2)
	}
	return min(p, 1)
}
