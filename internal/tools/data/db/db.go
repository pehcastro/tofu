package db

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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
	"tofu/internal/tools/contract"
)

const Name = "db"

const defaultKey = "DATABASE_URL"

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
			"SQLite opens with mode=ro, PRAGMA query_only and no ATTACH, Postgres inside BEGIN READ ONLY, so a write statement is refused by the database itself",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string", "description": "one SELECT statement; left out, the schema is printed"},
				"table": map[string]any{"type": "string", "description": "print the schema of this table only"},
				"env":   map[string]any{"type": "string", "description": "the key holding the connection, " + defaultKey + " when left out"},
			},
		},
	}
}

type engine struct {
	driver, dsn, schema string
	secrets             []string
}

func (d DB) Run(ctx context.Context, raw json.RawMessage) (contract.Result, error) {
	var args struct {
		Query string `json:"query"`
		Table string `json:"table"`
		Env   string `json:"env"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return contract.Result{}, fmt.Errorf("db: arguments are not the expected shape: %w", err)
	}
	if args.Query != "" && args.Table != "" {
		return contract.Failure("db: give query or table, not both"), nil
	}
	key := cmp.Or(args.Env, defaultKey)
	value, err := d.connection(key)
	if err != nil {
		return contract.Result{}, err
	}
	if value == "" {
		return contract.Failure("db: no connection: " + key + " is not set in .env or the environment; name the key holding it with env"), nil
	}
	found, refusal := d.open(key, value)
	if refusal != "" {
		return contract.Failure(refusal), nil
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(d.limits.QueryMillis)*time.Millisecond)
	defer cancel()
	handle, err := sql.Open(found.driver, found.dsn)
	if err != nil {
		return contract.Failure(found.said("db: cannot open the database "+key+" names", err)), nil
	}
	defer func() { _ = handle.Close() }()
	conn, err := handle.Conn(ctx)
	if err != nil {
		return contract.Failure(found.said("db: cannot reach the database "+key+" names", err)), nil
	}
	defer func() { _ = conn.Close() }()
	if found.driver == "sqlite" {
		if _, err := sqlite.Limit(conn, sqlite3.SQLITE_LIMIT_ATTACHED, 0); err != nil {
			return contract.Result{}, fmt.Errorf("db: refusing ATTACH on the SQLite connection: %w", err)
		}
	}
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return contract.Failure(found.said("db: cannot reach the database "+key+" names", err)), nil
	}
	defer func() { _ = tx.Rollback() }()
	if args.Query == "" {
		return columns(ctx, tx, found, args.Table)
	}
	return d.rows(ctx, tx, found, key, args.Query)
}

func (d DB) connection(key string) (string, error) {
	body, err := os.ReadFile(filepath.Join(string(d.root), ".env"))
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
		return engine{driver: "postgres", dsn: value, schema: postgresSchema, secrets: []string{value, password, parsed.Host, parsed.Hostname(), parsed.User.Username()}}, ""
	}
	path := strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(value, "sqlite:"), "file:"), "//")
	if !filepath.IsAbs(path) {
		path = filepath.Join(string(d.root), path)
	}
	if !contract.IsFile(path) {
		return engine{}, "db: no SQLite file at the path " + key + " names"
	}
	uri := url.URL{Scheme: "file", Path: "/" + strings.TrimPrefix(filepath.ToSlash(path), "/"), RawQuery: "mode=ro&_query_only=1"}
	return engine{driver: "sqlite", dsn: uri.String(), schema: sqliteSchema, secrets: []string{value, path, filepath.ToSlash(path), uri.String()}}, ""
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

func (d DB) rows(ctx context.Context, tx *sql.Tx, found engine, key, query string) (contract.Result, error) {
	statement, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return contract.Failure(refusedOrFailed(found, key, err)), nil
	}
	defer func() { _ = statement.Close() }()
	rows, err := statement.QueryContext(ctx)
	if err != nil {
		return contract.Failure(refusedOrFailed(found, key, err)), nil
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
		return contract.Failure(refusedOrFailed(found, key, err)), nil
	default:
		fmt.Fprintf(&out, "%d rows shown of %d\n", min(count, d.limits.Rows), count)
	}
	return contract.Result{Content: out.String()}, nil
}

func refusedOrFailed(found engine, key string, err error) string {
	var lite interface{ Code() int }
	var postgres interface{ SQLState() string }
	if (errors.As(err, &lite) && lite.Code()&0xff == sqliteReadOnlyCode) || (errors.As(err, &postgres) && postgres.SQLState() == postgresReadOnlyTransaction) {
		return found.said("db: refused, "+key+" is opened read only and this statement writes", err)
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
