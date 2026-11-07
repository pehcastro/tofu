package db

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	_ "github.com/lib/pq"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"tofu/internal/models/llm/core"
	"tofu/internal/platform/credential"
	"tofu/internal/tools/contract"
)

const Name = "db"

const defaultKey = "DATABASE_URL"

const sqliteHeader = "SQLite format 3\x00"

const (
	sqliteReadOnlyCode          = 8
	postgresReadOnlyTransaction = "25006"
)

type Limits struct {
	Rows, CellBytes, QueryMillis int
}

type DB struct {
	root   contract.Root
	limits Limits
}

func New(dir string, limits Limits) (contract.Tool, error) {
	root, err := contract.NewRoot(dir)
	return DB{root: root, limits: limits}, err
}

func (DB) Name() string { return Name }

func (d DB) Definition() core.Tool {
	return core.Tool{
		Name: Name,
		Description: "reads the project's SQLite or Postgres database and never writes to it. " +
			"use it instead of sqlite3 or psql in the shell: those print wide tables and ask for a password, and nothing stops them writing. " +
			"with no query it prints the schema, one line per column with its type, not null and primary key, for every table or for the table named. " +
			"with query it runs that one statement and prints the first " + strconv.Itoa(d.limits.Rows) + " rows and the full count. " +
			"the connection is the value of a key in the project's .env or the environment, DATABASE_URL unless env names another: a postgres:// url, or a SQLite file path, sqlite: or file: in front allowed. the value is never printed. " +
			"with no key set it opens the project's one SQLite file by itself, whatever variable the code reads its path from, so there is nothing to export first; file opens a SQLite file in the project by its path. " +
			"SQLite opens with mode=ro, PRAGMA query_only and no ATTACH, Postgres inside BEGIN READ ONLY, so a write statement is refused by the database itself",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string", "description": "one SELECT statement; left out, the schema is printed"},
				"table": map[string]any{"type": "string", "description": "print the schema of this table only"},
				"env":   map[string]any{"type": "string", "description": "the key holding the connection, " + defaultKey + " when left out"},
				"file":  map[string]any{"type": "string", "description": "a SQLite file in the project, by path; used instead of any key"},
			},
		},
	}
}

type engine struct {
	driver, dsn, schema, name, opened string
	secrets                           []string
}

func (d DB) Run(ctx context.Context, raw json.RawMessage) (contract.Result, error) {
	var args struct {
		Query string `json:"query"`
		Table string `json:"table"`
		Env   string `json:"env"`
		File  string `json:"file"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return contract.Result{}, fmt.Errorf("db: arguments are not the expected shape: %w", err)
	}
	if args.Query != "" && args.Table != "" {
		return contract.Failure("db: give query or table, not both"), nil
	}
	found, refusal, err := d.locate(args.File, args.Env)
	if err != nil {
		return contract.Result{}, err
	}
	if refusal != "" {
		return contract.Failure(refusal), nil
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(d.limits.QueryMillis)*time.Millisecond)
	defer cancel()
	handle, err := sql.Open(found.driver, found.dsn)
	if err != nil {
		return contract.Failure(found.said("db: cannot open "+found.name, err)), nil
	}
	defer func() { _ = handle.Close() }()
	conn, err := handle.Conn(ctx)
	if err != nil {
		return contract.Failure(found.said("db: cannot reach "+found.name, err)), nil
	}
	defer func() { _ = conn.Close() }()
	if found.driver == "sqlite" {
		if _, err := sqlite.Limit(conn, sqlite3.SQLITE_LIMIT_ATTACHED, 0); err != nil {
			return contract.Result{}, fmt.Errorf("db: refusing ATTACH on the SQLite connection: %w", err)
		}
	}
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return contract.Failure(found.said("db: cannot reach "+found.name, err)), nil
	}
	defer func() { _ = tx.Rollback() }()
	var shown contract.Result
	if args.Query == "" {
		shown, err = columns(ctx, tx, found, args.Table)
	} else {
		shown, err = d.rows(ctx, tx, found, args.Query)
	}
	if shown.FailureText == "" {
		shown.Content = found.opened + shown.Content
	}
	return shown, err
}

func (d DB) locate(file, env string) (engine, string, error) {
	if file != "" {
		path, err := d.root.Resolve(file)
		if err != nil {
			return engine{}, "db: " + err.Error(), nil
		}
		if !isSQLite(path) {
			return engine{}, "db: " + file + " is not a SQLite file", nil
		}
		return sqliteEngine(path, file, "opened "+file+"\n"), "", nil
	}
	key := cmp.Or(env, defaultKey)
	if slices.Contains(credential.KeyNames(), key) {
		return engine{}, "db: env names " + key + ", which holds a tofu credential rather than a connection, so it is not read", nil
	}
	value, err := d.connection(key)
	if err != nil {
		return engine{}, "", err
	}
	if value != "" {
		found, refusal := d.open(key, value)
		return found, refusal, nil
	}
	if env != "" {
		return engine{}, "db: no connection: " + key + " is not set in .env or the environment; name the key holding it with env", nil
	}
	listed, err := contract.WalkListing(d.root)
	if err != nil {
		return engine{}, "", fmt.Errorf("db: looking for a SQLite file in the project: %w", err)
	}
	files := slices.DeleteFunc(listed, func(rel string) bool {
		switch strings.ToLower(filepath.Ext(rel)) {
		case ".db", ".sqlite", ".sqlite3", ".db3":
			return !isSQLite(filepath.Join(string(d.root), rel))
		}
		return true
	})
	switch len(files) {
	case 0:
		return engine{}, "db: no connection: " + key + " is not set in .env or the environment and the project holds no SQLite file; name the key holding it with env, or the file with file", nil
	case 1:
		return sqliteEngine(filepath.Join(string(d.root), files[0]), files[0], "opened "+files[0]+", the one SQLite file in the project, as "+key+" is not set\n"), "", nil
	}
	return engine{}, "db: " + key + " is not set and the project holds " + strconv.Itoa(len(files)) + " SQLite files: " + strings.Join(files, ", ") + "; name one with file", nil
}

func isSQLite(path string) bool {
	opened, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = opened.Close() }()
	head := make([]byte, len(sqliteHeader))
	_, err = io.ReadFull(opened, head)
	return err == nil && string(head) == sqliteHeader
}

func (d DB) connection(key string) (string, error) {
	body, err := credential.ReadCredential(filepath.Join(string(d.root), credential.CredentialFileName))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("db: reading .env: %w", err)
	}
	for line := range strings.Lines(string(body)) {
		name, value, ok := strings.Cut(strings.TrimPrefix(strings.TrimSpace(line), "export "), "=")
		if !ok || strings.TrimSpace(name) != key {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
			value = value[1 : len(value)-1]
		}
		return value, nil
	}
	return os.Getenv(key), nil
}

func (d DB) open(key, value string) (engine, string) {
	if strings.HasPrefix(value, "postgres://") || strings.HasPrefix(value, "postgresql://") {
		parsed, err := url.Parse(value)
		if err != nil {
			return engine{}, "db: the value of " + key + " is not a postgres url"
		}
		password, _ := parsed.User.Password()
		return engine{driver: "postgres", dsn: value, schema: postgresSchema, name: "the database " + key + " names", secrets: []string{value, password, parsed.Host, parsed.Hostname(), parsed.User.Username()}}, ""
	}
	path := strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(value, "sqlite:"), "file:"), "//")
	if !filepath.IsAbs(path) {
		path = filepath.Join(string(d.root), path)
	}
	if !contract.IsFile(path) {
		return engine{}, "db: no SQLite file at the path " + key + " names"
	}
	found := sqliteEngine(path, "the database "+key+" names", "")
	found.secrets = []string{value, path, filepath.ToSlash(path), found.dsn}
	return found, ""
}

func sqliteEngine(path, name, opened string) engine {
	uri := url.URL{Scheme: "file", Path: "/" + strings.TrimPrefix(filepath.ToSlash(path), "/"), RawQuery: "mode=ro&_query_only=1"}
	return engine{driver: "sqlite", dsn: uri.String(), schema: sqliteSchema, name: name, opened: opened}
}

func (e engine) said(what string, err error) string {
	secrets := slices.DeleteFunc(slices.Clone(e.secrets), func(s string) bool { return s == "" })
	slices.SortFunc(secrets, func(a, b string) int { return len(b) - len(a) })
	text := err.Error()
	for _, secret := range secrets {
		text = strings.ReplaceAll(text, secret, "<connection>")
	}
	return what + ": " + text
}

const sqliteSchema = `SELECT m.name, p.name, p.type, p."notnull", p.pk > 0 FROM sqlite_schema m JOIN pragma_table_info(m.name) p
WHERE m.type IN ('table', 'view') AND m.name NOT LIKE 'sqlite_%' AND (?1 = '' OR m.name = ?1) ORDER BY m.name, p.cid`

const postgresSchema = `SELECT c.table_schema || '.' || c.table_name, c.column_name, c.data_type, c.is_nullable = 'NO',
EXISTS (SELECT 1 FROM information_schema.table_constraints t JOIN information_schema.key_column_usage k
  ON k.constraint_schema = t.constraint_schema AND k.constraint_name = t.constraint_name
  WHERE t.constraint_type = 'PRIMARY KEY' AND k.table_schema = c.table_schema AND k.table_name = c.table_name AND k.column_name = c.column_name)
FROM information_schema.columns c
WHERE c.table_schema NOT IN ('pg_catalog', 'information_schema') AND ($1 = '' OR c.table_name = $1 OR c.table_schema || '.' || c.table_name = $1)
ORDER BY 1, c.ordinal_position`

func columns(ctx context.Context, tx *sql.Tx, found engine, table string) (contract.Result, error) {
	rows, err := tx.QueryContext(ctx, found.schema, table)
	if err != nil {
		return contract.Failure(found.said("db: reading the schema", err)), nil
	}
	defer func() { _ = rows.Close() }()
	var out strings.Builder
	tables := 0
	for last := ""; rows.Next(); {
		var owner, column, kind string
		var notNull, primary bool
		if err := rows.Scan(&owner, &column, &kind, &notNull, &primary); err != nil {
			return contract.Failure(found.said("db: reading the schema", err)), nil
		}
		if owner != last {
			last, tables = owner, tables+1
		}
		out.WriteString(owner + "." + column + " " + cmp.Or(kind, "untyped"))
		if primary {
			out.WriteString(" primary key")
		}
		if notNull {
			out.WriteString(" not null")
		}
		out.WriteString("\n")
	}
	if err := rows.Err(); err != nil {
		return contract.Failure(found.said("db: reading the schema", err)), nil
	}
	switch {
	case tables == 0 && table != "":
		return contract.Failure("db: no table or view named " + table + "; leave table out to list every one"), nil
	case tables == 0:
		return contract.Result{Content: "the database holds no table\n"}, nil
	case tables == 1:
		out.WriteString("1 table\n")
	default:
		fmt.Fprintf(&out, "%d tables\n", tables)
	}
	return contract.Result{Content: out.String()}, nil
}

func (d DB) rows(ctx context.Context, tx *sql.Tx, found engine, query string) (contract.Result, error) {
	statement, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return contract.Failure(refusedOrFailed(found, err)), nil
	}
	defer func() { _ = statement.Close() }()
	rows, err := statement.QueryContext(ctx)
	if err != nil {
		return contract.Failure(refusedOrFailed(found, err)), nil
	}
	defer func() { _ = rows.Close() }()
	names, err := rows.Columns()
	if err != nil {
		return contract.Failure(found.said("db: reading the columns", err)), nil
	}
	var out strings.Builder
	out.WriteString(strings.Join(names, " | ") + "\n")
	cells, count := make([]any, len(names)), 0
	for i := range cells {
		cells[i] = new(any)
	}
	for rows.Next() {
		if count++; count > d.limits.Rows {
			continue
		}
		if err := rows.Scan(cells...); err != nil {
			return contract.Failure(found.said("db: reading row "+strconv.Itoa(count), err)), nil
		}
		shown := make([]string, len(cells))
		for i, cell := range cells {
			shown[i] = d.cell(*cell.(*any))
		}
		out.WriteString(strings.Join(shown, " | ") + "\n")
	}
	switch err := rows.Err(); {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		fmt.Fprintf(&out, "%d rows shown of at least %d: counting stopped after %d ms\n", min(count, d.limits.Rows), count, d.limits.QueryMillis)
	case err != nil:
		return contract.Failure(refusedOrFailed(found, err)), nil
	default:
		fmt.Fprintf(&out, "%d rows shown of %d\n", min(count, d.limits.Rows), count)
	}
	return contract.Result{Content: out.String()}, nil
}

func refusedOrFailed(found engine, err error) string {
	var lite interface{ Code() int }
	var postgres interface{ SQLState() string }
	if (errors.As(err, &lite) && lite.Code()&0xff == sqliteReadOnlyCode) || (errors.As(err, &postgres) && postgres.SQLState() == postgresReadOnlyTransaction) {
		return found.said("db: refused, "+found.name+" is opened read only and this statement writes", err)
	}
	return found.said("db: the query failed", err)
}

func (d DB) cell(value any) string {
	var text string
	switch v := value.(type) {
	case nil:
		return "NULL"
	case []byte:
		if !utf8.Valid(v) {
			return "<" + strconv.Itoa(len(v)) + " bytes>"
		}
		text = string(v)
	case time.Time:
		text = v.Format(time.RFC3339Nano)
	default:
		text = fmt.Sprint(v)
	}
	text = strings.NewReplacer("\r", `\r`, "\n", `\n`).Replace(text)
	if len(text) <= d.limits.CellBytes {
		return text
	}
	cut := d.limits.CellBytes
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut] + "... " + strconv.Itoa(len(text)) + " bytes"
}
