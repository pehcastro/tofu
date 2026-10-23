package main

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	catalog "tofu/internal/llm/models"
)

const tokenMustOutlive = 45 * time.Minute

func freshHome(dir, source string) ([]string, string, error) {
	owner, err := os.UserHomeDir()
	if err != nil {
		return nil, "", err
	}
	home := filepath.Join(dir, "home")
	roaming, local := filepath.Join(home, "AppData", "Roaming"), filepath.Join(home, "AppData", "Local")
	for _, made := range []string{filepath.Join(home, ".tofu"), roaming, local} {
		if err := os.MkdirAll(made, 0o700); err != nil {
			return nil, "", err
		}
	}
	binding, err := copyClassifierBinding(filepath.Join(owner, ".tofu"), filepath.Join(home, ".tofu"))
	if err != nil {
		return nil, "", err
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Join(owner, ".tofu", "agent.db"))+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = db.Close() }()
	if err := loginOutlivesRun(db, source); err != nil {
		return nil, "", err
	}
	if _, err := db.Exec(`VACUUM INTO ?`, filepath.Join(home, ".tofu", "agent.db")); err != nil {
		return nil, "", err
	}
	return []string{"HOME=" + home, "USERPROFILE=" + home, "APPDATA=" + roaming, "LOCALAPPDATA=" + local}, binding, nil
}

func copyClassifierBinding(ownerState, freshState string) (string, error) {
	body, err := os.ReadFile(catalog.RoleFile(ownerState, catalog.RoleClassifier))
	if errors.Is(err, fs.ErrNotExist) {
		return "", errors.New("the owner's home binds no classifier and the gate shadow run needs one: tofu models bind classifier provider/model")
	}
	if err != nil {
		return "", err
	}
	copied := catalog.RoleFile(freshState, catalog.RoleClassifier)
	if err := os.MkdirAll(filepath.Dir(copied), 0o700); err != nil {
		return "", err
	}
	return strings.TrimSpace(string(body)), os.WriteFile(copied, body, 0o644)
}

func loginOutlivesRun(db *sql.DB, source string) error {
	rows, err := db.Query(`SELECT json_extract(data, '$.expires') FROM credentials WHERE provider = ? AND disabled_cause IS NULL`, source)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	logins := 0
	for rows.Next() {
		var stamp string
		if err := rows.Scan(&stamp); err != nil {
			return err
		}
		expires, err := time.Parse(time.RFC3339Nano, stamp)
		if err != nil {
			return err
		}
		if left := time.Until(expires); left < tokenMustOutlive {
			return fmt.Errorf("the %s access token expires in %s, under %s, and a refresh inside the copy would rotate the refresh token the owner's own home holds", source, left.Round(time.Minute), tokenMustOutlive)
		}
		logins++
	}
	if logins == 0 {
		return fmt.Errorf("the owner's home holds no usable %s login", source)
	}
	return rows.Err()
}

func loginSource(model string) string {
	source, _, _ := strings.Cut(model, "/")
	return source
}
