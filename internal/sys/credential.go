package sys

import (
	"cmp"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

const (
	CredentialFileName     = ".env"
	CredentialStoreName    = "agent.db"
	OpenRouterKeyName      = "OPENROUTER_KEY"
	TypeSafeKeyName        = "TYPESAFE_API_KEY"
	BraveSearchKeyName     = "BRAVE_SEARCH_KEY"
	MetaMuseKeyName        = "META_MUSE_API_KEY"
	KeyRedactedMark        = "[key redacted]"
	liveCredentialOptIn    = "TOFU_TEST_LIVE_CREDENTIAL"
	credentialStoreMode    = 0o600
	credentialStoreDirMode = 0o700
	credentialStorePragmas = "?_txlock=immediate&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	keyTableSchema         = `CREATE TABLE IF NOT EXISTS api_keys (name TEXT PRIMARY KEY, value TEXT NOT NULL, updated_at INTEGER NOT NULL)`
	shortestRedactedKey    = 8
)

var builtGuarded string

func AllowLiveCredential(tb testing.TB) {
	tb.Setenv(liveCredentialOptIn, "1")
}

func CredentialsHiddenFromTests() bool {
	return (testing.Testing() || builtGuarded != "") && os.Getenv(liveCredentialOptIn) != "1"
}

func ReadCredential(path string) ([]byte, error) {
	if err := refuseOwnerCredential(path); err != nil {
		return nil, err
	}
	return readCredentialFile(path)
}

func readCredentialFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func WriteCredential(path string, data []byte, perm os.FileMode) error {
	if err := refuseOwnerCredential(path); err != nil {
		return err
	}
	return WriteFile(path, data, perm)
}

func refuseOwnerCredential(path string) error {
	if !CredentialsHiddenFromTests() {
		return nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	if !IsOwnerCredential(abs) {
		return nil
	}
	return fmt.Errorf("sys: %s is hidden from tests: it is one of the owner's credential files, and a test that means to use it calls sys.AllowLiveCredential first", abs)
}

func PlantOwnerCredential(tb testing.TB, name string) bool {
	value := ownerCredentialValue(name)
	if value == "" {
		return false
	}
	tb.Setenv(name, value)
	return true
}

func EqualsOwnerCredential(name, value string) bool {
	return value != "" && value == ownerCredentialValue(name)
}

func ownerCredentialValue(name string) string {
	for _, dir := range []string{SourceRoot(), OwnerHomeStateDir()} {
		if dir == "" {
			continue
		}
		raw, err := readCredentialFile(filepath.Join(dir, CredentialFileName))
		if err != nil {
			continue
		}
		if value := CredentialAssignment(string(raw), name); value != "" {
			return value
		}
	}
	return ""
}

func CredentialAssignment(body, name string) string {
	for line := range strings.SplitSeq(body, "\n") {
		if value := assignedTo(line, name); value != "" {
			return value
		}
	}
	return ""
}

func assignedTo(line, name string) string {
	line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "export "))
	value, found := strings.CutPrefix(line, name+"=")
	if !found {
		return ""
	}
	return strings.Trim(strings.TrimSpace(value), `"'`)
}

func KeyNames() []string {
	return []string{OpenRouterKeyName, TypeSafeKeyName, BraveSearchKeyName, MetaMuseKeyName}
}

func CredentialStorePath() (string, error) {
	home, err := HomeConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, CredentialStoreName), nil
}

func OpenCredentialStore(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), credentialStoreDirMode); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+credentialStorePragmas)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(keyTableSchema); err != nil {
		_ = db.Close()
		return nil, err
	}
	_ = os.Chmod(path, credentialStoreMode)
	return db, nil
}

func SaveKey(name, value string) error {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("sys: a %s value is one line and not empty", name)
	}
	path, err := CredentialStorePath()
	if err != nil {
		return err
	}
	if err := refuseOwnerCredential(path); err != nil {
		return err
	}
	db, err := OpenCredentialStore(path)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	_, err = db.Exec(`INSERT INTO api_keys (name, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		name, value, time.Now().UnixMilli())
	return err
}

func StoredKeys() (map[string]string, error) {
	path, err := CredentialStorePath()
	if err != nil {
		return nil, err
	}
	if refuseOwnerCredential(path) != nil {
		return nil, nil
	}
	present, err := Exists(path)
	if err != nil || !present {
		return nil, err
	}
	db, err := OpenCredentialStore(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()
	rows, err := db.Query(`SELECT name, value FROM api_keys`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	stored := map[string]string{}
	for rows.Next() {
		var name, value string
		if err := rows.Scan(&name, &value); err != nil {
			return nil, err
		}
		stored[name] = value
	}
	return stored, rows.Err()
}

type KeyMigration struct {
	From    string
	Moved   []string
	Removed bool
}

func MigrateHomeKeys() (KeyMigration, error) {
	home, err := HomeConfigDir()
	if err != nil {
		return KeyMigration{}, err
	}
	migration := KeyMigration{From: filepath.Join(home, CredentialFileName)}
	present, err := Exists(migration.From)
	if err != nil || !present {
		return migration, err
	}
	raw, err := ReadCredential(migration.From)
	if err != nil {
		return migration, err
	}
	var kept []string
	for line := range strings.SplitSeq(strings.TrimRight(string(raw), "\r\n"), "\n") {
		name, value := knownKeyIn(line)
		if name == "" {
			kept = append(kept, line)
			continue
		}
		if err := SaveKey(name, value); err != nil {
			return migration, err
		}
		migration.Moved = append(migration.Moved, name)
	}
	if len(migration.Moved) == 0 {
		return migration, nil
	}
	if !slices.ContainsFunc(kept, worthKeeping) {
		migration.Removed = true
		return migration, os.Remove(migration.From)
	}
	return migration, WriteCredential(migration.From, []byte(strings.Join(kept, "\n")+"\n"), credentialStoreMode)
}

func knownKeyIn(line string) (string, string) {
	for _, name := range KeyNames() {
		if value := assignedTo(line, name); value != "" {
			return name, value
		}
	}
	return "", ""
}

func worthKeeping(line string) bool {
	line = strings.TrimSpace(line)
	return line != "" && !strings.HasPrefix(line, "#")
}

type KeyRedactor struct {
	secrets  []string
	assigned *regexp.Regexp
}

func LoadKeyRedactor() KeyRedactor {
	names := KeyNames()
	var secrets []string
	for _, name := range names {
		secrets = append(secrets, strings.TrimSpace(os.Getenv(name)))
	}
	stored, _ := StoredKeys()
	for name, value := range stored {
		names = append(names, name)
		secrets = append(secrets, value)
	}
	secrets = slices.DeleteFunc(secrets, func(secret string) bool { return len(secret) < shortestRedactedKey })
	slices.SortFunc(secrets, func(a, b string) int { return cmp.Compare(len(b), len(a)) })
	for i, name := range names {
		names[i] = regexp.QuoteMeta(name)
	}
	return KeyRedactor{
		secrets:  secrets,
		assigned: regexp.MustCompile(`\b(` + strings.Join(names, "|") + `)(\s*=\s*\\?["']?)[^\s"'\\\[][^\s"'\\]*`),
	}
}

func (r KeyRedactor) Redact(text string) string {
	for _, secret := range r.secrets {
		text = strings.ReplaceAll(text, secret, KeyRedactedMark)
	}
	return r.assigned.ReplaceAllString(text, "${1}${2}"+KeyRedactedMark)
}
