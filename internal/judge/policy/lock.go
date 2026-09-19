package policy

import (
	"fmt"
	"strconv"
	"time"

	"boji/internal/sys"
)

type Lock struct {
	Policy           string
	PolicyVersion    int
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
	if lock.Policy == "" {
		return Lock{}, fmt.Errorf("%s: the lock names no policy", path)
	}
	return lock, nil
}

func (l Lock) Pinned(pol Policy) Policy {
	if l.PinsThresholds {
		pol.Thresholds = l.Thresholds
	}
	return pol
}

func (l *Lock) setField(key, value, path string, line int) error {
	switch key {
	case "policy":
		l.Policy = value
	case "policy_version":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("%s:%d: policy_version is a whole number, found %q", path, line, value)
		}
		l.PolicyVersion = n
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

func LockPath(dir string, pol Policy) string {
	return sys.Join(dir, fmt.Sprintf("%s@%d.lock", pol.Name, pol.PolicyVersion))
}
