#[derive(Clone, Copy, PartialEq, Eq)]
pub enum Kin {
    Go,
    Ts,
    Qa,
    Py,
}

#[derive(Clone, Copy)]
pub enum State {
    Running,
    Waiting,
    Failed,
    Done,
}

pub struct Agent {
    pub id: &'static str,
    pub kin: Kin,
    pub state: State,
    pub add: &'static str,
    pub del: &'static str,
}

#[derive(Clone, Copy, PartialEq, Eq)]
pub enum Op {
    Created,
    Deleted,
    Modified,
}

#[derive(Clone, Copy)]
pub enum Lang {
    Go,
    React,
}

pub struct File {
    pub base: &'static str,
    pub dir: &'static str,
    pub op: Op,
    pub lang: Lang,
    pub add: &'static str,
    pub del: &'static str,
}

#[derive(Clone, Copy)]
pub enum Mark {
    Same,
    Gone,
    New,
}

pub struct Line {
    pub number: &'static str,
    pub mark: Mark,
    pub code: &'static str,
}

pub struct Change {
    pub by: &'static str,
    pub op: Op,
    pub file: &'static str,
    pub dir: &'static str,
    pub summary: &'static str,
    pub add: &'static str,
    pub del: &'static str,
    pub ago: &'static str,
    pub hunk: &'static str,
    pub lines: &'static [Line],
}

pub const FILE_SUM: &str = "20 files";
pub const ADD_SUM: &str = "+102";
pub const DEL_SUM: &str = "-39";
pub const RUNNING: &str = "5 running";

const fn agent(
    id: &'static str,
    kin: Kin,
    state: State,
    add: &'static str,
    del: &'static str,
) -> Agent {
    Agent {
        id,
        kin,
        state,
        add,
        del,
    }
}

pub const AGENTS: [Agent; 16] = [
    agent("go-dev 1", Kin::Go, State::Running, "+58", "-5"),
    agent("go-dev 3", Kin::Go, State::Running, "+2", ""),
    agent("go-dev 7", Kin::Go, State::Running, "+4", "-5"),
    agent("ts-dev 1", Kin::Ts, State::Failed, "+8", "-3"),
    agent("ts-dev 3", Kin::Ts, State::Running, "+7", "-2"),
    agent("qa 2", Kin::Qa, State::Waiting, "+2", ""),
    agent("py-dev 1", Kin::Py, State::Failed, "+2", ""),
    agent("py-dev 3", Kin::Py, State::Running, "+2", ""),
    agent("go-dev 2", Kin::Go, State::Done, "+14", ""),
    agent("go-dev 4", Kin::Go, State::Done, "+2", ""),
    agent("go-dev 8", Kin::Go, State::Done, "+0", "-31"),
    agent("ts-dev 2", Kin::Ts, State::Done, "+2", ""),
    agent("ts-dev 4", Kin::Ts, State::Done, "+2", ""),
    agent("qa 1", Kin::Qa, State::Done, "+2", ""),
    agent("qa 3", Kin::Qa, State::Done, "+2", ""),
    agent("py-dev 2", Kin::Py, State::Done, "+2", ""),
];

const fn file(
    base: &'static str,
    dir: &'static str,
    op: Op,
    lang: Lang,
    add: &'static str,
    del: &'static str,
) -> File {
    File {
        base,
        dir,
        op,
        lang,
        add,
        del,
    }
}

pub const FILES: [File; 8] = [
    file("sqlite.go", "store/", Op::Created, Lang::Go, "+52", ""),
    file("json.go", "store/", Op::Deleted, Lang::Go, "", "-31"),
    file("store.go", "store/", Op::Modified, Lang::Go, "+4", "-5"),
    file("Header.tsx", "web/", Op::Modified, Lang::React, "+1", "-1"),
    file("notes_test.go", "notes/", Op::Created, Lang::Go, "+12", ""),
    file(
        "NoteList.tsx",
        "web/",
        Op::Modified,
        Lang::React,
        "+5",
        "-2",
    ),
    file("notes.go", "notes/", Op::Modified, Lang::Go, "+2", ""),
    file("keys.ts", "web/", Op::Modified, Lang::React, "+2", ""),
];

const fn line(number: &'static str, mark: Mark, code: &'static str) -> Line {
    Line { number, mark, code }
}

const MAIN_HUNK: [Line; 6] = [
    line("20", Mark::Same, "export function main() {"),
    line("21", Mark::Same, "  const db = connect()"),
    line(
        "22",
        Mark::New,
        "  const rows = await db.all(\"SELECT * FROM notes\")",
    ),
    line("23", Mark::New, "  render(rows)"),
    line("24", Mark::Same, "  db.close()"),
    line("25", Mark::Same, "}"),
];

pub const CHANGES: [Change; 3] = [
    Change {
        by: "ts-dev 1",
        op: Op::Modified,
        file: "Header.tsx",
        dir: "web/",
        summary: "Hides the badge when the count is zero",
        add: "+1",
        del: "-1",
        ago: "1m ago",
        hunk: "@@ -29,7 +29,7 @@ export function Header()",
        lines: &[
            line("29", Mark::Same, "  const { data: count = 0 } = useCount()"),
            line("30", Mark::Same, "  return ("),
            line("31", Mark::Same, "    <header className=\"top\">"),
            line("32", Mark::Same, "      <h1>Notes</h1>"),
            line("33", Mark::Gone, "      <Badge count={count} />"),
            line(
                "33",
                Mark::New,
                "      {count > 0 && <Badge count={count} />}",
            ),
            line("34", Mark::Same, "    </header>"),
            line("35", Mark::Same, "  )"),
            line("36", Mark::Same, "}"),
        ],
    },
    Change {
        by: "go-dev 1",
        op: Op::Modified,
        file: "notes.go",
        dir: "notes/",
        summary: "Port Store.All to SQLite",
        add: "+2",
        del: "",
        ago: "2m ago",
        hunk: "@@ -20,6 +20,8 @@",
        lines: &MAIN_HUNK,
    },
    Change {
        by: "ts-dev 3",
        op: Op::Modified,
        file: "keys.ts",
        dir: "web/",
        summary: "Move NoteList to TanStack Query",
        add: "+2",
        del: "",
        ago: "2m ago",
        hunk: "@@ -20,6 +20,8 @@",
        lines: &MAIN_HUNK,
    },
];

pub struct Story {
    pub file: &'static str,
    pub note: &'static str,
    pub by: &'static str,
    pub ago: &'static str,
    pub summary: &'static str,
    pub head: &'static str,
    pub added: &'static [&'static str],
}

pub const STORIES: [Story; 1] = [Story {
    file: "sqlite.go",
    note: "created in this session \u{b7} 1 edit \u{b7} The SQLite store, with WAL mode and soft delete",
    by: "go-dev 1",
    ago: "3m ago",
    summary: "1 hunk",
    head: "new file \u{b7} 52 lines",
    added: &[
        "package store",
        "",
        "import (",
        "        \"database/sql\"",
        "        \"fmt\"",
        "        \"time\"",
        "",
        "        _ \"modernc.org/sqlite\"",
        ")",
        "",
        "type SQLite struct {",
        "        db *sql.DB",
        "}",
        "",
        "func Open(path string) (*SQLite, error) {",
        "        db, err := sql.Open(\"sqlite\", \"file:\"+path+\"?_pragma=journal_mode(WAL)\")",
        "        if err != nil {",
        "                return nil, fmt.Errorf(\"open %s: %w\", path, err)",
        "        }",
        "        if _, err := db.Exec(schema); err != nil {",
        "                return nil, err",
        "        }",
        "        return &SQLite{db: db}, nil",
        "}",
        "",
        "func (s *SQLite) All() ([]Note, error) {",
        "        rows, err := s.db.Query(\"SELECT id, title, created_at FROM notes WHERE deleted_at IS NULL ORDER BY created_at\")",
        "        if err != nil {",
        "                return nil, err",
        "        }",
        "        defer rows.Close()",
        "        var out []Note",
        "        for rows.Next() {",
        "                var n Note",
        "                if err := rows.Scan(&n.ID, &n.Title, &n.CreatedAt); err != nil {",
        "                        return nil, err",
        "                }",
        "                out = append(out, n)",
        "        }",
        "        return out, rows.Err()",
        "}",
        "",
        "func (s *SQLite) Count() (int, error) {",
        "        var n int",
        "        err := s.db.QueryRow(\"SELECT count(*) FROM notes WHERE deleted_at IS NULL\").Scan(&n)",
        "        return n, err",
        "}",
        "",
        "func (s *SQLite) Delete(id int64) error {",
        "        _, err := s.db.Exec(\"UPDATE notes SET deleted_at = ? WHERE id = ?\", time.Now(), id)",
        "        return err",
        "}",
    ],
}];
