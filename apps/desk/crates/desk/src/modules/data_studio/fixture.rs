pub struct Note {
    pub id: u32,
    pub title: &'static str,
    pub user: &'static str,
    pub at: &'static str,
    pub deleted: bool,
}

pub const BLANK_ID: u32 = 3;
pub const NEW_ID: u32 = 7;
pub const EDITED_TITLE: &str = "Ask about the deposit";

pub const NOTES: [Note; 6] = [
    Note {
        id: 1,
        title: "Buy oat milk",
        user: "1 · Luiz",
        at: "10-05 09:12",
        deleted: false,
    },
    Note {
        id: 2,
        title: "Call the landlord",
        user: "1 · Luiz",
        at: "10-05 10:40",
        deleted: false,
    },
    Note {
        id: 3,
        title: "",
        user: "2 · Ana",
        at: "10-05 11:02",
        deleted: false,
    },
    Note {
        id: 4,
        title: "Read chapter 4",
        user: "2 · Ana",
        at: "10-05 12:20",
        deleted: false,
    },
    Note {
        id: 5,
        title: "",
        user: "2 · Ana",
        at: "10-05 12:21",
        deleted: true,
    },
    Note {
        id: 6,
        title: "Pay the internet",
        user: "2 · Ana",
        at: "10-05 13:05",
        deleted: false,
    },
];

pub const NEW_NOTE: Note = Note {
    id: NEW_ID,
    title: "",
    user: "1 · Luiz",
    at: "now",
    deleted: false,
};

pub const USERS: [(u32, &str, &str); 2] = [(1, "Luiz", "2 notes"), (2, "Ana", "4 notes")];

pub struct Table {
    pub name: &'static str,
    pub rows: Option<u32>,
    pub tell: Option<&'static str>,
}

pub const TABLES: [Table; 4] = [
    Table {
        name: "notes",
        rows: Some(6),
        tell: None,
    },
    Table {
        name: "users",
        rows: Some(2),
        tell: None,
    },
    Table {
        name: "tags",
        rows: Some(9),
        tell: Some("Opens tags, 9 rows, in this grid."),
    },
    Table {
        name: "note_tags",
        rows: Some(14),
        tell: Some(
            "Opens note_tags, 14 rows: a join table, so each row shows the note and the tag it links.",
        ),
    },
];

pub const VIEW: Table = Table {
    name: "visible_notes",
    rows: None,
    tell: Some(
        "Opens the view visible_notes: its rows read only, and its SQL in a pane under the grid.",
    ),
};

pub const ENUM: Table = Table {
    name: "note_kind",
    rows: Some(3),
    tell: Some("Shows the enum note_kind: todo, idea, link, and the columns that use it."),
};

pub const TELL_SCHEMA: &str = "Picks the schema: public, auth, audit. The tables below follow it.";
pub const TELL_MIGRATIONS: &str = "Opens the migrations/ folder in the editor tab; the newest, 0008_sqlite.sql, is not applied yet.";
pub const TELL_SORT: &str = "Adds a sort by any column; the SQL in the footer follows.";
pub const TELL_COLUMNS: &str = "Shows or hides columns and sets their order, kept per table.";
pub const TELL_TEST: &str = "Opens the connection and runs select 1; shows the time and the server version, or the error in words.";
pub const TELL_CONNECT: &str =
    "Adds staging notes under Recent and opens it read only in a second tab of this module.";
pub const TELL_FILE: &str = "Opens the system file picker for .db, .sqlite, .sqlite3 and .duckdb files. Dropping a file on the data tab does the same.";

pub struct Source {
    pub name: &'static str,
    pub kind: &'static str,
    pub detail: &'static str,
    pub state: &'static str,
    pub tell: &'static str,
}

pub const FOUND: [Source; 2] = [
    Source {
        name: "data/fixtures.db",
        kind: "sqlite",
        detail: "a file in the repository · 2.1 MB",
        state: "",
        tell: "Opens data/fixtures.db, a SQLite file in the repository, in a second tab of this module. Writes go to the file on disk.",
    },
    Source {
        name: "db",
        kind: "postgres 16",
        detail: "docker-compose.yml · stopped",
        state: "start and connect",
        tell: "The db service in docker-compose.yml is stopped. Starts it with docker compose up db in a Terminal tile, then connects when the port answers.",
    },
];

pub const OWN: [Source; 2] = [
    Source {
        name: "agent.db",
        kind: "sqlite",
        detail: "~/.tofu · accounts and sign-ins, secrets masked",
        state: "read only",
        tell: "Opens ~/.tofu/agent.db read only: accounts and sign-ins. Token columns stay masked and never reach the chat. Unlocking writes needs a confirm.",
    },
    Source {
        name: "sessions",
        kind: "JSON lines as tables",
        detail: "~/.tofu/sessions · turns, tools, decisions",
        state: "read only",
        tell: "Opens ~/.tofu/sessions as tables, one row per turn, tool call and decision, read from the JSON lines files. Read only: a session is history.",
    },
];

pub const RECENT: Source = Source {
    name: "staging notes",
    kind: "postgres",
    detail: "staging.internal:5432 · 3d ago",
    state: "read only",
    tell: "Reconnects to staging notes read only; the password comes from agent.db.",
};
