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

type Allowance struct {
	Limit     int       `json:"limit"`
	Remaining int       `json:"remaining"`
	ResetsAt  time.Time `json:"resets_at,omitzero"`
}

type KeyError struct {
	Status int    `json:"status,omitempty"`
	What   string `json:"what"`
}

type KeyUse struct {
	Key       string     `json:"key"`
	Provider  string     `json:"provider"`
	Model     string     `json:"model,omitempty"`
	Usage     Usage      `json:"usage"`
	Queries   int        `json:"queries,omitempty"`
	Charge    Charge     `json:"charge"`
	Allowance *Allowance `json:"allowance,omitempty"`
	Failed    *KeyError  `json:"failed,omitempty"`
	At        time.Time  `json:"at"`
}

func HeaderAllowance(header http.Header, now time.Time) *Allowance {
	limit, limitRead := lastNumber(header.Get("X-RateLimit-Limit"))
	remaining, remainingRead := lastNumber(header.Get("X-RateLimit-Remaining"))
	if !limitRead || !remainingRead {
		return nil
	}
	allowance := &Allowance{Limit: limit, Remaining: remaining}
	if reset, read := lastNumber(header.Get("X-RateLimit-Reset")); read {
		allowance.ResetsAt = now.Add(time.Duration(reset) * time.Second).UTC()
	}
	return allowance
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
