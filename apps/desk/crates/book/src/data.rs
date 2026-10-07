use desk_ui::components::avatar::{Agent, AgentKind, AgentStatus};
use desk_ui::components::chip::GitStatus;
use desk_ui::components::feed::FeedEvent;
use desk_ui::components::list::{CellKind, Column, Width};
use gpui::SharedString;

const AGENTS: usize = 36;
const FILES: usize = 50;
pub const TABLE_ROWS: usize = 5_000;
const WORKING: usize = 20;
const ASKING: usize = 24;
const FAILED: usize = 27;

pub fn agents() -> Vec<Agent> {
    (0..AGENTS)
        .map(|ix| Agent {
            kind: AgentKind::ALL[ix % AgentKind::ALL.len()],
            instance: u32::try_from(ix / AgentKind::ALL.len() + 1).unwrap_or(u32::MAX),
            status: match ix {
                0..WORKING => AgentStatus::Working,
                WORKING..ASKING => AgentStatus::Asking,
                ASKING..FAILED => AgentStatus::Failed,
                _ => AgentStatus::Finished,
            },
        })
        .collect()
}

const FOLDERS: [&str; 5] = [
    "internal/turn",
    "internal/shell",
    "cmd/tofu",
    "library/models",
    "interface/tui",
];
const STEMS: [&str; 10] = [
    "loop", "frame", "ledger", "policy", "judge", "drive", "cassette", "picker", "budget", "owner",
];
const STATUSES: [GitStatus; 4] = [
    GitStatus::Modified,
    GitStatus::Added,
    GitStatus::Deleted,
    GitStatus::Untracked,
];

pub fn files() -> Vec<(GitStatus, SharedString)> {
    (0..FILES)
        .map(|ix| {
            let path = format!(
                "{}/{}.go",
                FOLDERS[ix % FOLDERS.len()],
                STEMS[ix % STEMS.len()]
            );
            (STATUSES[ix % STATUSES.len()], path.into())
        })
        .collect()
}

pub const COLUMNS: [Column; 4] = [
    Column {
        title: "Agent",
        width: Width::Fixed(110.0),
        kind: CellKind::Text,
    },
    Column {
        title: "File",
        width: Width::Flex(2.0),
        kind: CellKind::Mono,
    },
    Column {
        title: "Event",
        width: Width::Flex(1.0),
        kind: CellKind::Text,
    },
    Column {
        title: "When",
        width: Width::Fixed(72.0),
        kind: CellKind::Time,
    },
];

const VERBS: [&str; 6] = [
    "read",
    "edited",
    "ran go test",
    "searched",
    "asked the lead",
    "finished",
];

pub fn table_cell(row: usize, column: usize) -> SharedString {
    let kind = AgentKind::ALL[row % AgentKind::ALL.len()];
    match column {
        0 => format!("{} {}", kind.name(), row % 6 + 1).into(),
        1 => format!(
            "{}/{}_{row}.go",
            FOLDERS[row % FOLDERS.len()],
            STEMS[row % STEMS.len()]
        )
        .into(),
        2 => VERBS[row % VERBS.len()].into(),
        _ => format!("{}m ago", row / 12).into(),
    }
}

pub fn feed(agents: &[Agent]) -> Vec<(Agent, FeedEvent, SharedString)> {
    let Some(last) = agents.last().copied() else {
        return Vec::new();
    };
    let events = [
        FeedEvent::Read {
            path: "internal/turn/loop.go".into(),
        },
        FeedEvent::CodeSearch {
            query: "\\.Layers\\(".into(),
            hits: 27,
        },
        FeedEvent::WebSearch {
            query: "gpui uniform_list scroll".into(),
            results: 8,
        },
        FeedEvent::PageRead {
            url: "docs.rs/gpui".into(),
        },
        FeedEvent::Edit {
            path: "internal/turn/loop.go".into(),
            added: 2,
            removed: 1,
        },
        FeedEvent::Command {
            command: "go test ./internal/turn".into(),
            exit: 0,
        },
        FeedEvent::BrowserStep {
            action: "clicked Settings".into(),
        },
        FeedEvent::Ask {
            question: "May I delete the seven test callers?".into(),
        },
        FeedEvent::Failure {
            message: "exit 1: undefined: models.Layers".into(),
        },
        FeedEvent::Finished {
            worked_for: "12m".into(),
        },
        FeedEvent::Spawn { agent: last },
    ];
    events
        .into_iter()
        .enumerate()
        .filter_map(|(ix, event)| {
            let agent = agents.get(ix * 3 % agents.len()).copied()?;
            Some((agent, event, format!("{}m ago", ix * 2).into()))
        })
        .collect()
}
