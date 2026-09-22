package cred

import (
	"strings"
	"time"

	"tofu/internal/sys"
)

const dayStamp = "2006-01-02"

func DoctorState(now time.Time) string {
	path, err := Path()
	if err != nil {
		return "unknown: " + err.Error()
	}
	exists, err := sys.Exists(path)
	if err != nil {
		return "unreadable: " + err.Error()
	}
	if !exists {
		return "none"
	}
	store, err := Open(path)
	if err != nil {
		return "unreadable: " + err.Error()
	}
	defer func() { _ = store.Close() }()
	rows, err := store.List()
	if err != nil {
		return "unreadable: " + err.Error()
	}
	return Report(rows, now)
}

func Report(rows []Row, now time.Time) string {
	if len(rows) == 0 {
		return "none"
	}
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		lines = append(lines, row.State(now))
	}
	return strings.Join(lines, "; ")
}

func (r Row) Unusable(now time.Time) string {
	if r.DisabledCause != "" {
		return "disabled: " + strings.ReplaceAll(r.DisabledCause, "\n", " ")
	}
	deadline, dated := r.reloginBy()
	if !dated || now.Before(deadline) {
		return ""
	}
	return "the login expired on " + deadline.UTC().Format(dayStamp) +
		", run tofu login " + string(r.Credential.Provider)
}

func (r Row) reloginBy() (time.Time, bool) {
	spec, err := Lookup(string(r.Credential.Provider))
	if err != nil || spec.GrantLife == 0 || r.Credential.Authorized.IsZero() {
		return time.Time{}, false
	}
	return r.Credential.Authorized.Add(spec.GrantLife), true
}

func (r Row) State(now time.Time) string {
	line := string(r.Credential.Provider) + " " + r.Credential.Kind
	if cause := r.Unusable(now); cause != "" {
		return line + ", " + cause
	}
	line += ", expires " + r.Credential.Expires.UTC().Format(time.RFC3339)
	deadline, dated := r.reloginBy()
	if !dated {
		return line
	}
	return line + ", re-login by " + deadline.UTC().Format(dayStamp)
}
