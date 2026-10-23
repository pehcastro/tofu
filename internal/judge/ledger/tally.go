package ledger

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"tofu/internal/sys"
)

const tallyDirName = "keyuse"

type AllowanceScope string

const (
	ScopeKey  AllowanceScope = "key"
	ScopeTeam AllowanceScope = "team"
)

type AllowanceUnit string

const (
	UnitCalls  AllowanceUnit = "calls"
	UnitTokens AllowanceUnit = "tokens"
)

type Allowance struct {
	Scope     AllowanceScope `json:"scope"`
	Unit      AllowanceUnit  `json:"unit"`
	Limit     int            `json:"limit"`
	Remaining int            `json:"remaining"`
	ResetsAt  time.Time      `json:"resets_at,omitzero"`
}

type KeyError struct {
	Status int    `json:"status,omitempty"`
	What   string `json:"what"`
}

type KeyUse struct {
	Key         string      `json:"key"`
	Provider    string      `json:"provider"`
	Model       string      `json:"model,omitempty"`
	Usage       Usage       `json:"usage"`
	Queries     int         `json:"queries,omitempty"`
	Charge      Charge      `json:"charge"`
	UpstreamUSD float64     `json:"upstream_usd,omitempty"`
	Allowances  []Allowance `json:"allowances,omitempty"`
	Failed      *KeyError   `json:"failed,omitempty"`
	At          time.Time   `json:"at"`
}

func HeaderAllowance(header http.Header, now time.Time) []Allowance {
	allowance, read := headerAllowance(header, ScopeKey, UnitCalls, "X-RateLimit-Limit", "X-RateLimit-Remaining")
	if !read {
		return nil
	}
	if reset, read := lastNumber(header.Get("X-RateLimit-Reset")); read {
		allowance.ResetsAt = now.Add(time.Duration(reset) * time.Second).UTC()
	}
	return []Allowance{allowance}
}

func PerUnitAllowance(header http.Header, scope AllowanceScope) []Allowance {
	var allowances []Allowance
	for _, per := range []struct {
		unit   AllowanceUnit
		suffix string
	}{{UnitCalls, "-Requests"}, {UnitTokens, "-Tokens"}} {
		if allowance, read := headerAllowance(header, scope, per.unit, "X-RateLimit-Limit"+per.suffix, "X-RateLimit-Remaining"+per.suffix); read {
			allowances = append(allowances, allowance)
		}
	}
	return allowances
}

func headerAllowance(header http.Header, scope AllowanceScope, unit AllowanceUnit, limitName, remainingName string) (Allowance, bool) {
	limit, limitRead := lastNumber(header.Get(limitName))
	remaining, remainingRead := lastNumber(header.Get(remainingName))
	return Allowance{Scope: scope, Unit: unit, Limit: limit, Remaining: remaining}, limitRead && remainingRead
}

func lastNumber(header string) (int, bool) {
	fields := strings.Split(header, ",")
	number, err := strconv.Atoi(strings.TrimSpace(fields[len(fields)-1]))
	return number, err == nil
}

func tallyDir() (string, error) {
	home, err := sys.HomeConfigDir()
	return filepath.Join(home, tallyDirName), err
}

func Tally(use KeyUse) error {
	dir, err := tallyDir()
	if err != nil {
		return err
	}
	if use.At.IsZero() {
		use.At = time.Now()
	}
	use.At = use.At.UTC().Truncate(time.Millisecond)
	line, err := json.Marshal(use)
	if err != nil {
		return err
	}
	return appendLine(filepath.Join(dir, use.At.Format(dayLayout)+logSuffix), line)
}

func KeyUses() ([]KeyUse, error) {
	dir, err := tallyDir()
	if err != nil {
		return nil, err
	}
	days, err := NewReader(dir).days()
	if err != nil {
		return nil, err
	}
	var uses []KeyUse
	for _, day := range days {
		read, err := keyUsesOn(filepath.Join(dir, day+logSuffix))
		if err != nil {
			return nil, err
		}
		uses = append(uses, read...)
	}
	return uses, nil
}

func keyUsesOn(path string) ([]KeyUse, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	var uses []KeyUse
	lines := bufio.NewScanner(file)
	for lines.Scan() {
		var use KeyUse
		if json.Unmarshal(lines.Bytes(), &use) == nil && use.Key != "" {
			uses = append(uses, use)
		}
	}
	return uses, lines.Err()
}
