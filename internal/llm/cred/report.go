package cred

import (
	"strings"
	"time"

	"tofu/internal/sys"
)

const dayStamp = "2006-01-02"

func DoctorState() string {
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
	return Report(rows)
}

func Report(rows []Row) string {
	if len(rows) == 0 {
		return "none"
	}
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		lines = append(lines, describe(row))
	}
	return strings.Join(lines, "; ")
}

func describe(row Row) string {
	line := string(row.Credential.Provider) + " " + row.Credential.Kind
	if row.DisabledCause != "" {
		return line + ", disabled: " + strings.ReplaceAll(row.DisabledCause, "\n", " ")
	}
	line += ", expires " + row.Credential.Expires.UTC().Format(time.RFC3339)
	spec, err := Lookup(string(row.Credential.Provider))
	if err != nil || spec.GrantLife == 0 || row.Credential.Authorized.IsZero() {
		return line
	}
	return line + ", re-login by " + row.Credential.Authorized.Add(spec.GrantLife).UTC().Format(dayStamp)
}
