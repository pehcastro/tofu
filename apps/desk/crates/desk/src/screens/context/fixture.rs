pub const BOARD: &str = "ICONTEXT-1";
pub const CAPACITY: usize = 250;
pub const FORK_AT: usize = 199;
pub const FORK_NOW: &str = "Fork now opens the session forks, ISESSION-2.";

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Part {
    System,
    Rules,
    Files,
    Tools,
    Reports,
    Conversation,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Swatch {
    Strong,
    Faint,
    Accent,
    Live,
    Trace,
    Warn,
}

pub struct Item {
    pub name: &'static str,
    pub meta: &'static str,
    pub tokens: &'static str,
    pub fate: &'static str,
}

pub struct Category {
    pub part: Part,
    pub name: &'static str,
    pub swatch: Swatch,
    pub title: &'static str,
}

pub const CATEGORIES: [Category; 6] = [
    Category {
        part: Part::System,
        name: "System and tools",
        swatch: Swatch::Strong,
        title: "Fixed",
    },
    Category {
        part: Part::Rules,
        name: "Rules and instructions",
        swatch: Swatch::Faint,
        title: "Rules that fired",
    },
    Category {
        part: Part::Files,
        name: "Files read",
        swatch: Swatch::Accent,
        title: "Files, newest read wins",
    },
    Category {
        part: Part::Tools,
        name: "Tool results",
        swatch: Swatch::Live,
        title: "Largest first",
    },
    Category {
        part: Part::Reports,
        name: "Sub-agent reports",
        swatch: Swatch::Trace,
        title: "Reports the lead has read",
    },
    Category {
        part: Part::Conversation,
        name: "Conversation",
        swatch: Swatch::Warn,
        title: "Turns",
    },
];

pub const LEGEND: [(Swatch, &str); 6] = [
    (Swatch::Strong, "system and tools"),
    (Swatch::Faint, "rules and instructions"),
    (Swatch::Accent, "files read"),
    (Swatch::Live, "tool results"),
    (Swatch::Trace, "sub-agent reports"),
    (Swatch::Warn, "conversation"),
];

pub fn size(part: Part, compacted: bool) -> (usize, &'static str) {
    match part {
        Part::System => (9, "20 tools"),
        Part::Rules => (4, "4 rules, AGENTS.md"),
        Part::Files => (11, "6 files"),
        Part::Tools if compacted => (12, "3 kept, 9 handles"),
        Part::Tools => (38, "12 results"),
        Part::Reports => (6, "3 reports"),
        Part::Conversation => (16, "14 turns"),
    }
}

const fn item(
    name: &'static str,
    meta: &'static str,
    tokens: &'static str,
    fate: &'static str,
) -> Item {
    Item {
        name,
        meta,
        tokens,
        fate,
    }
}

pub fn items(part: Part, compacted: bool) -> [Option<Item>; 3] {
    let handle = if compacted {
        "becomes a handle"
    } else {
        "kept"
    };
    match part {
        Part::Tools => [
            Some(item(
                "bash · go test ./... -v",
                "turn 9 · 14k of test output",
                "14k",
                handle,
            )),
            Some(item("grep · Store over notes/", "turn 11", "8k", handle)),
            Some(item(
                "browser_do · 6 steps",
                "turn 13, recent",
                "6k",
                "kept, recent",
            )),
        ],
        Part::Files => [
            Some(item(
                "notes/store.go",
                "read 3 times, the last copy counts",
                "5k",
                "kept",
            )),
            Some(item("web/NoteList.tsx", "read once", "3k", "kept")),
            Some(item("migrations/0007_notes.sql", "read once", "1k", "kept")),
        ],
        Part::Reports => [
            Some(item(
                "go-dev · add Count",
                "done, 2 files, checks passed",
                "2k",
                "kept",
            )),
            Some(item("ts-dev · badge", "in progress", "3k", "kept")),
            Some(item("scout · callers", "6 callers listed", "1k", "kept")),
        ],
        Part::Rules => [
            Some(item(
                "AGENTS.md",
                "project instructions, 2.9k of the 32k cap",
                "3k",
                "fixed",
            )),
            Some(item(
                "go/table_tests@1",
                "fired for notes_test.go",
                "0.4k",
                "fixed",
            )),
            None,
        ],
        Part::System => [
            Some(item("system prompt", "fixed per session", "5k", "fixed")),
            Some(item("20 tool schemas", "fixed per session", "4k", "fixed")),
            None,
        ],
        Part::Conversation => [
            Some(item("your messages", "14", "4k", "kept")),
            Some(item("the lead's answers", "14", "12k", "kept")),
            None,
        ],
    }
}
