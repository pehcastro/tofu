#[derive(Clone, Copy)]
pub enum Run {
    Working,
    Finished,
    Stopped,
}

pub struct Session {
    pub name: &'static str,
    pub prompt: &'static str,
    pub when: &'static str,
    pub run: Run,
}

pub struct Project {
    pub name: &'static str,
    pub path: &'static str,
    pub when: &'static str,
    pub branch: &'static str,
    pub sessions: &'static [Session],
}

pub const PROJECTS: [Project; 3] = [
    Project {
        name: "notes-app",
        path: "F:/code/notes-app",
        when: "now",
        branch: "main",
        sessions: &[
            Session {
                name: "clear-sable-eagle",
                prompt: "Port the store to SQLite and keep the API the same",
                when: "22m ago",
                run: Run::Working,
            },
            Session {
                name: "quiet-amber-heron",
                prompt: "Check the badge after each reseed, every 10m",
                when: "1h ago",
                run: Run::Working,
            },
            Session {
                name: "tidy-ochre-wren",
                prompt: "Why is All sorted by id and not created_at?",
                when: "1d ago",
                run: Run::Finished,
            },
        ],
    },
    Project {
        name: "tofu",
        path: "F:/localhost/ephem-sh/tofu",
        when: "2h ago",
        branch: "develop",
        sessions: &[
            Session {
                name: "keen-brass-mole",
                prompt: "Run the bench and report the slowest step",
                when: "2h ago",
                run: Run::Finished,
            },
            Session {
                name: "crisp-azure-swift",
                prompt: "explain this repository to me",
                when: "3d ago",
                run: Run::Stopped,
            },
            Session {
                name: "vivid-sable-egret",
                prompt: "Fork keeps the recent tail word for word",
                when: "5d ago",
                run: Run::Finished,
            },
        ],
    },
    Project {
        name: "bob",
        path: "F:/localhost/ephem-sh/bob",
        when: "1w ago",
        branch: "main",
        sessions: &[Session {
            name: "fond-sandy-mink",
            prompt: "Scaffold the CLI and its first verb",
            when: "1w ago",
            run: Run::Finished,
        }],
    },
];

pub const ACCOUNT: &str = "claude-sub · personal";
pub const MODEL: &str = "Opus 5";
pub const EFFORT: &str = "High";
pub const CHECKOUT: &str = "Current checkout";

pub const TELL_ACCOUNT: &str = "Picks which account pays for this session.";
pub const TELL_ATTACH: &str = "Attaches a file or an image; it goes to the lead as a reference.";
pub const TELL_MODEL: &str = "Picks the lead model from your accounts.";
pub const TELL_CHECKOUT: &str =
    "Picks where the session works: this checkout, or a new worktree for it.";
pub const TELL_BRANCH: &str = "Picks the branch the session starts on.";
pub const TELL_FOLDER: &str =
    "Opens the system folder picker; tofu reads the folder for git, sessions and rules.";
pub const TELL_SESSION: &str = "Opens the session in the work workspace.";
pub const TELL_ALL_SESSIONS: &str = "Opens the Session screen (ctrl r).";
