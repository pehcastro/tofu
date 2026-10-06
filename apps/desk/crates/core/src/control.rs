pub const TELL_BADGE: &str = "not drawn yet";

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Control {
    NewWorkspace,
    Palette,
    Notifications,
    Account,
    OpenProject,
    Branch,
    Session,
    Context,
    Quota,
    Classifier,
    Cron,
}

impl Control {
    pub const STATUS_LEFT: [Control; 2] = [Control::Branch, Control::Session];
    pub const STATUS_RIGHT: [Control; 4] = [
        Control::Context,
        Control::Quota,
        Control::Classifier,
        Control::Cron,
    ];

    pub fn label(self) -> &'static str {
        match self {
            Control::NewWorkspace => "New workspace",
            Control::Palette => "Ctrl K",
            Control::Notifications => "Notifications",
            Control::Account => "Account",
            Control::OpenProject => "Open a project",
            Control::Branch => "no branch",
            Control::Session => "no session",
            Control::Context => "ctx",
            Control::Quota => "quota",
            Control::Classifier => "classifier",
            Control::Cron => "cron",
        }
    }

    pub fn tell(self) -> &'static str {
        match self {
            Control::NewWorkspace => {
                "Opens a new workspace with one empty tile: pick a module, or start from a preset."
            }
            Control::Palette => {
                "Opens the command palette: files, sessions, screens, settings and commands in one search."
            }
            Control::Notifications => {
                "Lists asks, failures and finished turns. Nothing has arrived yet."
            }
            Control::Account => {
                "Opens the account menu: subscriptions, keys, settings and what is new."
            }
            Control::OpenProject => {
                "Opens the project picker: pick a folder and tofu starts in it."
            }
            Control::Branch => "Opens Source control history and the branches of the project.",
            Control::Session => "Opens the Session screen for the session on screen.",
            Control::Context => "Opens the Context screen: what the lead holds and what it costs.",
            Control::Quota => "Opens the Limits screen: each account's quota and when it resets.",
            Control::Classifier => {
                "Opens the Classifier screen: the decisions Jev made in this turn."
            }
            Control::Cron => {
                "Opens the schedule in Settings: the loops and jobs tofu runs on its own."
            }
        }
    }
}
