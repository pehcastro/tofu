use Change::{Add, Del, Hunk, Same};
use Ink::{Add as Plus, Del as Minus, Dim, Fn, Kw, Plain, St, Ty, Word};

#[derive(Clone, Copy)]
pub enum Ink {
    Plain,
    Kw,
    Fn,
    Ty,
    St,
    Dim,
    Add,
    Del,
    Word,
}

#[derive(Clone, Copy, PartialEq, Eq)]
pub enum Change {
    Same,
    Add,
    Del,
    Hunk,
}

pub struct Line {
    pub number: &'static str,
    pub change: Change,
    pub runs: &'static [(Ink, &'static str)],
}

const fn line(number: &'static str, change: Change, runs: &'static [(Ink, &'static str)]) -> Line {
    Line {
        number,
        change,
        runs,
    }
}

#[derive(Clone, Copy)]
pub enum Icon {
    Folder,
    FolderCommand,
    FolderDatabase,
    FolderGithub,
    FolderPublic,
    FolderPublicOpen,
    FolderSrcOpen,
    Git,
    Go,
    GoMod,
    License,
    React,
    Readme,
    Tune,
    Typescript,
}

impl Icon {
    pub fn bytes(self) -> &'static [u8] {
        match self {
            Icon::Folder => include_bytes!("icons/folder.svg"),
            Icon::FolderCommand => include_bytes!("icons/folder-command.svg"),
            Icon::FolderDatabase => include_bytes!("icons/folder-database.svg"),
            Icon::FolderGithub => include_bytes!("icons/folder-github.svg"),
            Icon::FolderPublic => include_bytes!("icons/folder-public.svg"),
            Icon::FolderPublicOpen => include_bytes!("icons/folder-public-open.svg"),
            Icon::FolderSrcOpen => include_bytes!("icons/folder-src-open.svg"),
            Icon::Git => include_bytes!("icons/git.svg"),
            Icon::Go => include_bytes!("icons/go.svg"),
            Icon::GoMod => include_bytes!("icons/go-mod.svg"),
            Icon::License => include_bytes!("icons/license.svg"),
            Icon::React => include_bytes!("icons/react_ts.svg"),
            Icon::Readme => include_bytes!("icons/readme.svg"),
            Icon::Tune => include_bytes!("icons/tune.svg"),
            Icon::Typescript => include_bytes!("icons/typescript.svg"),
        }
    }
}

#[derive(Clone, Copy, PartialEq, Eq)]
pub enum Git {
    Clean,
    Modified,
    Untracked,
    Deleted,
    Conflict,
    Ignored,
}

pub struct Entry {
    pub name: &'static str,
    pub icon: Icon,
    pub git: Git,
    pub nested: bool,
    pub tell: &'static str,
}

const fn entry(
    name: &'static str,
    icon: Icon,
    git: Git,
    nested: bool,
    tell: &'static str,
) -> Entry {
    Entry {
        name,
        icon,
        git,
        nested,
        tell,
    }
}

pub const TOP: &[Entry] = &[
    entry(
        ".github",
        Icon::FolderGithub,
        Git::Clean,
        false,
        "Expands .github: workflows and templates.",
    ),
    entry(
        "cmd",
        Icon::FolderCommand,
        Git::Clean,
        false,
        "Expands cmd: one folder per binary.",
    ),
    entry(
        "migrations",
        Icon::FolderDatabase,
        Git::Clean,
        false,
        "Expands migrations: 8 SQL files, the newest not applied yet.",
    ),
];

pub const WEB: &[Entry] = &[
    entry(
        "web",
        Icon::FolderPublicOpen,
        Git::Modified,
        false,
        "Collapses web.",
    ),
    entry(
        "NoteList.tsx",
        Icon::React,
        Git::Modified,
        true,
        "Opens web/NoteList.tsx in a preview tab, in italics until you edit it. ts-dev changed it this session.",
    ),
    entry(
        "count.ts",
        Icon::Typescript,
        Git::Untracked,
        true,
        "Opens web/count.ts in a preview tab. Untracked: git has never seen it.",
    ),
    entry(
        "OldBadge.tsx",
        Icon::React,
        Git::Deleted,
        true,
        "Opens web/OldBadge.tsx as it was before it was deleted, read only, with Restore at the top.",
    ),
    entry(
        "Header.tsx",
        Icon::React,
        Git::Conflict,
        true,
        "Opens web/Header.tsx with the merge conflict: ours, theirs and both, one choice per block.",
    ),
    entry(
        "node_modules",
        Icon::Folder,
        Git::Ignored,
        false,
        "Expands node_modules. Ignored by git, so it stays dimmed and is never searched.",
    ),
    entry(
        ".env",
        Icon::Tune,
        Git::Ignored,
        false,
        "Opens .env with the values masked; click a value to show it. It never goes to the chat.",
    ),
    entry(
        ".gitignore",
        Icon::Git,
        Git::Clean,
        false,
        "Opens .gitignore in a preview tab.",
    ),
    entry(
        "go.mod",
        Icon::GoMod,
        Git::Clean,
        false,
        "Opens go.mod in a preview tab.",
    ),
    entry(
        "README.md",
        Icon::Readme,
        Git::Clean,
        false,
        "Opens README.md in a preview tab.",
    ),
    entry(
        "LICENSE",
        Icon::License,
        Git::Clean,
        false,
        "Opens LICENSE in a preview tab.",
    ),
];

pub const MENU: &[&[(&str, Option<&str>, &str)]] = &[
    &[
        (
            "New file",
            Some("N"),
            "Creates a file in web and opens it in a new tab.",
        ),
        ("New folder", None, "Creates a folder in web."),
    ],
    &[
        (
            "Rename",
            Some("F2"),
            "Renames web in place; imports that point into it are listed before anything moves.",
        ),
        ("Copy path", None, "Copies the absolute path of web."),
        (
            "Copy relative path",
            None,
            "Copies web, relative to notes-app.",
        ),
        (
            "Mention in chat",
            Some("@"),
            "Adds web to the chat as a reference the lead can read.",
        ),
    ],
    &[
        ("Open in Terminal", None, "Opens a shell tile in web."),
        ("Reveal in Explorer", None, "Shows web in Explorer."),
        (
            "Collapse all",
            None,
            "Collapses every open folder under web.",
        ),
    ],
    &[(
        "Delete",
        None,
        "Moves web to the recycle bin after a confirm naming its 4 files.",
    )],
];

pub const NOTES: &[Line] = &[
    line("1", Same, &[(Kw, "package"), (Plain, " notes")]),
    line("2", Same, &[]),
    line(
        "3",
        Same,
        &[(Kw, "import"), (Plain, " "), (St, "\"strings\"")],
    ),
    line("4", Same, &[]),
    line(
        "5",
        Same,
        &[
            (Kw, "type"),
            (Plain, " "),
            (Ty, "Note"),
            (Plain, " "),
            (Kw, "struct"),
            (Plain, " {"),
        ],
    ),
    line("6", Same, &[(Plain, "    ID    "), (Ty, "int")]),
    line("7", Same, &[(Plain, "    Title "), (Ty, "string")]),
    line("8", Same, &[(Plain, "}")]),
    line("9", Same, &[]),
    line(
        "10",
        Same,
        &[
            (Kw, "func"),
            (Plain, " "),
            (Fn, "Count"),
            (Plain, "(notes []"),
            (Ty, "Note"),
            (Plain, ") "),
            (Ty, "int"),
            (Plain, " {"),
        ],
    ),
    line("11", Same, &[(Plain, "    n := "), (St, "0")]),
    line(
        "12",
        Same,
        &[
            (Plain, "    "),
            (Kw, "for"),
            (Plain, " _, note := "),
            (Kw, "range"),
            (Plain, " notes {"),
        ],
    ),
    line(
        "13",
        Same,
        &[
            (Plain, "        "),
            (Kw, "if"),
            (Plain, " strings."),
            (Fn, "TrimSpace"),
            (Plain, "(note.Title) != "),
            (St, "\"\""),
            (Plain, " {"),
        ],
    ),
    line("14", Same, &[(Plain, "            n++")]),
    line("15", Same, &[(Plain, "        }")]),
    line("16", Same, &[(Plain, "    }")]),
    line(
        "17",
        Same,
        &[(Plain, "    "), (Kw, "return"), (Plain, " n")],
    ),
    line("18", Same, &[(Plain, "}")]),
    line("19", Same, &[]),
    line(
        "20",
        Same,
        &[
            (Kw, "func"),
            (Plain, " "),
            (Fn, "Titles"),
            (Plain, "(notes []"),
            (Ty, "Note"),
            (Plain, ") []"),
            (Ty, "string"),
            (Plain, " {"),
        ],
    ),
    line(
        "21",
        Same,
        &[
            (Plain, "    out := "),
            (Fn, "make"),
            (Plain, "([]"),
            (Ty, "string"),
            (Plain, ", "),
            (St, "0"),
            (Plain, ", "),
            (Fn, "len"),
            (Plain, "(notes))"),
        ],
    ),
    line(
        "22",
        Same,
        &[
            (Plain, "    "),
            (Kw, "for"),
            (Plain, " _, note := "),
            (Kw, "range"),
            (Plain, " notes {"),
        ],
    ),
    line(
        "23",
        Same,
        &[
            (Plain, "        out = "),
            (Fn, "append"),
            (Plain, "(out, note.Title)"),
        ],
    ),
];

pub const COUNT: &[Line] = NOTES.split_at(9).1.split_at(9).0;

pub const STORE: &[Line] = &[
    line("1", Same, &[(Kw, "package"), (Plain, " notes")]),
    line("2", Same, &[]),
    line(
        "3",
        Same,
        &[
            (Kw, "type"),
            (Plain, " "),
            (Ty, "Store"),
            (Plain, " "),
            (Kw, "struct"),
            (Plain, " {"),
        ],
    ),
    line("4", Same, &[(Plain, "    notes []"), (Ty, "Note")]),
    line("5", Same, &[(Plain, "}")]),
];

pub const STORE_DIFF: &[Line] = &[
    line(
        "",
        Hunk,
        &[(Dim, "@@ -20,6 +20,9 @@ "), (Dim, "type Store")],
    ),
    line(
        "20",
        Same,
        &[
            (Kw, "type"),
            (Plain, " "),
            (Ty, "Store"),
            (Plain, " "),
            (Kw, "struct"),
            (Plain, " {"),
        ],
    ),
    line(
        "21",
        Del,
        &[(Minus, "-"), (Plain, "    notes []"), (Ty, "Note")],
    ),
    line(
        "21",
        Add,
        &[
            (Plus, "+"),
            (Plain, "    "),
            (Word, "db  *sql."),
            (Ty, "DB"),
        ],
    ),
    line(
        "22",
        Add,
        &[(Plus, "+"), (Plain, "    ins *sql."), (Ty, "Stmt")],
    ),
    line("23", Same, &[(Plain, " }")]),
    line(
        "",
        Hunk,
        &[(Dim, "@@ -25,3 +26,8 @@ "), (Dim, "func (s *Store) All")],
    ),
    line(
        "25",
        Del,
        &[
            (Minus, "-"),
            (Plain, "    "),
            (Kw, "return"),
            (Plain, " s.notes, "),
            (Kw, "nil"),
        ],
    ),
    line(
        "26",
        Add,
        &[
            (Plus, "+"),
            (Plain, "    rows, err := s.db."),
            (Fn, "Query"),
            (Plain, "("),
            (St, "`SELECT id, title FROM notes ORDER BY id`"),
            (Plain, ")"),
        ],
    ),
];

pub const COUNT_TEST: &[Line] = &[
    line(
        "3",
        Same,
        &[
            (Kw, "func"),
            (Plain, " "),
            (Fn, "TestCount"),
            (Plain, "(t *testing."),
            (Ty, "T"),
            (Plain, ") {"),
        ],
    ),
    line(
        "4",
        Same,
        &[(Plain, "    cases := []"), (Kw, "struct"), (Plain, " {")],
    ),
    line("5", Same, &[(Plain, "        name "), (Ty, "string")]),
    line("6", Same, &[(Plain, "        in   []"), (Ty, "Note")]),
    line("7", Same, &[(Plain, "        want "), (Ty, "int")]),
    line("8", Same, &[(Plain, "    }{")]),
    line(
        "9",
        Same,
        &[
            (Plain, "        {"),
            (St, "\"empty\""),
            (Plain, ", "),
            (Kw, "nil"),
            (Plain, ", "),
            (St, "0"),
            (Plain, "},"),
        ],
    ),
    line(
        "10",
        Same,
        &[
            (Plain, "        {"),
            (St, "\"blank\""),
            (Plain, ", []"),
            (Ty, "Note"),
            (Plain, "{{Title: "),
            (St, "\"   \""),
            (Plain, "}}, "),
            (St, "0"),
            (Plain, "},"),
        ],
    ),
];
