package cred

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"tofu/internal/sys"
)

const (
	schemaStatements = `
CREATE TABLE IF NOT EXISTS credentials (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	provider TEXT NOT NULL,
	kind TEXT NOT NULL,
	account_key TEXT NOT NULL,
	data TEXT NOT NULL,
	disabled_cause TEXT,
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_credentials_account ON credentials(provider, account_key);
CREATE TABLE IF NOT EXISTS refresh_leases (
	credential_id INTEGER PRIMARY KEY,
	owner TEXT NOT NULL,
	expires_at_ms INTEGER NOT NULL
);
`
)

type Row struct {
	ID            int64
	Credential    Credential
	DisabledCause string
}

type Store struct {
	db *sql.DB
}

func Path() (string, error) {
	return sys.CredentialStorePath()
}

func Open(path string) (*Store, error) {
	db, err := sys.OpenCredentialStore(path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schemaStatements); err != nil {
		_ = db.Close()
		return nil, err
	}
	store := &Store{db: db}
	if err := store.renameRetiredSources(); err != nil {
		_ = store.Close()
		return nil, err
	}
	if err := store.captureMissingKeys(); err != nil {
		_ = store.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) renameRetiredSources() error {
	for retired, source := range retiredProviderWords {
		if _, err := s.db.Exec(
			`UPDATE OR IGNORE credentials SET provider = ? WHERE provider = ?`,
			string(source), retired); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) captureMissingKeys() error {
	rows, err := s.selectRows("WHERE account_key = ''")
	if err != nil {
		return err
	}
	for _, row := range rows {
		spec, err := Lookup(string(row.Credential.Provider))
		if err != nil || spec.IdentityTokenField == "" {
			continue
		}
		identity := identityFrom(spec, jwtClaims(row.Credential.Access))
		if identity.AccountID == "" {
			continue
		}
		row.Credential.Identity = identity
		data, err := json.Marshal(row.Credential)
		if err != nil {
			return err
		}
		if _, err := s.db.Exec(
			`UPDATE credentials SET account_key = ?, data = ? WHERE id = ? AND NOT EXISTS
			(SELECT 1 FROM credentials taken WHERE taken.provider = ? AND taken.account_key = ?)`,
			identity.AccountID, string(data), row.ID,
			string(row.Credential.Provider), identity.AccountID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Save(credential Credential, now time.Time) error {
	data, err := json.Marshal(credential)
	if err != nil {
		return err
	}
	accountKey := credential.Identity.AccountID
	if accountKey == "" {
		accountKey = credential.Identity.Email
	}
	stamp := now.UnixMilli()
	_, err = s.db.Exec(
		`INSERT INTO credentials (provider, kind, account_key, data, disabled_cause, created_at, updated_at)
		VALUES (?, ?, ?, ?, NULL, ?, ?)
		ON CONFLICT(provider, account_key) DO UPDATE SET
			kind = excluded.kind,
			data = excluded.data,
			disabled_cause = NULL,
			updated_at = excluded.updated_at`,
		string(credential.Provider), credential.Kind, accountKey, string(data), stamp, stamp)
	return err
}

func (s *Store) List() ([]Row, error) {
	return s.selectRows("ORDER BY id")
}

func (s *Store) Row(provider Provider) (Row, bool, error) {
	return s.RowAt(provider, time.Now())
}

func (s *Store) RowByID(id int64) (Row, bool, error) {
	rows, err := s.selectRows("WHERE id = ?", id)
	if err != nil || len(rows) == 0 {
		return Row{}, false, err
	}
	return rows[0], true, nil
}

func (s *Store) RowAt(provider Provider, now time.Time) (Row, bool, error) {
	rows, err := s.selectRows("WHERE provider = ? ORDER BY id", string(provider))
	if err != nil || len(rows) == 0 {
		return Row{}, false, err
	}
	for _, row := range rows {
		if row.Unusable(now) == "" {
			return row, true, nil
		}
	}
	return rows[0], true, nil
}

func (s *Store) selectRows(clause string, args ...any) ([]Row, error) {
	query, err := s.db.Query(`SELECT id, data, COALESCE(disabled_cause, '') FROM credentials `+clause, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = query.Close() }()
	var out []Row
	for query.Next() {
		var row Row
		var data string
		if err := query.Scan(&row.ID, &data, &row.DisabledCause); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(data), &row.Credential); err != nil {
			return nil, err
		}
		source, parseErr := ParseProvider(string(row.Credential.Provider))
		switch {
		case parseErr == nil:
			row.Credential.Provider = source
		case row.DisabledCause == "":
			row.DisabledCause = parseErr.Error()
		}
		out = append(out, row)
	}
	return out, query.Err()
}

func (s *Store) UpdateIfRefreshMatches(id int64, credential Credential, expected string, now time.Time) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	var data string
	if err := tx.QueryRow(`SELECT data FROM credentials WHERE id = ?`, id).Scan(&data); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	var stored Credential
	if err := json.Unmarshal([]byte(data), &stored); err != nil {
		return false, err
	}
	if stored.Refresh != expected {
		return false, nil
	}
	encoded, err := json.Marshal(credential)
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(`UPDATE credentials SET data = ?, updated_at = ? WHERE id = ?`,
		string(encoded), now.UnixMilli(), id); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func (s *Store) Disable(id int64, cause string, now time.Time) error {
	_, err := s.db.Exec(
		`UPDATE credentials SET disabled_cause = ?, updated_at = ? WHERE id = ? AND disabled_cause IS NULL`,
		cause, now.UnixMilli(), id)
	return err
}

func (s *Store) Enable(id int64, now time.Time) error {
	_, err := s.db.Exec(
		`UPDATE credentials SET disabled_cause = NULL, updated_at = ? WHERE id = ?`,
		now.UnixMilli(), id)
	return err
}

func (s *Store) AcquireLease(id int64, owner string, until, now time.Time) (bool, error) {
	result, err := s.db.Exec(
		`INSERT INTO refresh_leases (credential_id, owner, expires_at_ms) VALUES (?, ?, ?)
		ON CONFLICT(credential_id) DO UPDATE SET owner = excluded.owner, expires_at_ms = excluded.expires_at_ms
		WHERE refresh_leases.expires_at_ms <= ?`,
		id, owner, until.UnixMilli(), now.UnixMilli())
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected > 0, err
}

func (s *Store) ReleaseLease(id int64, owner string) error {
	_, err := s.db.Exec(`DELETE FROM refresh_leases WHERE credential_id = ? AND owner = ?`, id, owner)
	return err
}

func (s *Store) LeaseExpiry(id int64) (time.Time, bool, error) {
	var millis int64
	err := s.db.QueryRow(`SELECT expires_at_ms FROM refresh_leases WHERE credential_id = ?`, id).Scan(&millis)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("cred: reading the refresh lease: %w", err)
	}
	return time.UnixMilli(millis), true, nil
}
