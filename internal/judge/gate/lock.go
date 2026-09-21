package gate

import (
	"fmt"
	"strconv"
	"time"

	"tofu/internal/sys"
)

type Lock struct {
	Rule             string
	RuleVersion      int
	Questions        string
	QuestionsVersion int
	Build            string
	FittedAt         time.Time
	VerifiedAt       time.Time
	NFit             int
	NVerify          int
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
			return lock.setField(key, value, path, line)
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

func (l *Lock) setField(key, value, path string, line int) error {
	switch key {
	case "rule":
		l.Rule = value
	case "rule_version":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("%s:%d: rule_version is a whole number, found %q", path, line, value)
		}
		l.RuleVersion = n
	case "questions":
		l.Questions = value
	case "questions_version":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("%s:%d: questions_version is a whole number, found %q", path, line, value)
		}
		l.QuestionsVersion = n
	case "build":
		l.Build = value
	case "fitted_at":
		t, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return fmt.Errorf("%s:%d: fitted_at is RFC3339, found %q", path, line, value)
		}
		l.FittedAt = t
	case "verified_at":
		t, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return fmt.Errorf("%s:%d: verified_at is RFC3339, found %q", path, line, value)
		}
		l.VerifiedAt = t
	case "n_fit":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("%s:%d: n_fit is a whole number, found %q", path, line, value)
		}
		l.NFit = n
	case "n_verify":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("%s:%d: n_verify is a whole number, found %q", path, line, value)
		}
		l.NVerify = n
	default:
		return fmt.Errorf("%s:%d: unknown field %q", path, line, key)
	}
	return nil
}

func LockPath(dir string, r Rule) string {
	return sys.Join(dir, fmt.Sprintf("%s@%d.lock", r.Name, r.RuleVersion))
}
