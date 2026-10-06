#[derive(Clone, Copy, PartialEq, Eq)]
pub enum Kind {
    Go,
    Tsx,
    Ts,
    Readme,
    Markdown,
}

#[derive(Clone, Copy, PartialEq, Eq)]
pub enum Author {
    GoDev,
    TsDev,
    You,
}

pub struct Change {
    pub name: &'static str,
    pub dir: Option<&'static str>,
    pub kind: Kind,
    pub author: Author,
    pub added: u32,
    pub removed: Option<u32>,
    pub tracked: bool,
    pub stageable: bool,
    pub edit: Option<&'static str>,
    pub tell: &'static str,
}

pub const CHANGES: [Change; 5] = [
    Change {
        name: "notes.go",
        dir: Some("notes"),
        kind: Kind::Go,
        author: Author::GoDev,
        added: 10,
        removed: Some(1),
        tracked: true,
        stageable: true,
        edit: Some("edit#34a87e"),
        tell: "",
    },
    Change {
        name: "NoteList.tsx",
        dir: Some("web"),
        kind: Kind::Tsx,
        author: Author::TsDev,
        added: 7,
        removed: Some(2),
        tracked: true,
        stageable: false,
        edit: None,
        tell: "Shows the changes to web/NoteList.tsx: 7 lines added, 2 removed, by ts-dev.",
    },
    Change {
        name: "README.md",
        dir: None,
        kind: Kind::Readme,
        author: Author::You,
        added: 2,
        removed: None,
        tracked: true,
        stageable: false,
        edit: None,
        tell: "Shows the changes to README.md: 2 lines added, by you.",
    },
    Change {
        name: "notes_test.go",
        dir: Some("notes"),
        kind: Kind::Go,
        author: Author::GoDev,
        added: 24,
        removed: None,
        tracked: false,
        stageable: true,
        edit: Some("edit#4f60d8"),
        tell: "",
    },
    Change {
        name: "count.ts",
        dir: Some("web"),
        kind: Kind::Ts,
        author: Author::TsDev,
        added: 12,
        removed: None,
        tracked: false,
        stageable: false,
        edit: None,
        tell: "Shows web/count.ts, a new file of 12 lines, by ts-dev.",
    },
];

pub const MESSAGE: &str = "feat(notes): count notes and show a badge";
pub const COMMITTED: &str = "d4e2f10";

#[derive(Clone, Copy)]
pub enum Tone {
    Plain,
    Keyword,
    Function,
    Literal,
    Comment,
    Type,
}

pub struct Line {
    pub number: u32,
    pub added: bool,
    pub parts: &'static [(Tone, &'static str)],
}

use Tone::{Comment, Function, Keyword, Literal, Plain, Type};

pub const HUNK: &str = "@@ -9,4 +9,13 @@";
pub const HUNK_EDIT: &str = "edit#34a87e";

pub const LINES: [Line; 10] = [
    Line {
        number: 9,
        added: false,
        parts: &[(Comment, "// TODO: count notes")],
    },
    Line {
        number: 10,
        added: true,
        parts: &[
            (Keyword, "func"),
            (Function, "Count"),
            (Plain, "(notes []"),
            (Type, "Note"),
            (Plain, ") "),
            (Type, "int"),
            (Plain, " {"),
        ],
    },
    Line {
        number: 11,
        added: true,
        parts: &[(Plain, "    n := "), (Literal, "0")],
    },
    Line {
        number: 12,
        added: true,
        parts: &[
            (Keyword, "for"),
            (Plain, " _, note := "),
            (Keyword, "range"),
            (Plain, " notes {"),
        ],
    },
    Line {
        number: 13,
        added: true,
        parts: &[
            (Keyword, "if"),
            (Plain, " strings."),
            (Function, "TrimSpace"),
            (Plain, "(note.Title) != "),
            (Literal, "\"\""),
            (Plain, " {"),
        ],
    },
    Line {
        number: 14,
        added: true,
        parts: &[(Plain, "            n++")],
    },
    Line {
        number: 15,
        added: true,
        parts: &[(Plain, "        }")],
    },
    Line {
        number: 16,
        added: true,
        parts: &[(Plain, "    }")],
    },
    Line {
        number: 17,
        added: true,
        parts: &[(Keyword, "return"), (Plain, " n")],
    },
    Line {
        number: 18,
        added: true,
        parts: &[(Plain, "}")],
    },
];

#[derive(Clone, Copy, PartialEq, Eq)]
pub enum Day {
    Today,
    Yesterday,
}

#[derive(Clone, Copy)]
pub enum Avatar {
    Pehcastro,
    Ana,
}

pub struct Commit {
    pub day: Day,
    pub hash: &'static str,
    pub summary: &'static str,
    pub author: &'static str,
    pub avatar: Avatar,
    pub age: &'static str,
    pub with: Option<&'static str>,
    pub when: &'static str,
    pub title: &'static str,
    pub body: &'static str,
    pub tell: Option<&'static str>,
}

pub const COMMITS: [Commit; 4] = [
    Commit {
        day: Day::Today,
        hash: "d9f27ab",
        summary: "fix(undo): snapshot the project root",
        author: "pehcastro",
        avatar: Avatar::Pehcastro,
        age: "2h",
        with: Some("with go-dev, 3 files"),
        when: "pehcastro \u{b7} today 15:10",
        title: "fix(undo): snapshot the project root, one store per project, prune",
        body: "Undo now snapshots the whole project root, keeps one store per project and prunes after 7 days. Most of it was written by go-dev in session crisp-azure-swift, turn 9.",
        tell: None,
    },
    Commit {
        day: Day::Today,
        hash: "a88092d",
        summary: "Merge feat/badge-spike",
        author: "pehcastro",
        avatar: Avatar::Pehcastro,
        age: "5h",
        with: None,
        when: "pehcastro \u{b7} today 12:40",
        title: "Merge feat/badge-spike",
        body: "Brings in the badge spike; no agent changes.",
        tell: None,
    },
    Commit {
        day: Day::Yesterday,
        hash: "871a759",
        summary: "feat(undo): put back the files a turn changed",
        author: "ana",
        avatar: Avatar::Ana,
        age: "1d",
        with: Some("with ts-dev, go-dev, 9 files"),
        when: "ana \u{b7} yesterday 18:02",
        title: "feat(undo): put back the files a turn changed",
        body: "tofu undo and /undo restore the files a turn changed. ts-dev and go-dev wrote 9 files over two sessions.",
        tell: None,
    },
    Commit {
        day: Day::Yesterday,
        hash: "151d974",
        summary: "docs(changelog): search units",
        author: "pehcastro",
        avatar: Avatar::Pehcastro,
        age: "1d",
        with: None,
        when: "",
        title: "",
        body: "",
        tell: Some("Opens 151d974: its message, its files and their diffs."),
    },
];

pub struct Touched {
    pub path: &'static str,
    pub kind: Kind,
    pub agent: bool,
    pub added: u32,
    pub removed: Option<u32>,
}

pub const TOUCHED: [Touched; 3] = [
    Touched {
        path: "internal/snapshot/snapshot.go",
        kind: Kind::Go,
        agent: true,
        added: 48,
        removed: Some(12),
    },
    Touched {
        path: "cmd/tofu/undo.go",
        kind: Kind::Go,
        agent: false,
        added: 6,
        removed: None,
    },
    Touched {
        path: "CHANGELOG.md",
        kind: Kind::Markdown,
        agent: false,
        added: 3,
        removed: None,
    },
];

pub const HISTORY_NOTE: &str = "\"with go-dev\" means the commit carries changes an agent made in a session; the desk knows it from tofu's records, git does not.";
pub const BLOCKED: &str =
    "feat/sort-order would overwrite 3 changed files, and ts-dev is writing web/ right now.";

pub struct Branch {
    pub name: &'static str,
    pub age: &'static str,
    pub switches: bool,
    pub tell: &'static str,
}

pub const TYPED: &str = "feat/so";
pub const CREATE_TELL: &str = "Creates the branch from main and switches to it when the turn ends.";
pub const BRANCHES: [Branch; 2] = [
    Branch {
        name: "feat/sort-order",
        age: "4h",
        switches: true,
        tell: "",
    },
    Branch {
        name: "feat/sockets",
        age: "2d",
        switches: false,
        tell: "Switches to feat/sockets when the turn ends; your 3 changed files would be stashed first.",
    },
];

pub const TELL_STAGE_HUNK: &str = "Stages this hunk only; the rest of the file stays unstaged.";
pub const TELL_DISCARD_HUNK: &str =
    "Discards this hunk after a confirm; tofu keeps a snapshot so undo can bring it back.";
pub const TELL_MENTION: &str =
    "Adds this commit to the chat as a reference: the lead reads its message and diff.";
pub const TELL_STASH: &str = "Waits for the turn to end, stashes the 3 changed files, switches to feat/sort-order and tells you where the stash is.";
