package gate

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"tofu/internal/sys"
)

type CalibrationMap string

const MapIdentity CalibrationMap = "identity"

type Lock struct {
	Rule             string
	RuleVersion      int
	Questions        string
	QuestionsVersion int
	Build            string
	Map              CalibrationMap
	FittedAt         time.Time
	VerifiedAt       time.Time
	NFit             int
	NVerify          int
	AgreementFit     float64
	AgreementVerify  float64
	RerunAgreement   float64
	CalibrationError float64
	Corpus           string
	Thresholds       Thresholds
	PinsThresholds   bool
	File             string
}

func LoadLock(path string) (Lock, error) {
	lock := Lock{File: path}
	inThresholds := false
	err := scanKV(path, func(indent int, key, value string, line int) error {
		switch {
		case indent == 0:
			inThresholds = key == "thresholds"
			if inThresholds {
				lock.PinsThresholds = true
				return nil
			}
			return lock.setField(key, value, fmt.Sprintf("%s:%d: %s", path, line, key))
		case inThresholds:
			return lock.Thresholds.setField(key, value, path, line)
		default:
			return fmt.Errorf("%s:%d: an indent where a top-level key was expected", path, line)
		}
	})
	if err != nil {
		return Lock{}, err
	}
	if lock.Rule == "" {
		return Lock{}, fmt.Errorf("%s: the lock names no rule", path)
	}
	return lock, nil
}

func (l *Lock) setField(key, value, at string) error {
	var err error
	switch key {
	case "rule":
		l.Rule = value
	case "rule_version":
		l.RuleVersion, err = strconv.Atoi(value)
	case "questions":
		l.Questions = value
	case "questions_version":
		l.QuestionsVersion, err = strconv.Atoi(value)
	case "build":
		l.Build = value
	case "map":
		if CalibrationMap(value) != MapIdentity {
			return fmt.Errorf("%s is %q, the one map a fit produces, found %q", at, MapIdentity, value)
		}
		l.Map = MapIdentity
	case "fitted_at":
		l.FittedAt, err = time.Parse(time.RFC3339, value)
	case "verified_at":
		l.VerifiedAt, err = time.Parse(time.RFC3339, value)
	case "n_fit":
		l.NFit, err = strconv.Atoi(value)
	case "n_verify":
		l.NVerify, err = strconv.Atoi(value)
	case "agreement_fit":
		l.AgreementFit, err = strconv.ParseFloat(value, 64)
	case "agreement_verify":
		l.AgreementVerify, err = strconv.ParseFloat(value, 64)
	case "rerun_agreement":
		l.RerunAgreement, err = strconv.ParseFloat(value, 64)
	case "calibration_error":
		l.CalibrationError, err = strconv.ParseFloat(value, 64)
	case "corpus":
		l.Corpus = value
	default:
		return fmt.Errorf("%s is an unknown field", at)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", at, err)
	}
	return nil
}

func WriteLock(path string, l Lock) error {
	var b strings.Builder
	fmt.Fprintf(&b, "rule: %s\nrule_version: %d\nquestions: %s\nquestions_version: %d\nbuild: %s\nmap: %s\n", l.Rule, l.RuleVersion, l.Questions, l.QuestionsVersion, l.Build, l.Map)
	fmt.Fprintf(&b, "fitted_at: %s\nverified_at: %s\nn_fit: %d\nn_verify: %d\n", l.FittedAt.Format(time.RFC3339), l.VerifiedAt.Format(time.RFC3339), l.NFit, l.NVerify)
	fmt.Fprintf(&b, "agreement_fit: %g\nagreement_verify: %g\nrerun_agreement: %g\ncalibration_error: %g\ncorpus: %s\n", l.AgreementFit, l.AgreementVerify, l.RerunAgreement, l.CalibrationError, l.Corpus)
	if l.PinsThresholds {
		t := l.Thresholds
		fmt.Fprintf(&b, "thresholds:\n  risk_ask_at: %g\n  risk_deny_at: %g\n  user_requested_relax_at: %g\n  approval_relax_at: %g\n  from_untrusted_block_at: %g\n",
			t.RiskAskAt, t.RiskDenyAt, t.UserRequestedRelaxAt, t.ApprovalRelaxAt, t.FromUntrustedBlockAt)
	}
	return sys.WriteFile(path, []byte(b.String()), 0o644)
}

func LockPath(dir string, r Rule) string {
	return sys.Join(dir, fmt.Sprintf("%s@%d.lock", r.Name, r.RuleVersion))
}
