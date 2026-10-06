pub struct Step {
    pub short: &'static str,
    pub long: &'static str,
}

pub const STEPS: [Step; 5] = [
    Step {
        short: "open localhost:5871/notes",
        long: "Opened localhost:5871/notes and waited for the list",
    },
    Step {
        short: "read the badge: 5 notes",
        long: "Read span.badge before the delete: \"5 notes\"",
    },
    Step {
        short: "click Delete on Call the landlord",
        long: "Clicking Delete on the row \"Call the landlord\"",
    },
    Step {
        short: "read the badge again",
        long: "Read span.badge after the delete: still \"5 notes\", the list shows 4",
    },
    Step {
        short: "report to the lead",
        long: "Reported to the lead: the badge is stale after a delete, see the console warning",
    },
];

pub const FIRST_STEP: usize = 2;
pub const DELETE_STEP: usize = 2;
pub const TARGET: &str = "Call the landlord";

pub const NOTES: [(&str, &str); 5] = [
    ("Buy oat milk", "09:12"),
    ("Call the landlord", "10:40"),
    ("Read chapter 4", "12:20"),
    ("Pay the internet", "13:05"),
    ("Ask about the deposit", "11:02"),
];

pub const CONSOLE: [(&str, &str, Option<&str>, bool); 4] = [
    ("16:44:02", "[vite] connected.", None, false),
    ("16:44:09", "DELETE /api/notes/2 ", Some("204"), false),
    (
        "16:44:09",
        "useCount: stale value 5, refetch skipped (cache 30s)",
        None,
        true,
    ),
    ("16:44:10", "GET /api/notes ", Some("200"), false),
];

pub const CONSOLE_TAIL: &str = " · 4 rows";

#[derive(Clone, Copy, PartialEq, Eq)]
pub enum Engine {
    Built,
    Chrome,
    Chromium,
}

impl Engine {
    pub const ALL: [Engine; 3] = [Engine::Built, Engine::Chrome, Engine::Chromium];

    pub fn label(self) -> &'static str {
        match self {
            Engine::Built => "Built in",
            Engine::Chrome => "Your Chrome",
            Engine::Chromium => "Chromium",
        }
    }

    pub fn line(self) -> &'static str {
        match self {
            Engine::Built => "built in · WebView2 · its own profile",
            Engine::Chrome => "your Chrome · through the relay",
            Engine::Chromium => "tofu's Chromium · clean profile",
        }
    }
}

pub const TELL_CLOSE_EDITOR: &str =
    "Closes the editor workspace; its tiles are kept and come back with ctrl shift t.";
pub const TELL_CLOSE_DATA: &str =
    "Closes the data workspace; its tiles are kept and come back with ctrl shift t.";
pub const TELL_NEW_WORKSPACE: &str = "Opens a new workspace with one empty tile: pick a module, or start from a preset (work 1+2, editor, data).";
pub const TELL_PALETTE: &str = "Opens the command palette: files, sessions, screens, settings and commands in one search (alt k).";
pub const TELL_MINIMIZE: &str = "Minimizes tofu to the taskbar.";
pub const TELL_MAXIMIZE: &str =
    "Maximizes the window. Hold the pointer here for the Windows snap layouts.";
pub const TELL_CLOSE: &str =
    "Closes the window. clear-sable-eagle is still working, so tofu asks before it stops the turn.";
pub const TELL_NEW_SESSION: &str =
    "Starts a new session in notes-app and opens its chat in the work workspace.";
pub const TELL_SWITCH: &str =
    "Switches the desk to quiet-amber-heron: its chat and feeds replace these, the tabs stay.";
pub const TELL_CRON: &str =
    "One scheduled job: quiet-amber-heron runs its loop every 10 minutes. Opens the schedule.";
pub const TELL_MENTION_STEP: &str =
    "Adds this browser step to the chat as a reference: the URL, the element and a screenshot.";
pub const TELL_ATTACH: &str =
    "Attaches a file or an image; it goes to the lead as a reference, like an @ mention.";
pub const TELL_MODEL: &str =
    "Picks the lead's model from your accounts; the change applies from the next turn.";
pub const TELL_EFFORT: &str = "Cycles the effort: low, medium, high.";
pub const TELL_BACK: &str = "Goes back one page in this tile.";
pub const TELL_FORWARD: &str = "Goes forward one page in this tile.";
pub const TELL_RELOAD: &str = "Reloads the page. When vite dev restarts, tofu reloads it for you.";
pub const TELL_HAND_TAB: &str = "Lists your Chrome tabs; the one you pick becomes this tile. Tabs you do not pick stay untouched.";
pub const TELL_COPY_SELECTOR: &str = "Copies a selector for this element that survives a reload.";
pub const TELL_CHANGELOG: &str = "Opens the notes for this version in a Changelog screen.";
pub const TELL_DOCS: &str = "Opens the tofu docs in the Browser module, in a new workspace.";
pub const TELL_LINK: &str = "Opens another board; this screen draws the browser alone.";
pub const TELL_REOPEN: [(&str, &str, &str); 3] = [
    (
        "fond-sandy-mink",
        "2h",
        "Reopens fond-sandy-mink where it stopped; nothing runs until you send.",
    ),
    (
        "tidy-ochre-wren",
        "1d",
        "Reopens tidy-ochre-wren where it stopped; nothing runs until you send.",
    ),
    (
        "crisp-azure-swift",
        "3d",
        "Reopens crisp-azure-swift where it stopped; nothing runs until you send.",
    ),
];
