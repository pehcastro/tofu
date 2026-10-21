#[cfg(feature = "screen-boardy")]
use crate::modules::chat::BoardyMoved;
use crate::modules::chat::Find;
#[cfg(all(
    feature = "screen-work",
    any(feature = "screen-settings", feature = "screen-boardy")
))]
use crate::modules::chat::SettingsMoved;
#[cfg(feature = "screen-work")]
use crate::modules::chat::{Chat, Listed, TofuStopped, Touched};
use crate::pick;
#[cfg(feature = "screen-work")]
use crate::project::{self, Head, Picked};
#[cfg(all(feature = "screen-work", feature = "screen-accounts"))]
use crate::screens::accounts::Accounts as AccountsScreen;
#[cfg(feature = "screen-boardy")]
use crate::screens::boardy::Boardy as BoardyScreen;
#[cfg(all(feature = "screen-work", feature = "screen-classifier"))]
use crate::screens::classifier::Classifier;
#[cfg(all(feature = "screen-work", feature = "screen-context"))]
use crate::screens::context::ContextScreen;
#[cfg(all(feature = "screen-work", feature = "screen-forks"))]
use crate::screens::forks::ForksScreen;
#[cfg(all(feature = "screen-work", feature = "screen-library"))]
use crate::screens::library::Library;
#[cfg(all(feature = "screen-work", feature = "screen-limits"))]
use crate::screens::limits::Limits;
#[cfg(all(feature = "screen-work", feature = "screen-session"))]
use crate::screens::session::SessionScreen;
#[cfg(all(feature = "screen-work", feature = "screen-settings"))]
use crate::screens::settings::Rows as SettingsScreen;
#[cfg(all(feature = "screen-work", feature = "screen-usage"))]
use crate::screens::usage::UsageScreen;
#[cfg(feature = "screen-work")]
use crate::screens::work::split::timed;
#[cfg(feature = "screen-work")]
use crate::screens::work::{self, Work};
use crate::status_bar::status_bar;
use crate::title_bar::{self, Bell, ask_github, github, title_bar};
#[cfg(feature = "screen-work")]
use desk_core::boardy::Gate;
use desk_core::control::{Control, TELL_BADGE};
use desk_core::limits::TOAST_LIFETIME;
#[cfg(feature = "screen-work")]
use desk_core::model::Store;
#[cfg(feature = "screen-work")]
use desk_core::protocol::{
    AccountStateChanged, AccountStateChangedState, AccountStatusState, Accounts as AccountList,
    CronJob, QuotaWindow, Role, WindowStatus,
};
#[cfg(feature = "screen-boardy")]
use desk_core::protocol::{NoParams, request::QuerySettings};
#[cfg(feature = "screen-work")]
use desk_core::query::Answer;
#[cfg(feature = "screen-boardy")]
use desk_core::settings::Settings;
#[cfg(feature = "screen-work")]
use desk_tiling::WORKSPACE_EDGE;
use desk_tiling::{Action, Key, SHORTCUTS};
use desk_ui::components::empty::{EmptyAction, empty_state};
#[cfg(feature = "screen-work")]
use desk_ui::components::form::TextInput;
use desk_ui::components::glyph::Glyph;
use desk_ui::components::keymap::editor_claims;
use desk_ui::components::overlay::toast;
#[cfg(feature = "screen-work")]
use desk_ui::components::overlay::{Align, MenuButton, MenuItem, Placement, Side, context_menu};
use desk_ui::components::paint::{ink, ring};
use desk_ui::components::palette::{Palette, PaletteEntry, PaletteItem};
#[cfg(feature = "screen-work")]
use desk_ui::components::sheet::Sheet;
#[cfg(feature = "screen-work")]
use desk_ui::components::sidebar::SessionState;
use desk_ui::components::sidebar::{Project, SIDEBAR_COLUMN, Sidebar, SidebarPick};
#[cfg(feature = "screen-work")]
use desk_ui::components::sidebar::{SessionAt, unnamed};
use desk_ui::components::size::T2;
#[cfg(feature = "screen-work")]
use desk_ui::components::status_bar::{
    Account, Branch, ContextUse, Reset, Serving, SessionGroup, Standing, UsageWindow, cron_trigger,
};
use desk_ui::components::status_bar::{Accounts, Status};
#[cfg(feature = "screen-work")]
use desk_ui::components::tabs::{ScreenTabMenu, Tab, TabFlag, TabMark};
#[cfg(feature = "screen-work")]
use desk_ui::components::tiling_board::Reopened;
use desk_ui::components::title_bar::{Github, Marks, TitleBar};
#[cfg(feature = "screen-work")]
use desk_ui::components::tree::OpenProject;
#[cfg(feature = "screen-work")]
use desk_ui::icon::Icon;
use desk_ui::live::ActiveTheme;
use desk_ui::metrics::{
    STATUS_BAR_HEIGHT, TEXT, TITLE_BAR_HEIGHT, TOAST_BOTTOM, WINDOW_HEIGHT, WINDOW_MIN_HEIGHT,
    WINDOW_MIN_WIDTH, WINDOW_WIDTH,
};
use desk_ui::theme::{ColorToken, Mode, Theme, WordToken};
#[cfg(feature = "screen-work")]
use gpui::EntityInputHandler;
use gpui::{
    AnyElement, AnyView, App, ClickEvent, Context, Entity, FocusHandle, IntoElement, KeyDownEvent,
    Pixels, Render, SharedString, Size, StyleRefinement, Task, TitlebarOptions, WeakEntity, Window,
    WindowBounds, WindowOptions, div, prelude::*, px, size,
};
#[cfg(feature = "screen-work")]
use gpui::{ClipboardItem, Focusable, Point};
#[cfg(feature = "screen-work")]
use std::path::PathBuf;
#[cfg(feature = "screen-work")]
use std::time::Instant;

#[cfg(feature = "screen-work")]
#[path = "states/tofu_stopped.rs"]
mod tofu_stopped;

pub const WINDOW_TITLE: &str = "Tofu Desk";
pub const BOARD_VIEWPORT_WIDTH: f32 = 1440.0;
pub const BOARD_VIEWPORT_HEIGHT: f32 = 900.0;
const GUTTER: f32 = 8.0;
const TILE_TOP: f32 = 2.0;
const TILE_BOTTOM: f32 = 8.0;
const WINDOW_RADIUS: f32 = 8.0;
const WINDOW_RING: f32 = 0.1;
const SETTINGS: &str = "settings";
const LIMITS_SCREEN: &str = "limits";
const WHOLE_SCREENS: [&str; 3] = ["intro", "onboarding", "no-tofu"];
const SCREEN_ID: &str = "screen.";
const LAYOUT_ID: &str = "layout.";
const OPEN_SETTINGS_ID: &str = "settings.open";
#[cfg(feature = "screen-work")]
const REOPEN_ID: &str = "tab.reopen";
#[cfg(feature = "screen-work")]
const WORK_SCREEN: &str = "work";
#[cfg(feature = "screen-work")]
const EDITOR_SCREEN: &str = "editor";
#[cfg(feature = "screen-work")]
const BOARDY_SCREEN: &str = "boardy";
#[cfg(feature = "screen-work")]
const SAVE_AFTER: std::time::Duration = std::time::Duration::from_millis(500);
#[derive(Clone, Copy, PartialEq, Eq)]
pub enum ScreenGroup {
    Top,
    Insights,
}

pub struct ScreenMark {
    pub name: &'static str,
    pub label: &'static str,
    pub glyph: Glyph,
    pub group: ScreenGroup,
}

const fn mark(
    name: &'static str,
    label: &'static str,
    glyph: Glyph,
    group: ScreenGroup,
) -> ScreenMark {
    ScreenMark {
        name,
        label,
        glyph,
        group,
    }
}

#[cfg(feature = "screen-work")]
#[path = "screens/git/mod.rs"]
pub mod git;

pub const SCREEN_MARKS: [ScreenMark; 13] = [
    mark("editor", "Editor", Glyph::Code, ScreenGroup::Top),
    mark("boardy", "Boardy", Glyph::Inbox, ScreenGroup::Top),
    mark("git", "Git", Glyph::Branch, ScreenGroup::Top),
    mark("library", "Library", Glyph::Book, ScreenGroup::Top),
    mark("settings", "Settings", Glyph::Settings, ScreenGroup::Top),
    mark("accounts", "Accounts", Glyph::Person, ScreenGroup::Top),
    mark("theme", "Theme", Glyph::Palette, ScreenGroup::Top),
    mark(
        "classifier",
        "Classifier",
        Glyph::Brain,
        ScreenGroup::Insights,
    ),
    mark("usage", "Usage", Glyph::BarChart, ScreenGroup::Insights),
    mark("limits", "Limits", Glyph::Gauge, ScreenGroup::Insights),
    mark("context", "Context", Glyph::Attach, ScreenGroup::Insights),
    mark("session", "Session", Glyph::Chat, ScreenGroup::Insights),
    mark("forks", "Forks", Glyph::Branch, ScreenGroup::Insights),
];

pub fn screen_mark(name: &str) -> Option<&'static ScreenMark> {
    SCREEN_MARKS.iter().find(|mark| mark.name == name)
}
#[cfg(feature = "screen-work")]
const CRON_PROMPT_CHARS: usize = 32;
#[cfg(feature = "screen-work")]
const MINUTES_PER_HOUR: i64 = 60;
#[cfg(feature = "screen-work")]
const MINUTES_PER_DAY: i64 = 1440;
#[cfg(feature = "screen-work")]
const NO_TOFU: &str = "no work screen runs tofu here";

#[cfg(feature = "screen-work")]
fn cron_button(cx: &mut App) -> Entity<MenuButton> {
    let button = MenuButton::new("Cron".into(), Vec::new(), cx);
    button.update(cx, |button, _| {
        button.placement(Placement {
            side: Side::Top,
            align: Align::End,
            ..Placement::below()
        });
    });
    button
}

#[cfg(feature = "screen-work")]
fn shortened(prompt: &str) -> String {
    match prompt.char_indices().nth(CRON_PROMPT_CHARS) {
        Some((cut, _)) => format!("{}…", prompt.get(..cut).unwrap_or(prompt)),
        None => prompt.to_owned(),
    }
}

pub type Open = fn(Option<&str>, &mut Window, &mut App) -> Result<AnyView, String>;

#[derive(Clone, Copy)]
pub struct Screen {
    pub name: &'static str,
    pub open: Open,
}

pub struct NoTofu {
    pub title: SharedString,
    pub line: SharedString,
    pub command: SharedString,
}

impl Render for NoTofu {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let command = self.command.clone();
        let copy = EmptyAction {
            label: "Copy the command".into(),
            glyph: None,
            keys: None,
        };
        let panel = empty_state(
            "no-tofu",
            self.title.clone(),
            Some(self.line.clone()),
            &[copy],
            &[],
            &theme,
            move |_, _, cx| {
                cx.write_to_clipboard(gpui::ClipboardItem::new_string(command.to_string()));
                eprintln!("desk: no-tofu copied the command");
            },
        )
        .flex_col()
        .gap_3()
        .child(
            div()
                .font_family(theme.word(WordToken::ShapeMono))
                .text_color(ink(&theme, T2))
                .child(self.command.clone()),
        );
        div()
            .flex_1()
            .min_h_0()
            .size_full()
            .flex()
            .items_center()
            .justify_center()
            .child(panel)
    }
}

enum Body {
    #[cfg(feature = "screen-work")]
    Work(Entity<Work>),
    View(AnyView),
    Whole(AnyView),
}

struct Shown {
    name: SharedString,
    body: Body,
}

#[cfg(feature = "screen-work")]
struct ClosedScreen {
    name: &'static str,
    shown: Shown,
    tab: usize,
    at: Instant,
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum Part {
    Title,
    Sidebar,
    Status,
}

struct Chrome {
    desk: WeakEntity<Desk>,
    part: Part,
    _watch: [gpui::Subscription; 2],
}

impl Render for Chrome {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let part = self.part;
        self.desk
            .update(cx, |desk, cx| desk.chrome_body(part, cx))
            .unwrap_or_else(|error| {
                eprintln!("desk: chrome outlived the desk: {error}");
                div().into_any_element()
            })
    }
}

pub struct Desk {
    chrome: Vec<Entity<Chrome>>,
    sidebar_open: bool,
    toast: Option<Toast>,
    toasts: u64,
    branch_open: bool,
    screens: Vec<Screen>,
    shown: Shown,
    parked: Vec<Shown>,
    palette: Entity<Palette>,
    focus: FocusHandle,
    bell: Bell,
    marks: Marks,
    github: Github,
    #[cfg(feature = "screen-work")]
    projects: Projects,
    #[cfg(feature = "screen-work")]
    tabbed: Vec<&'static str>,
    #[cfg(feature = "screen-work")]
    expanded_in: Vec<(&'static str, SharedString)>,
    #[cfg(feature = "screen-work")]
    closed_screens: Vec<ClosedScreen>,
    #[cfg(feature = "screen-work")]
    held: Vec<Held>,
    #[cfg(feature = "screen-work")]
    menu_screen: Option<&'static str>,
    #[cfg(feature = "screen-work")]
    menu_at: Option<Point<Pixels>>,
    #[cfg(feature = "screen-work")]
    cron: Entity<MenuButton>,
    #[cfg(feature = "screen-work")]
    gate: Option<Gate>,
}

#[cfg(feature = "screen-work")]
struct Held {
    name: &'static str,
    locked: bool,
}

#[cfg(feature = "screen-work")]
#[derive(Default)]
struct Projects {
    head: Option<Head>,
    recents: Vec<PathBuf>,
    menu: Option<Entity<Palette>>,
    switching: Option<PathBuf>,
    confirming: bool,
    active: Vec<String>,
    renaming: Option<Renaming>,
    refused: Option<Option<SharedString>>,
    restored: bool,
    restoring: Option<String>,
    resumed: Option<String>,
    named: Option<SharedString>,
    written: Option<project::Remembered>,
    stopped: Option<TofuStopped>,
    _watch: Vec<gpui::Subscription>,
    _head: Option<Task<()>>,
    _reading: Option<Task<()>>,
    _saving: Option<Task<()>>,
}

#[cfg(feature = "screen-work")]
struct Renaming {
    id: String,
    at: SessionAt,
    input: Entity<TextInput>,
}

pub struct Toast {
    pub id: u64,
    pub title: SharedString,
    pub body: SharedString,
    pub source: &'static str,
    pub at: String,
    badge: Option<&'static str>,
    _expiry: Option<Task<gpui::Result<()>>>,
}

pub enum Unpicked {
    Off(String),
    Failed(String),
}

pub fn desk_client() -> Size<Pixels> {
    size(px(WINDOW_WIDTH), px(WINDOW_HEIGHT))
}

pub fn board_client() -> Size<Pixels> {
    size(px(BOARD_VIEWPORT_WIDTH), px(BOARD_VIEWPORT_HEIGHT))
}

pub fn window_options(title: SharedString, client: Size<Pixels>, cx: &App) -> WindowOptions {
    let mut options = WindowOptions::new()
        .window_bounds(Some(WindowBounds::centered(client, cx)))
        .window_min_size(Some(size(px(WINDOW_MIN_WIDTH), px(WINDOW_MIN_HEIGHT))))
        .titlebar(Some(TitlebarOptions {
            title: Some(title),
            appears_transparent: true,
            traffic_light_position: None,
        }));
    #[cfg(target_os = "windows")]
    {
        options.windows_window_background = gpui::WindowsWindowBackground::Blurred;
    }
    #[cfg(target_os = "macos")]
    {
        options.macos_window_background = gpui::MacosWindowBackground::Blurred;
    }
    #[cfg(any(target_os = "linux", target_os = "freebsd"))]
    {
        options.linux_window_background = gpui::LinuxWindowBackground::Blurred;
    }
    options
}

impl Shown {
    fn new(name: SharedString, view: AnyView) -> Self {
        #[cfg(feature = "screen-work")]
        let view = match view.downcast::<Work>() {
            Ok(work) => {
                return Shown {
                    name,
                    body: Body::Work(work),
                };
            }
            Err(view) => view,
        };
        let body = match WHOLE_SCREENS.contains(&&*name) {
            true => Body::Whole(view),
            false => Body::View(view),
        };
        Shown { name, body }
    }
}

#[cfg(not(feature = "screen-work"))]
fn timed(_: &'static str, child: impl IntoElement) -> AnyElement {
    child.into_any_element()
}

#[cfg(feature = "screen-work")]
fn thousands(tokens: i64) -> String {
    match tokens {
        ..1000 => tokens.to_string(),
        _ => format!("{}k", tokens / 1000),
    }
}

#[cfg(feature = "screen-work")]
struct Room {
    left: f64,
    resets: Option<chrono::DateTime<chrono::FixedOffset>>,
    out: bool,
    id: i64,
}

#[cfg(feature = "screen-work")]
fn room(
    state: &AccountStatusState,
    id: i64,
    windows: &[WindowStatus],
    now: chrono::DateTime<chrono::Local>,
) -> Room {
    let tightest = windows
        .iter()
        .map(|window| {
            let resets = window
                .resets_at
                .as_deref()
                .and_then(|stamp| chrono::DateTime::parse_from_rfc3339(stamp).ok());
            let left = match resets {
                Some(at) if at <= now => 1.0,
                _ => 1.0 - window.used,
            };
            (left, resets)
        })
        .min_by(|a, b| a.0.total_cmp(&b.0).then_with(|| sooner(a.1, b.1)));
    let (left, resets) = tightest.unwrap_or((1.0, None));
    Room {
        left,
        resets,
        out: matches!(
            state,
            AccountStatusState::Spent | AccountStatusState::RateLimited
        ) || left <= 0.0,
        id,
    }
}

#[cfg(feature = "screen-work")]
fn sooner(
    a: Option<chrono::DateTime<chrono::FixedOffset>>,
    b: Option<chrono::DateTime<chrono::FixedOffset>>,
) -> std::cmp::Ordering {
    match (a, b) {
        (Some(a), Some(b)) => a.cmp(&b),
        (Some(_), None) => std::cmp::Ordering::Less,
        (None, Some(_)) => std::cmp::Ordering::Greater,
        (None, None) => std::cmp::Ordering::Equal,
    }
}

#[cfg(feature = "screen-work")]
fn predicted_account(accounts: &[(Account, Room)], source: Option<&str>) -> Serving {
    let candidates: Vec<(usize, &Account, &Room)> = accounts
        .iter()
        .enumerate()
        .filter(|(_, (account, _))| source.is_none_or(|source| *account.source == *source))
        .map(|(at, (account, room))| (at, account, room))
        .collect();
    let name = |account: &Account| {
        account
            .email
            .as_ref()
            .and_then(|email| email.split('@').next())
            .map_or_else(|| account.provider.to_string(), str::to_owned)
    };
    let (Some(first), Some(_)) = (candidates.first(), source) else {
        return match source {
            Some(source) => Serving::Key(display_name(source).into()),
            None => candidates
                .iter()
                .filter(|(_, _, room)| !room.out)
                .min_by(|a, b| ranked(a.2, b.2))
                .map_or(Serving::Unpicked, |(at, account, _)| Serving::Account {
                    at: *at,
                    why: format!(
                        "{}, most room of every subscription, no model picked yet",
                        name(account)
                    )
                    .into(),
                    spent: false,
                }),
        };
    };
    let provider = first.1.provider.clone();
    let count = candidates.len();
    let open = candidates
        .iter()
        .filter(|(_, _, room)| !room.out)
        .min_by(|a, b| ranked(a.2, b.2));
    match open {
        Some((at, account, _)) => Serving::Account {
            at: *at,
            why: match count {
                1 => format!("{}, the only {provider} account", name(account)),
                _ => format!(
                    "{}, most room of {count} {provider} accounts",
                    name(account)
                ),
            }
            .into(),
            spent: false,
        },
        None => {
            let (at, account, _) = candidates
                .iter()
                .min_by(|a, b| sooner(a.2.resets, b.2.resets).then(a.2.id.cmp(&b.2.id)))
                .unwrap_or(first);
            Serving::Account {
                at: *at,
                why: format!(
                    "every {provider} account is spent or rate limited, {} resets soonest",
                    name(account)
                )
                .into(),
                spent: true,
            }
        }
    }
}

#[cfg(feature = "screen-work")]
fn ranked(a: &Room, b: &Room) -> std::cmp::Ordering {
    b.left
        .total_cmp(&a.left)
        .then_with(|| sooner(a.resets, b.resets))
        .then(a.id.cmp(&b.id))
}

#[cfg(feature = "screen-work")]
fn accounts(
    answer: &Answer<AccountList>,
    quota: &[QuotaWindow],
    source: Option<&str>,
) -> (Accounts, Serving, Vec<i64>) {
    let Some(read) = &answer.read else {
        let accounts = match &answer.failed {
            Some(error) => Accounts::Unread(error.to_string().into()),
            None => Accounts::Reading,
        };
        return (accounts, Serving::Unpicked, Vec::new());
    };
    let now = chrono::Local::now();
    let mut accounts: Vec<(Account, Room)> = read
        .value
        .subscriptions
        .iter()
        .filter(|source| source.role == Role::Llm)
        .flat_map(|source| {
            source.accounts.iter().map(|account| {
                let live = account.live(&source.source, quota);
                let shown = Account {
                    provider: display_name(&source.source).into(),
                    email: account.email().map(|email| email.to_owned().into()),
                    source: source.source.clone().into(),
                    plan: account.plan.clone().map(Into::into),
                    standing: standing(&account.state),
                    windows: live
                        .iter()
                        .map(|window| UsageWindow {
                            label: window.id.clone().into(),
                            percent: (window.used * 100.0).round().clamp(0.0, 100.0) as u8,
                            reset: window
                                .resets_at
                                .as_deref()
                                .and_then(|stamp| resets(stamp, now)),
                        })
                        .collect(),
                };
                (shown, room(&account.state, account.id, &live, now))
            })
        })
        .collect();
    accounts.sort_by_key(|(account, _)| {
        (
            Some(&*account.source) != source,
            !matches!(account.standing, Standing::Serving),
        )
    });
    let serving = predicted_account(&accounts, source);
    let ids = accounts.iter().map(|(_, room)| room.id).collect();
    (
        Accounts::Read(accounts.into_iter().map(|(account, _)| account).collect()),
        serving,
        ids,
    )
}

#[cfg(feature = "screen-work")]
fn display_name(source: &str) -> String {
    let words: Vec<String> = source
        .split('-')
        .map(|word| {
            let mut chars = word.chars();
            chars
                .next()
                .map(|first| first.to_uppercase().chain(chars).collect())
                .unwrap_or_default()
        })
        .collect();
    words.join(" ")
}

#[cfg(feature = "screen-work")]
fn standing(state: &AccountStatusState) -> Standing {
    let word = || String::from(state.clone()).replace('_', " ").into();
    match state {
        AccountStatusState::InUse | AccountStatusState::Standby => Standing::Serving,
        AccountStatusState::RefreshFailed
        | AccountStatusState::Expired
        | AccountStatusState::Refused => Standing::Reauth(word()),
        AccountStatusState::SetAside
        | AccountStatusState::Spent
        | AccountStatusState::RateLimited
        | AccountStatusState::Unread
        | AccountStatusState::Unchecked
        | AccountStatusState::Unknown(_) => Standing::Trouble(word()),
    }
}

#[cfg(feature = "screen-work")]
fn held(notice: &AccountStateChanged, ids: &[i64], accounts: &mut Accounts, serving: &mut Serving) {
    let word = match &notice.state {
        AccountStateChangedState::Serving => return,
        AccountStateChangedState::RateLimited => "rate limited",
        AccountStateChangedState::Spent => "spent",
        AccountStateChangedState::Unknown(raw) => {
            return eprintln!("desk: account.state {raw} is not a state the desk knows");
        }
    };
    let (Accounts::Read(read), Serving::Account { at, why, spent }) = (accounts, serving) else {
        return;
    };
    if ids.get(*at) != Some(&notice.account_id) {
        return;
    }
    let Some(account) = read
        .get_mut(*at)
        .filter(|account| *account.source == notice.source)
    else {
        return;
    };
    let retry = notice
        .retry_at
        .as_deref()
        .and_then(|stamp| resets(stamp, chrono::Local::now()));
    let said = match &retry {
        Some(reset) => format!("{word}, retry {}", reset.left),
        None => word.to_owned(),
    };
    *why = match retry {
        Some(reset) => format!("{said}. {}", reset.at),
        None => said.clone(),
    }
    .into();
    account.standing = Standing::Trouble(said.into());
    *spent = true;
}

#[cfg(feature = "screen-work")]
fn running(store: &Store) -> Vec<&str> {
    store
        .sessions
        .iter()
        .filter(|(_, session)| session.turns.values().any(|turn| turn.status.is_none()))
        .map(|(id, _)| id.as_str())
        .collect()
}

#[cfg(feature = "screen-work")]
pub(crate) fn resets(stamp: &str, now: chrono::DateTime<chrono::Local>) -> Option<Reset> {
    let at = chrono::DateTime::parse_from_rfc3339(stamp)
        .ok()?
        .with_timezone(&chrono::Local);
    let left = match (at - now).num_minutes() {
        ..=0 => "now".to_owned(),
        left @ ..MINUTES_PER_HOUR => format!("in {left}m"),
        left @ ..MINUTES_PER_DAY => format!(
            "in {}h {}m",
            left / MINUTES_PER_HOUR,
            left % MINUTES_PER_HOUR
        ),
        left => format!("in {}d", left / MINUTES_PER_DAY),
    };
    Some(Reset {
        left: left.into(),
        at: format!("Resets {}", at.format("%a %-d %b, %H:%M")).into(),
    })
}

fn commands(screens: &[Screen]) -> Vec<PaletteItem> {
    let screens = screens.iter().map(|screen| PaletteItem {
        id: format!("{SCREEN_ID}{}", screen.name).into(),
        label: screen_mark(screen.name)
            .map_or(screen.name, |mark| mark.label)
            .into(),
        group: "Screens".into(),
        keys: None,
    });
    let layout = SHORTCUTS
        .iter()
        .filter(|shortcut| shortcut.key != Key::Digit && shortcut.action != Action::ReopenTab)
        .map(|shortcut| PaletteItem {
            id: format!("{LAYOUT_ID}{}", shortcut.label).into(),
            label: shortcut.label.into(),
            group: "Layout".into(),
            keys: Some(shortcut.keys.into()),
        });
    let settings = PaletteItem {
        id: OPEN_SETTINGS_ID.into(),
        label: "Open settings".into(),
        group: "Settings".into(),
        keys: Some("Ctrl ,".into()),
    };
    let mut items: Vec<PaletteItem> = screens.chain(layout).collect();
    #[cfg(feature = "screen-work")]
    items.push(PaletteItem {
        id: REOPEN_ID.into(),
        label: "Reopen closed tab".into(),
        group: "Tabs".into(),
        keys: Some("Ctrl Shift T".into()),
    });
    items.push(settings);
    items
}

impl Desk {
    #[cfg_attr(
        not(feature = "screen-work"),
        expect(unused_variables, reason = "only the work screen restores a project")
    )]
    pub fn new(
        screens: Vec<Screen>,
        (name, named): (SharedString, bool),
        view: AnyView,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> Self {
        #[cfg(feature = "screen-work")]
        let screens: Vec<Screen> = screens
            .into_iter()
            .filter(|screen| screen.name != "git")
            .chain([Screen {
                name: "git",
                open: git::open,
            }])
            .collect();
        let named = named.then(|| name.clone());
        let palette = Palette::new(commands(&screens), window, cx);
        let picked = cx.listener(|desk, id: &SharedString, window, cx| {
            eprintln!("desk: palette picked {id}");
            if let Err(Unpicked::Off(why) | Unpicked::Failed(why)) = desk.pick(id, window, cx) {
                eprintln!("desk: palette: {why}");
            }
        });
        let marks = SCREEN_MARKS
            .iter()
            .map(|mark| (format!("{SCREEN_ID}{}", mark.name).into(), mark.glyph))
            .collect();
        palette.update(cx, |palette, _| {
            palette.on_pick(picked);
            palette.on_close(|_, _| eprintln!("desk: palette closed"));
            palette.marks(marks);
        });
        #[cfg_attr(
            not(feature = "screen-work"),
            expect(unused_mut, reason = "only the work screen adopts a project")
        )]
        let mut desk = Desk {
            chrome: Vec::new(),
            sidebar_open: true,
            toast: None,
            toasts: 0,
            branch_open: false,
            screens,
            shown: Shown::new(name, view),
            parked: Vec::new(),
            palette,
            focus: cx.focus_handle(),
            bell: Bell::default(),
            marks: title_bar::marks(),
            github: Github::Asking,
            #[cfg(feature = "screen-work")]
            projects: Projects {
                named,
                ..Projects::default()
            },
            #[cfg(feature = "screen-work")]
            tabbed: Vec::new(),
            #[cfg(feature = "screen-work")]
            expanded_in: Vec::new(),
            #[cfg(feature = "screen-work")]
            closed_screens: Vec::new(),
            #[cfg(feature = "screen-work")]
            held: Vec::new(),
            #[cfg(feature = "screen-work")]
            menu_screen: None,
            #[cfg(feature = "screen-work")]
            menu_at: None,
            #[cfg(feature = "screen-work")]
            cron: cron_button(cx),
            #[cfg(feature = "screen-work")]
            gate: None,
        };
        #[cfg(feature = "screen-work")]
        desk.adopt_launched(window, cx);
        #[cfg(feature = "screen-work")]
        desk.park_editor(window, cx);
        #[cfg(feature = "screen-work")]
        {
            let closing = cx.weak_entity();
            window.on_window_should_close(cx, move |_, cx| {
                if let Err(error) = closing.update(cx, |desk, cx| desk.save_now(cx)) {
                    eprintln!("desk: the project state was not saved at close: {error}");
                }
                true
            });
        }
        #[cfg(feature = "screen-intro")]
        if let Body::Whole(view) = &desk.shown.body
            && let Ok(intro) = view.clone().downcast::<crate::screens::intro::Intro>()
        {
            cx.observe_in(&intro, window, |desk, _, window, cx| {
                desk.leave_intro(window, cx)
            })
            .detach();
        }
        desk.focus_shown(window, cx);
        cx.spawn(async move |this, cx| {
            let answer = cx.background_executor().spawn(async { ask_github() }).await;
            let told = this.update(cx, |desk, cx| {
                desk.github = github(answer);
                cx.notify();
            });
            if let Err(error) = told {
                eprintln!("desk: gh answered after the desk closed: {error}");
            }
        })
        .detach();
        desk
    }

    pub fn open_limits(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        self.show(LIMITS_SCREEN, window, cx);
    }

    pub fn open_palette(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        eprintln!("desk: palette open");
        self.palette
            .update(cx, |palette, cx| palette.open(window, cx));
    }

    pub fn route(
        &mut self,
        name: &'static str,
        view: AnyView,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        let left = std::mem::replace(&mut self.shown, Shown::new(name.into(), view));
        self.parked.push(left);
        self.focus_shown(window, cx);
        cx.notify();
    }

    fn show(&mut self, name: &str, window: &mut Window, cx: &mut Context<Self>) {
        if let Err(error) = self.open_screen(name, window, cx) {
            eprintln!("desk: {error}");
        }
    }

    pub fn open_screen(
        &mut self,
        name: &str,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> Result<(), String> {
        if self.shown.name == name {
            eprintln!("desk: screen {name} already shows");
            return Ok(());
        }
        let next = match self.parked.iter().position(|parked| parked.name == name) {
            Some(at) => self.parked.swap_remove(at),
            None => {
                let Some(screen) = self.screens.iter().find(|screen| screen.name == name) else {
                    return Err(format!("screen {name} is not in this build"));
                };
                let view = (screen.open)(None, window, cx)
                    .map_err(|error| format!("screen {name} did not open: {error}"))?;
                #[cfg(all(feature = "screen-work", feature = "module-editor"))]
                Self::lend_chat(&view, cx);
                #[cfg(feature = "screen-work")]
                if screen.name != WORK_SCREEN {
                    self.tabbed.push(screen.name);
                }
                Shown::new(screen.name.into(), view)
            }
        };
        let left = std::mem::replace(&mut self.shown, next);
        self.parked.push(left);
        #[cfg(all(feature = "screen-work", feature = "screen-settings"))]
        if let Body::View(view) = &self.shown.body
            && let Ok(settings) = view.clone().downcast::<SettingsScreen>()
        {
            settings.update(cx, |settings, _| settings.showing());
        }
        #[cfg(feature = "screen-work")]
        self.adopt_launched(window, cx);
        #[cfg(all(
            feature = "screen-work",
            any(
                feature = "screen-limits",
                feature = "screen-classifier",
                feature = "screen-library",
                feature = "screen-context",
                feature = "screen-session",
                feature = "screen-forks",
                feature = "screen-usage",
                feature = "screen-accounts",
                feature = "screen-settings",
                feature = "screen-boardy"
            )
        ))]
        self.feed_screens(cx);
        self.focus_shown(window, cx);
        eprintln!("desk: screen {name}");
        #[cfg(feature = "screen-work")]
        self.remember_state(cx);
        cx.notify();
        Ok(())
    }

    #[cfg(feature = "screen-intro")]
    #[cfg_attr(
        not(feature = "screen-work"),
        expect(unused_variables, reason = "only the work screen resumes a session")
    )]
    fn leave_intro(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let Body::Whole(view) = &self.shown.body else {
            return;
        };
        let Ok(intro) = view.clone().downcast::<crate::screens::intro::Intro>() else {
            return;
        };
        if !intro.read(cx).left() {
            return;
        }
        #[cfg(feature = "screen-work")]
        if let Some((folder, session)) = intro.read(cx).resuming() {
            eprintln!("desk: intro resumes {session} in {}", folder.display());
            let from = intro.read(cx).chat().clone();
            self.projects.named = None;
            self.switch_now(folder, window, cx);
            self.parked
                .retain(|parked| !matches!(parked.body, Body::Whole(_)));
            self.tabbed.retain(|name| !WHOLE_SCREENS.contains(name));
            if let Some(work) = self.work() {
                let chat = work.read(cx).chat().clone();
                let handed = from.update(cx, |from, cx| {
                    chat.update(cx, |chat, cx| chat.take_tofu(from, cx))
                });
                eprintln!("desk: intro tofu handed to work {handed}");
            }
            self.projects.restoring = Some(session.clone());
            self.projects.resumed = Some(session);
            self.restore_session(cx);
            return cx.notify();
        }
        self.shown.body = Body::View(view.clone());
        eprintln!("desk: intro left for work, chrome back");
        cx.notify();
    }

    fn focus_shown(&self, window: &mut Window, cx: &mut App) {
        let focus = match &self.shown.body {
            #[cfg(feature = "screen-work")]
            Body::Work(work) => work.focus_handle(cx),
            Body::View(_) | Body::Whole(_) => self.focus.clone(),
        };
        focus.focus(window, cx);
    }

    pub fn palette_entries<'a>(&self, cx: &'a App) -> &'a [PaletteEntry] {
        self.palette.read(cx).items()
    }

    pub fn pick(
        &mut self,
        id: &str,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> Result<(), Unpicked> {
        if let Some(name) = id.strip_prefix(SCREEN_ID) {
            #[cfg(feature = "screen-work")]
            if let Some(off) = self.gated_off(name) {
                return Err(Unpicked::Off(off));
            }
            return self.open_screen(name, window, cx).map_err(Unpicked::Failed);
        }
        if id == OPEN_SETTINGS_ID {
            return self
                .open_screen(SETTINGS, window, cx)
                .map_err(Unpicked::Failed);
        }
        #[cfg(feature = "screen-work")]
        if id == REOPEN_ID {
            self.reopen(window, cx);
            return Ok(());
        }
        let action = id
            .strip_prefix(LAYOUT_ID)
            .and_then(|label| SHORTCUTS.iter().find(|shortcut| shortcut.label == label))
            .map(|shortcut| shortcut.action);
        match (action, &self.shown.body) {
            (None, _) => Err(Unpicked::Failed(format!(
                "{id} is not a command the desk knows"
            ))),
            #[cfg(feature = "screen-work")]
            (Some(action), Body::Work(work)) => {
                work.update(cx, |work, cx| work.run(action, "", window, cx));
                Ok(())
            }
            (Some(action), Body::View(_) | Body::Whole(_)) => Err(Unpicked::Failed(format!(
                "{action:?} needs the work screen, and {} shows",
                self.shown.name
            ))),
        }
    }

    #[cfg_attr(
        not(feature = "screen-work"),
        expect(
            unused_variables,
            reason = "only the work screen has a composer to focus"
        )
    )]
    fn escape(&mut self, event: &KeyDownEvent, window: &mut Window, cx: &mut Context<Self>) {
        if event.keystroke.key != "escape" {
            return;
        }
        match &self.shown.body {
            #[cfg(feature = "screen-work")]
            Body::Work(work) => {
                if work.update(cx, |work, cx| work.cancel_drag(cx)) {
                    return eprintln!("desk: escape: the drag is cancelled");
                }
                let focused = work.update(cx, |work, cx| work.focus_composer(window, cx));
                eprintln!("desk: escape: the composer has focus {focused}");
            }
            Body::View(_) | Body::Whole(_) => {
                eprintln!("desk: escape: {} has no composer", self.shown.name)
            }
        }
    }

    fn tiles_x(&self) -> f32 {
        if self.sidebar_open {
            SIDEBAR_COLUMN
        } else {
            GUTTER
        }
    }

    #[cfg(feature = "screen-work")]
    fn strip(&mut self, theme: &Theme, cx: &mut Context<Self>) -> Option<AnyElement> {
        let work = self.work()?.clone();
        let screens: Vec<Tab> = self
            .tabbed
            .iter()
            .map(|name| Tab {
                label: screen_mark(name).map_or(*name, |mark| mark.label).into(),
                icon: screen_mark(name).map(|mark| mark.glyph),
                count: None,
                mark: match self.held.iter().find(|held| held.name == *name) {
                    Some(Held { locked: true, .. }) => TabMark::Locked,
                    Some(Held { locked: false, .. }) => TabMark::Pinned,
                    None => TabMark::Close,
                },
                flag: self
                    .expanded_in
                    .iter()
                    .find(|(expanded, _)| expanded == name)
                    .map(|(_, workspace)| TabFlag {
                        icon: Icon::Expand,
                        tip: format!(
                            "Expanded view of {name} in {workspace}. Closing this tab will not close the tile there."
                        )
                        .into(),
                    }),
            })
            .collect();
        let openable: Vec<&'static str> = self
            .screens
            .iter()
            .map(|screen| screen.name)
            .filter(|name| screen_mark(name).is_some() && self.gated_off(name).is_none())
            .collect();
        let screen = self.tabbed.iter().position(|name| self.shown.name == *name);
        let (desk, picker) = (cx.weak_entity(), cx.weak_entity());
        let strip = work.update(cx, |work, cx| {
            work.tabs(
                &screens,
                &openable,
                screen,
                theme,
                move |strip, window, cx| {
                    desk.update(cx, |desk, cx| desk.stripped(strip, window, cx))
                        .unwrap_or_else(|_| eprintln!("desk: the desk is gone"));
                },
                cx,
            )
        });
        let held = self
            .menu_screen
            .and_then(|name| self.held.iter().find(|held| held.name == name));
        let (pinned, locked) = held.map_or((false, false), |held| (true, held.locked));
        let items = [
            Some(MenuItem::action(if pinned { "Unpin" } else { "Pin" }).icon(Glyph::Pin)),
            Some(MenuItem::action(if locked { "Unlock" } else { "Lock" }).icon(Glyph::Lock)),
            (!pinned).then(|| MenuItem::action("Close").icon(Icon::Close)),
        ]
        .into_iter()
        .flatten()
        .collect();
        Some(
            div()
                .flex_1()
                .min_w_0()
                .child(
                    context_menu(items)
                        .id("screen-tab-menu")
                        .open_at(self.menu_at.take())
                        .on_pick(move |pick, window, cx| {
                            picker
                                .update(cx, |desk, cx| desk.picked_screen(*pick, window, cx))
                                .unwrap_or_else(|_| eprintln!("desk: the desk is gone"));
                        })
                        .child(strip),
                )
                .into_any_element(),
        )
    }

    #[cfg(feature = "screen-work")]
    fn screen_menu(&mut self, asked: &ScreenTabMenu, _: &mut Window, cx: &mut Context<Self>) {
        let Some(name) = self.tabbed.get(asked.at).copied() else {
            return eprintln!("desk: screen tab {} is not open", asked.at);
        };
        self.menu_screen = Some(name);
        self.menu_at = Some(asked.position);
        eprintln!("desk: screen {name} menu");
        cx.notify();
    }

    #[cfg(feature = "screen-work")]
    fn picked_screen(&mut self, pick: usize, window: &mut Window, cx: &mut Context<Self>) {
        let Some(name) = self.menu_screen else {
            return eprintln!("desk: the screen menu has no tab");
        };
        let at = self.held.iter().position(|held| held.name == name);
        let locked = at
            .and_then(|at| self.held.get(at))
            .is_some_and(|held| held.locked);
        match (pick, at) {
            (0, Some(at)) => {
                self.held.remove(at);
            }
            (0, None) => self.held.push(Held {
                name,
                locked: false,
            }),
            (1, Some(at)) if locked => {
                self.held.remove(at);
            }
            (1, Some(at)) => {
                if let Some(held) = self.held.get_mut(at) {
                    held.locked = true;
                }
            }
            (1, None) => self.held.push(Held { name, locked: true }),
            (2, None) => return self.close_screen(name, window, cx),
            (pick, _) => return eprintln!("desk: screen menu has no item {pick}"),
        }
        let held = self.held.iter().find(|held| held.name == name);
        eprintln!(
            "desk: screen {name} pinned={} locked={}",
            held.is_some(),
            held.is_some_and(|held| held.locked)
        );
        self.remember_state(cx);
        cx.notify();
    }

    #[cfg(feature = "screen-work")]
    fn stripped(&mut self, strip: &work::Strip, window: &mut Window, cx: &mut Context<Self>) {
        let named = |at: &usize| self.tabbed.get(*at).copied();
        match strip {
            work::Strip::Work => self.show(WORK_SCREEN, window, cx),
            work::Strip::Show(at) => match named(at) {
                Some(name) => self.show(name, window, cx),
                None => eprintln!("desk: screen tab {at} is not open"),
            },
            work::Strip::Close(at) => match named(at) {
                Some(name) => self.close_screen(name, window, cx),
                None => eprintln!("desk: screen tab {at} is not open"),
            },
            work::Strip::Open(name) => self.show(name, window, cx),
        }
    }

    #[cfg(feature = "screen-work")]
    fn expand(&mut self, expanded: &work::Expanded, window: &mut Window, cx: &mut Context<Self>) {
        let name = expanded.name;
        if !self.tabbed.contains(&name) {
            self.closed_screens.retain(|closed| closed.name != name);
            self.tabbed.push(name);
            self.parked.push(Shown {
                name: name.into(),
                body: Body::View(expanded.view.clone()),
            });
            self.expanded_in.push((name, expanded.workspace.clone()));
        }
        self.show(name, window, cx);
    }

    #[cfg(feature = "screen-work")]
    fn close_screen(&mut self, name: &'static str, window: &mut Window, cx: &mut Context<Self>) {
        if self.shown.name == name {
            self.show(WORK_SCREEN, window, cx);
        }
        let tab = self.tabbed.iter().position(|tabbed| *tabbed == name);
        if let (Some(tab), Some(at)) = (
            tab,
            self.parked.iter().position(|parked| parked.name == name),
        ) {
            self.closed_screens.push(ClosedScreen {
                name,
                shown: self.parked.remove(at),
                tab,
                at: Instant::now(),
            });
        }
        self.tabbed.retain(|tabbed| *tabbed != name);
        self.expanded_in.retain(|(expanded, _)| *expanded != name);
        eprintln!("desk: screen {name} closed");
        self.remember_state(cx);
        cx.notify();
    }

    #[cfg(feature = "screen-work")]
    fn reopen(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let work = self.work().cloned();
        let board_at = work
            .as_ref()
            .and_then(|work| work.read(cx).last_closed_at());
        let screen_at = self.closed_screens.last().map(|closed| closed.at);
        if screen_at > board_at
            && let Some(closed) = self.closed_screens.pop()
        {
            self.tabbed
                .insert(closed.tab.min(self.tabbed.len()), closed.name);
            self.parked.push(closed.shown);
            eprintln!("desk: reopen screen {}", closed.name);
            return self.show(closed.name, window, cx);
        }
        let Some(work) = work.filter(|_| board_at.is_some()) else {
            return eprintln!("desk: reopen: nothing closed");
        };
        if self.shown.name != WORK_SCREEN {
            self.show(WORK_SCREEN, window, cx);
        }
        match work.update(cx, |work, cx| work.reopen(window, cx)) {
            Some(Reopened::Tile(name)) => eprintln!("desk: reopen tile {name}"),
            Some(Reopened::Workspace(name)) => eprintln!("desk: reopen workspace {name}"),
            None => eprintln!("desk: reopen: nothing closed"),
        }
    }

    #[cfg_attr(
        not(feature = "screen-work"),
        expect(
            unused_variables,
            reason = "only the work screen is fitted to the window"
        )
    )]
    fn content(&mut self, theme: &Theme, cx: &mut Context<Self>) -> AnyElement {
        match &self.shown.body {
            #[cfg(feature = "screen-work")]
            Body::Work(work) => {
                work.update(cx, |work, _| {
                    work.fit(TILE_BOTTOM + STATUS_BAR_HEIGHT - WORKSPACE_EDGE)
                });
                let stopped = self.projects.stopped.as_ref().map(|stopped| {
                    tofu_stopped::banner(
                        stopped,
                        theme,
                        cx.listener(|desk, _: &ClickEvent, _, cx| desk.restart_tofu(cx)),
                    )
                });
                div()
                    .flex_1()
                    .min_w_0()
                    .relative()
                    .child(work.clone())
                    .children(stopped)
                    .into_any_element()
            }
            Body::View(view) => div()
                .flex_1()
                .min_w_0()
                .flex()
                .child(view.clone())
                .into_any_element(),
            Body::Whole(view) => view.clone().into_any_element(),
        }
    }

    fn tiled(&mut self, content: AnyElement, cx: &mut Context<Self>) -> AnyElement {
        let sidebar = self.sidebar_open.then(|| self.chrome(Part::Sidebar, cx));
        let status = self.chrome(Part::Status, cx);
        div()
            .flex_1()
            .min_h_0()
            .flex()
            .flex_col()
            .child(
                div()
                    .flex_1()
                    .min_h_0()
                    .flex()
                    .pr(px(GUTTER))
                    .children(sidebar)
                    .when(!self.sidebar_open, |body| body.pl(px(GUTTER)))
                    .child(
                        div()
                            .flex_1()
                            .min_w_0()
                            .flex()
                            .pt(px(TILE_TOP))
                            .pb(px(TILE_BOTTOM))
                            .child(content),
                    ),
            )
            .child(status)
            .into_any_element()
    }

    fn chrome(&mut self, part: Part, cx: &mut Context<Self>) -> AnyElement {
        let made = self.chrome.iter().find(|made| made.read(cx).part == part);
        let made = match made {
            Some(made) => made.clone(),
            None => {
                let desk = cx.entity();
                let made = cx.new(|cx| Chrome {
                    desk: desk.downgrade(),
                    part,
                    _watch: [
                        cx.observe(&desk, |_, _, cx| cx.notify()),
                        cx.observe_global::<ActiveTheme>(|_, cx| cx.notify()),
                    ],
                });
                self.chrome.push(made.clone());
                made
            }
        };
        let (name, style) = match part {
            Part::Title => (
                "title bar",
                StyleRefinement::default()
                    .w_full()
                    .h(px(TITLE_BAR_HEIGHT))
                    .flex_none(),
            ),
            Part::Sidebar => (
                "sidebar",
                StyleRefinement::default()
                    .w(px(SIDEBAR_COLUMN))
                    .h_full()
                    .flex_none(),
            ),
            Part::Status => (
                "status bar",
                StyleRefinement::default()
                    .w_full()
                    .h(px(STATUS_BAR_HEIGHT))
                    .flex_none(),
            ),
        };
        timed(name, made.cached(style)).into_any_element()
    }

    fn chrome_body(&mut self, part: Part, cx: &mut Context<Self>) -> AnyElement {
        match part {
            Part::Title => {
                #[cfg(feature = "screen-work")]
                let tabs = self.strip(&ActiveTheme::theme(cx), cx);
                #[cfg(not(feature = "screen-work"))]
                let tabs = None;
                let whole = matches!(self.shown.body, Body::Whole(_));
                title_bar(
                    self.sidebar_open,
                    self.tiles_x(),
                    self.github.clone(),
                    &self.bell,
                    &self.marks,
                    tabs,
                    cx,
                )
                .when(whole, TitleBar::controls_only)
                .into_any_element()
            }
            Part::Sidebar => self.sidebar(cx).into_any_element(),
            Part::Status => {
                let status = self.status(cx);
                let cron = status.session_group.as_ref().map_or(0, |group| group.cron);
                let cron_menu = self.cron_menu(cron, cx);
                status_bar(&ActiveTheme::problems(cx), status, cx)
                    .cron_menu(cron_menu)
                    .branch_popover(
                        self.branch_open,
                        cx.listener(|desk, open: &bool, _, cx| desk.open_branch(*open, cx)),
                    )
                    .into_any_element()
            }
        }
    }

    pub fn toggle_theme(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let since = std::time::Instant::now();
        let mode = match ActiveTheme::mode(cx) {
            Mode::Dark => Mode::Light,
            Mode::Light => Mode::Dark,
        };
        let toggled = pick::chosen().and_then(|chosen| {
            pick::set_mode(&chosen.theme, Some(mode), cx)?;
            #[cfg(feature = "screen-theme")]
            pick::save(&chosen.theme, ActiveTheme::mode(cx))?;
            Ok(())
        });
        if let Err(error) = toggled {
            eprintln!("desk: theme toggle: {error}");
            return;
        }
        let label = mode.label();
        window.on_next_frame(move |_, _| {
            eprintln!(
                "desk: theme toggled to {label} in {} ms",
                since.elapsed().as_millis()
            );
        });
        cx.notify();
    }

    pub fn toggle_sidebar(&mut self, cx: &mut Context<Self>) {
        self.sidebar_open = !self.sidebar_open;
        let state = if self.sidebar_open { "open" } else { "closed" };
        eprintln!("desk: sidebar {state}");
        cx.notify();
    }

    fn global_key(&mut self, event: &KeyDownEvent, window: &mut Window, cx: &mut Context<Self>) {
        let stroke = &event.keystroke;
        let held = stroke.modifiers;
        if !held.control || held.alt || held.platform || editor_claims(event, window, cx) {
            return;
        }
        use desk_ui::components::keymap::{AppKey, app_key};
        match (app_key(stroke, cx), held.shift, stroke.key.as_str()) {
            #[cfg(feature = "screen-work")]
            (None, true, "t") => self.reopen(window, cx),
            (Some(AppKey::Sidebar), ..) => self.toggle_sidebar(cx),
            (Some(AppKey::Palette), ..) => self.open_palette(window, cx),
            (Some(AppKey::Find), ..) => window.dispatch_action(Box::new(Find), cx),
            (Some(AppKey::Settings), ..) => self.show(SETTINGS, window, cx),
            _ => return,
        }
        cx.stop_propagation();
    }

    pub fn read_notices(&mut self, cx: &mut Context<Self>) {
        self.bell.read();
        cx.notify();
    }

    pub fn tell(&mut self, control: Control, cx: &mut Context<Self>) {
        self.toast(control.tell().into(), Some(TELL_BADGE), cx);
    }

    pub fn open_branch(&mut self, open: bool, cx: &mut Context<Self>) {
        eprintln!("desk: status branch popover open {open}");
        self.branch_open = open;
        cx.notify();
    }

    fn toast(&mut self, text: SharedString, badge: Option<&'static str>, cx: &mut Context<Self>) {
        let expiry = cx.spawn(async move |this, cx| {
            cx.background_executor().timer(TOAST_LIFETIME).await;
            this.update(cx, |desk, cx| {
                desk.toast = None;
                cx.notify();
            })
        });
        self.post(text, SharedString::default(), badge, Some(expiry), cx);
    }

    pub fn notify(
        &mut self,
        title: SharedString,
        body: SharedString,
        cx: &mut Context<Self>,
    ) -> u64 {
        self.post(title, body, None, None, cx)
    }

    fn post(
        &mut self,
        title: SharedString,
        body: SharedString,
        badge: Option<&'static str>,
        expiry: Option<Task<gpui::Result<()>>>,
        cx: &mut Context<Self>,
    ) -> u64 {
        self.toasts += 1;
        self.toast = Some(Toast {
            id: self.toasts,
            title,
            body,
            source: if expiry.is_some() { "desk" } else { "control" },
            at: chrono::Local::now().to_rfc3339(),
            badge,
            _expiry: expiry,
        });
        cx.notify();
        self.toasts
    }

    pub fn shown_toast(&self) -> Option<&Toast> {
        self.toast.as_ref()
    }

    pub fn dismiss_toast(&mut self, id: Option<u64>, cx: &mut Context<Self>) -> bool {
        let shown = self
            .toast
            .as_ref()
            .is_some_and(|toast| id.is_none_or(|id| id == toast.id));
        if shown {
            self.toast = None;
            cx.notify();
        }
        shown
    }

    #[cfg(not(feature = "screen-work"))]
    fn pick_from_sidebar(&mut self, pick: &SidebarPick, _: &mut Window, cx: &mut Context<Self>) {
        match pick {
            SidebarPick::Project => self.tell(Control::OpenProject, cx),
            SidebarPick::NewSession
            | SidebarPick::Open(_)
            | SidebarPick::CopyId(_)
            | SidebarPick::Rename(_)
            | SidebarPick::Docs => eprintln!("desk: sidebar: {pick:?} needs the work screen"),
        }
    }

    fn sidebar(&self, cx: &mut Context<Self>) -> Sidebar {
        let sidebar = Sidebar::new(
            "sidebar",
            self.sidebar_project(cx),
            cx.listener(|desk, pick: &SidebarPick, window, cx| {
                desk.pick_from_sidebar(pick, window, cx)
            }),
        );
        #[cfg(feature = "screen-work")]
        if let Some(renaming) = &self.projects.renaming {
            let field = div()
                .on_key_down(cx.listener(Self::rename_key))
                .child(renaming.input.clone());
            return sidebar.editing(renaming.at, field.into_any_element());
        }
        sidebar
    }

    #[cfg(not(feature = "screen-work"))]
    pub fn open_notice(&mut self, _: usize, _: &mut Context<Self>) {}

    #[cfg(not(feature = "screen-work"))]
    fn sidebar_project(&self, _: &App) -> Option<Project> {
        None
    }

    #[cfg(not(feature = "screen-work"))]
    pub fn status(&self, _: &App) -> Status {
        Status {
            accounts: Accounts::Read(Vec::new()),
            ..Status::default()
        }
    }

    #[cfg(not(feature = "screen-work"))]
    pub fn sign_in(&mut self, source: &str, _: &mut Context<Self>) {
        eprintln!("desk: sign in {source}: this build has no work screen to run tofu");
    }

    #[cfg(not(feature = "screen-work"))]
    fn cron_menu(&self, _: usize, _: &mut Context<Self>) -> Option<AnyView> {
        None
    }

    #[cfg(not(feature = "screen-work"))]
    fn switch_sheet(&self, _: &mut Context<Self>) -> Option<AnyElement> {
        None
    }
}

#[cfg(feature = "screen-work")]
impl Desk {
    pub fn screen(&self) -> &str {
        &self.shown.name
    }

    pub fn open_screens(&self) -> Vec<&str> {
        std::iter::once(&self.shown)
            .chain(&self.parked)
            .map(|shown| &*shown.name)
            .collect()
    }

    pub fn gated_off(&self, name: &str) -> Option<String> {
        (name == BOARDY_SCREEN && !self.gate.is_some_and(|gate| gate.on)).then(|| {
            format!(
                "screen boardy is off in this project: set {} to boardy in Settings",
                desk_core::boardy::MANAGEMENT
            )
        })
    }

    pub fn modal(&self) -> bool {
        self.projects.confirming
    }

    fn restart_tofu(&mut self, cx: &mut Context<Self>) {
        let Some(stopped) = self.projects.stopped.take() else {
            return;
        };
        let Some(chat) = self.work().map(|work| work.read(cx).chat().clone()) else {
            return eprintln!("desk: restart: {NO_TOFU}");
        };
        chat.update(cx, |chat, cx| chat.reconnect(stopped.session, cx));
        cx.notify();
    }

    pub fn work(&self) -> Option<&Entity<Work>> {
        std::iter::once(&self.shown)
            .chain(&self.parked)
            .find_map(|shown| match &shown.body {
                Body::Work(work) => Some(work),
                Body::View(_) | Body::Whole(_) => None,
            })
    }

    #[cfg(feature = "module-editor")]
    pub fn editor(&self) -> Option<Entity<crate::modules::editor::Editor>> {
        std::iter::once(&self.shown)
            .chain(&self.parked)
            .filter(|shown| shown.name == EDITOR_SCREEN)
            .find_map(|shown| match &shown.body {
                Body::View(view) | Body::Whole(view) => view.clone().downcast().ok(),
                #[cfg(feature = "screen-work")]
                Body::Work(_) => None,
            })
    }

    pub fn sign_in(&mut self, source: &str, cx: &mut Context<Self>) {
        let Some(chat) = self.work().map(|work| work.read(cx).chat().clone()) else {
            return eprintln!("desk: sign in {source}: no work screen runs tofu");
        };
        chat.update(cx, |chat, cx| chat.sign_in(source, cx));
    }

    pub fn open_notice(&mut self, ix: usize, cx: &mut Context<Self>) {
        let Some(id) = self.bell.session(ix).map(str::to_owned) else {
            return eprintln!("desk: bell: notice {ix} is gone");
        };
        let Some(chat) = self.work().map(|work| work.read(cx).chat().clone()) else {
            return eprintln!("desk: bell: no work screen for session {id}");
        };
        eprintln!("desk: bell: open {id}");
        chat.update(cx, |chat, cx| chat.open_session(Some(id), cx));
    }

    #[cfg(feature = "module-editor")]
    fn lend_chat(view: &AnyView, cx: &mut Context<Self>) {
        let Ok(editor) = view.clone().downcast::<crate::modules::editor::Editor>() else {
            return;
        };
        let desk = cx.weak_entity();
        editor.update(cx, |editor, _| {
            editor.chat_from(move |cx| {
                let desk = desk.upgrade()?;
                Some(desk.read(cx).work()?.read(cx).chat().clone())
            })
        });
    }

    fn park_editor(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self.shown.name != WORK_SCREEN {
            return;
        }
        let Some(screen) = self
            .screens
            .iter()
            .find(|screen| screen.name == EDITOR_SCREEN)
        else {
            return eprintln!("desk: screen {EDITOR_SCREEN} is not in this build");
        };
        match (screen.open)(None, window, cx) {
            Ok(view) => {
                #[cfg(feature = "module-editor")]
                Self::lend_chat(&view, cx);
                self.parked.push(Shown::new(EDITOR_SCREEN.into(), view));
                self.tabbed.push(EDITOR_SCREEN);
                eprintln!("desk: screen {EDITOR_SCREEN} parked");
            }
            Err(error) => eprintln!("desk: screen {EDITOR_SCREEN} did not open: {error}"),
        }
    }

    fn adopt_launched(&mut self, window: &Window, cx: &mut Context<Self>) {
        let Some(work) = self
            .work()
            .filter(|_| self.projects.head.is_none())
            .cloned()
        else {
            return;
        };
        match project::launched() {
            Ok((folder, from)) => {
                eprintln!("desk: launch opens {} from {from}", folder.display());
                self.adopt(folder, &work, window, cx)
            }
            Err(error) => eprintln!("desk: {error}"),
        }
    }

    fn restore(
        &mut self,
        read: Result<Option<project::Remembered>, String>,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        let saved = match read {
            Ok(Some(saved)) => saved,
            Ok(None) => {
                eprintln!("desk: restore: this project has no saved state yet");
                self.projects.restored = true;
                return self.remember_state(cx);
            }
            Err(error) => {
                eprintln!(
                    "desk: restore: nothing is restored, because the saved state is unreadable: {error}"
                );
                self.projects.restored = true;
                return self.remember_state(cx);
            }
        };
        let kept: Vec<(&'static str, &project::Kept)> = saved
            .screens
            .iter()
            .filter_map(|kept| {
                let name = self
                    .screens
                    .iter()
                    .map(|screen| screen.name)
                    .find(|name| *name == kept.name && screen_mark(name).is_some())?;
                if self.gate.is_some() && self.gated_off(name).is_some() {
                    eprintln!("desk: restore: skipped {name}, off in this project");
                    return None;
                }
                Some((name, kept))
            })
            .collect();
        let wanted = |name: &str| kept.iter().any(|(kept, _)| *kept == name);
        let named = self.projects.named.take();
        if named.is_none() && self.shown.name != WORK_SCREEN && !wanted(&self.shown.name) {
            self.show(WORK_SCREEN, window, cx);
        }
        self.parked
            .retain(|parked| matches!(parked.body, Body::Work(_)) || wanted(&parked.name));
        let mut tabbed = Vec::new();
        for (name, _) in &kept {
            let open = self.shown.name == *name || self.parked.iter().any(|p| p.name == *name);
            let opened = open
                || match self.screens.iter().find(|screen| screen.name == *name) {
                    Some(screen) => match (screen.open)(None, window, cx) {
                        Ok(view) => {
                            #[cfg(feature = "module-editor")]
                            Self::lend_chat(&view, cx);
                            self.parked.push(Shown::new((*name).into(), view));
                            true
                        }
                        Err(error) => {
                            eprintln!("desk: restore: screen {name} did not open: {error}");
                            false
                        }
                    },
                    None => false,
                };
            if opened {
                tabbed.push(*name);
            }
        }
        if let Some(name) = self
            .screens
            .iter()
            .map(|screen| screen.name)
            .find(|name| Some(*name) == named.as_deref() && *name != WORK_SCREEN)
            && !tabbed.contains(&name)
        {
            tabbed.push(name);
        }
        self.held = kept
            .iter()
            .filter(|(name, kept)| (kept.pinned || kept.locked) && tabbed.contains(name))
            .map(|(name, kept)| Held {
                name,
                locked: kept.locked,
            })
            .collect();
        self.tabbed = tabbed;
        let resumed = self.projects.resumed.take();
        if resumed.is_none() {
            self.projects.restoring = saved.session.clone();
        }
        self.projects.restored = true;
        eprintln!(
            "desk: restore: screens [{}] focus {} session {}",
            self.tabbed.join(", "),
            saved.focus,
            self.projects.restoring.as_deref().unwrap_or("none")
        );
        match (&named, &resumed) {
            (_, Some(session)) => {
                eprintln!("desk: restore kept the resumed session {session} and the work screen");
                if self.shown.name != WORK_SCREEN {
                    self.show(WORK_SCREEN, window, cx);
                }
            }
            (Some(name), None) => eprintln!("desk: restore kept, --screen {name} focused"),
            (None, None) => {
                let focus = self
                    .tabbed
                    .iter()
                    .copied()
                    .find(|name| *name == saved.focus)
                    .unwrap_or(WORK_SCREEN);
                if self.shown.name != focus {
                    self.show(focus, window, cx);
                }
            }
        }
        self.projects.written = Some(self.snapshot(cx));
        self.restore_session(cx);
        cx.notify();
    }

    fn restore_session(&mut self, cx: &mut Context<Self>) {
        let Some(chat) = self.work().map(|work| work.read(cx).chat().clone()) else {
            return;
        };
        if chat.read(cx).rows().is_empty() {
            return;
        }
        let Some(id) = self.projects.restoring.take() else {
            return;
        };
        if chat.read(cx).rows().iter().any(|row| row.id == id) {
            eprintln!("desk: restore: session {id}");
            chat.update(cx, |chat, cx| chat.open_session(Some(id), cx));
        } else {
            eprintln!(
                "desk: restore: the last session {id} is no longer in this project, so none is opened"
            );
        }
    }

    fn snapshot(&self, cx: &App) -> project::Remembered {
        let screens: Vec<project::Kept> = self
            .tabbed
            .iter()
            .filter(|name| {
                !self
                    .expanded_in
                    .iter()
                    .any(|(expanded, _)| expanded == *name)
            })
            .map(|name| {
                let held = self.held.iter().find(|held| held.name == *name);
                project::Kept {
                    name: (*name).to_owned(),
                    pinned: held.is_some(),
                    locked: held.is_some_and(|held| held.locked),
                }
            })
            .collect();
        let focus = match screens.iter().any(|kept| *kept.name == *self.shown.name) {
            true => self.shown.name.to_string(),
            false => WORK_SCREEN.to_owned(),
        };
        let session = self
            .projects
            .restoring
            .clone()
            .or_else(|| {
                self.work()
                    .and_then(|work| work.read(cx).chat().read(cx).open_id().map(str::to_owned))
            })
            .or_else(|| {
                self.projects
                    .written
                    .as_ref()
                    .and_then(|written| written.session.clone())
            });
        project::Remembered {
            session,
            screens,
            focus,
        }
    }

    fn remember_state(&mut self, cx: &mut Context<Self>) {
        let Some(folder) = self.projects.head.as_ref().map(|head| head.folder.clone()) else {
            return;
        };
        if !self.projects.restored {
            return;
        }
        let state = self.snapshot(cx);
        if self.projects.written.as_ref() == Some(&state) {
            return;
        }
        self.projects.written = Some(state.clone());
        self.projects._saving = Some(cx.spawn(async move |_, cx| {
            cx.background_executor().timer(SAVE_AFTER).await;
            let shown = folder.display().to_string();
            let wrote = cx
                .background_executor()
                .spawn(async move { project::write_state(&folder, &state) })
                .await;
            match wrote {
                Ok(()) => eprintln!("desk: remembered the state of {shown}"),
                Err(error) => eprintln!("desk: the project state was not saved: {error}"),
            }
        }));
    }

    fn save_now(&mut self, cx: &mut Context<Self>) {
        let Some(folder) = self.projects.head.as_ref().map(|head| head.folder.clone()) else {
            return;
        };
        if !self.projects.restored {
            return eprintln!(
                "desk: close: the saved state was never restored, so it is kept as it was"
            );
        }
        self.projects._saving = None;
        let state = self.snapshot(cx);
        match project::write_state(&folder, &state) {
            Ok(()) => eprintln!(
                "desk: close: remembered screens [{}] focus {} session {}",
                state
                    .screens
                    .iter()
                    .map(|kept| kept.name.as_str())
                    .collect::<Vec<_>>()
                    .join(", "),
                state.focus,
                state.session.as_deref().unwrap_or("none")
            ),
            Err(error) => eprintln!("desk: close: the project state was not saved: {error}"),
        }
    }

    fn adopt(
        &mut self,
        folder: PathBuf,
        work: &Entity<Work>,
        window: &Window,
        cx: &mut Context<Self>,
    ) {
        eprintln!("desk: project {}", folder.display());
        let chat = work.read(cx).chat().clone();
        chat.update(cx, |chat, cx| {
            chat.want_accounts(cx);
            chat.want_usage(cx);
            chat.want_context(cx);
        });
        let store = chat.read(cx).store().clone();
        self.projects.active.clear();
        self.projects.renaming = None;
        self.parked
            .retain(|parked| !work::EXPANDABLE.contains(&&*parked.name));
        self.tabbed.retain(|name| !work::EXPANDABLE.contains(name));
        self.expanded_in.clear();
        self.closed_screens.clear();
        self.projects._watch = vec![
            cx.subscribe_in(
                work,
                window,
                |desk, _, expanded: &work::Expanded, window, cx| desk.expand(expanded, window, cx),
            ),
            cx.subscribe(&chat, |desk, _, _: &Listed, cx| {
                #[cfg(feature = "screen-boardy")]
                if desk.gate.is_none() {
                    desk.reread_gate(cx);
                }
                desk.track_running(cx);
                desk.restore_session(cx);
                cx.notify();
            }),
            cx.subscribe(&chat, |desk, _, _: &Touched, cx| desk.reread_head(cx)),
            cx.subscribe(&chat, |desk, _, stopped: &TofuStopped, cx| {
                eprintln!(
                    "desk: tofu stopped {} after {} ms",
                    stopped.exit,
                    stopped.exit.closed.elapsed().as_millis()
                );
                desk.projects.stopped = Some(stopped.clone());
                cx.notify();
            }),
            #[cfg(feature = "screen-settings")]
            cx.subscribe(&chat, |desk, _, _: &SettingsMoved, cx| {
                desk.reread_settings(cx)
            }),
            #[cfg(feature = "screen-boardy")]
            cx.subscribe(&chat, |desk, _, _: &SettingsMoved, cx| desk.reread_gate(cx)),
            #[cfg(feature = "screen-boardy")]
            cx.subscribe(&chat, |desk, _, moved: &BoardyMoved, cx| {
                desk.boardy_moved(&moved.0, cx)
            }),
            cx.observe(&chat, |desk, _, cx| {
                desk.track_running(cx);
                desk.remember_state(cx);
                cx.notify();
            }),
            cx.observe(work, |_, _, cx| cx.notify()),
            cx.observe(&store, |desk, store, cx| {
                for (id, session) in &store.read(cx).sessions {
                    desk.bell.gather(id, session);
                }
                cx.notify();
            }),
        ];
        match project::remember(&folder) {
            Ok(recents) => self.projects.recents = recents,
            Err(error) => eprintln!("desk: recents: {error}"),
        }
        cx.set_global(OpenProject(folder.clone()));
        if let Some(saving) = self.projects._saving.take() {
            saving.detach();
        }
        self.projects.restored = false;
        self.projects.restoring = None;
        self.projects.resumed = None;
        self.projects.written = None;
        self.projects.stopped = None;
        let reading = folder.clone();
        self.projects._reading = Some(cx.spawn_in(window, async move |desk, cx| {
            let read = cx
                .background_executor()
                .spawn(async move { project::read_state(&reading) })
                .await;
            if let Err(error) =
                desk.update_in(cx, |desk, window, cx| desk.restore(read, window, cx))
            {
                eprintln!("desk: restore: the desk closed before its state was read: {error}");
            }
        }));
        self.projects.head = Some(Head::empty(folder));
        self.reread_head(cx);
        #[cfg(feature = "screen-boardy")]
        {
            self.gate = None;
            self.reread_gate(cx);
        }
        #[cfg(any(
            feature = "screen-limits",
            feature = "screen-classifier",
            feature = "screen-library",
            feature = "screen-context",
            feature = "screen-session",
            feature = "screen-forks",
            feature = "screen-usage",
            feature = "screen-accounts",
            feature = "screen-settings",
            feature = "screen-boardy"
        ))]
        self.feed_screens(cx);
        cx.notify();
    }

    #[cfg(feature = "screen-boardy")]
    fn reread_gate(&mut self, cx: &mut Context<Self>) {
        let Some(chat) = self.work().map(|work| work.read(cx).chat().clone()) else {
            return;
        };
        let desk = cx.weak_entity();
        chat.update(cx, |chat, cx| {
            chat.request::<QuerySettings>(&NoParams {}, cx, move |_, reply, cx| {
                let read = reply.and_then(|report| {
                    let settings = Settings::try_from(report).map_err(|error| error.to_string())?;
                    Gate::read(&settings).map_err(|error| error.to_string())
                });
                cx.defer(move |cx| {
                    if let Err(error) = desk.update(cx, |desk, cx| desk.gated(read, cx)) {
                        eprintln!("desk: boardy gate answered after the desk closed: {error}");
                    }
                });
            });
        });
    }

    #[cfg(feature = "screen-boardy")]
    fn gated(&mut self, read: Result<Gate, String>, cx: &mut Context<Self>) {
        let gate = match read {
            Ok(gate) => gate,
            Err(error) => return eprintln!("desk: boardy gate unread: {error}"),
        };
        if self.gate == Some(gate) {
            return;
        }
        eprintln!("desk: boardy gate on {} flow {:?}", gate.on, gate.flow);
        self.gate = Some(gate);
        let open = self.tabbed.contains(&BOARDY_SCREEN)
            || self.shown.name == BOARDY_SCREEN
            || self.parked.iter().any(|p| p.name == BOARDY_SCREEN);
        if !gate.on && open {
            self.parked.retain(|p| p.name != BOARDY_SCREEN);
            self.tabbed.retain(|name| *name != BOARDY_SCREEN);
            self.closed_screens
                .retain(|closed| closed.name != BOARDY_SCREEN);
            self.held.retain(|held| held.name != BOARDY_SCREEN);
            if self.shown.name == BOARDY_SCREEN
                && let Some(at) = self.parked.iter().position(|p| p.name == WORK_SCREEN)
            {
                self.shown = self.parked.remove(at);
            }
            eprintln!("desk: boardy off, closed its tab");
            self.remember_state(cx);
        }
        self.feed_screens(cx);
        cx.notify();
    }

    #[cfg(any(
        feature = "screen-limits",
        feature = "screen-classifier",
        feature = "screen-library",
        feature = "screen-context",
        feature = "screen-session",
        feature = "screen-forks",
        feature = "screen-usage",
        feature = "screen-accounts",
        feature = "screen-settings",
        feature = "screen-boardy"
    ))]
    fn feed_screens(&mut self, cx: &mut Context<Self>) {
        let Some(chat) = self.work().map(|work| work.read(cx).chat().clone()) else {
            return;
        };
        let views: Vec<AnyView> = std::iter::once(&self.shown)
            .chain(&self.parked)
            .filter_map(|shown| match &shown.body {
                Body::View(view) => Some(view.clone()),
                Body::Work(_) | Body::Whole(_) => None,
            })
            .collect();
        for view in views {
            #[cfg(feature = "screen-limits")]
            if let Ok(limits) = view.clone().downcast::<Limits>() {
                limits.update(cx, |limits, cx| limits.read_from(&chat, cx));
            }
            #[cfg(feature = "screen-classifier")]
            if let Ok(classifier) = view.clone().downcast::<Classifier>() {
                classifier.update(cx, |classifier, cx| classifier.read_from(&chat, cx));
            }
            #[cfg(feature = "screen-library")]
            if let Ok(library) = view.clone().downcast::<Library>() {
                library.update(cx, |library, cx| library.read_from(&chat, cx));
            }
            #[cfg(feature = "screen-context")]
            if let Ok(context) = view.clone().downcast::<ContextScreen>() {
                context.update(cx, |context, cx| context.read_from(&chat, cx));
            }
            #[cfg(feature = "screen-session")]
            if let Ok(session) = view.clone().downcast::<SessionScreen>() {
                session.update(cx, |session, cx| session.read_from(&chat, cx));
            }
            #[cfg(feature = "screen-forks")]
            if let Ok(forks) = view.clone().downcast::<ForksScreen>() {
                forks.update(cx, |forks, cx| forks.read_from(&chat, cx));
            }
            #[cfg(feature = "screen-accounts")]
            if let Ok(accounts) = view.clone().downcast::<AccountsScreen>() {
                accounts.update(cx, |accounts, cx| accounts.read_from(&chat, cx));
            }
            #[cfg(feature = "screen-settings")]
            if let Ok(settings) = view.clone().downcast::<SettingsScreen>() {
                settings.update(cx, |settings, cx| settings.read_from(&chat, cx));
            }
            #[cfg(feature = "screen-boardy")]
            if let (Ok(boardy), Some(gate)) = (view.clone().downcast::<BoardyScreen>(), self.gate) {
                boardy.update(cx, |boardy, cx| boardy.read_from(&chat, gate, cx));
            }
            #[cfg(feature = "screen-usage")]
            if let Ok(usage) = view.downcast::<UsageScreen>() {
                usage.update(cx, |usage, cx| usage.read_from(&chat, cx));
            }
        }
    }

    #[cfg(feature = "screen-settings")]
    fn reread_settings(&mut self, cx: &mut Context<Self>) {
        let views: Vec<Entity<SettingsScreen>> = std::iter::once(&self.shown)
            .chain(&self.parked)
            .filter_map(|shown| match &shown.body {
                Body::View(view) => view.clone().downcast::<SettingsScreen>().ok(),
                Body::Work(_) | Body::Whole(_) => None,
            })
            .collect();
        for settings in views {
            settings.update(cx, |settings, cx| settings.reread(cx));
        }
    }

    #[cfg(feature = "screen-boardy")]
    fn open_ticket(
        &mut self,
        asked: &crate::screens::boardy::OpenTicket,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        if let Err(why) = self.open_screen(BOARDY_SCREEN, window, cx) {
            return eprintln!("desk: boardy: cannot open {}: {why}", asked.id);
        }
        if let Body::View(view) = &self.shown.body
            && let Ok(boardy) = view.clone().downcast::<BoardyScreen>()
        {
            boardy.update(cx, |boardy, cx| boardy.open_ticket_at(&asked.id, cx));
        }
    }

    #[cfg(feature = "screen-boardy")]
    fn boardy_moved(&mut self, board: &str, cx: &mut Context<Self>) {
        let views: Vec<Entity<BoardyScreen>> = std::iter::once(&self.shown)
            .chain(&self.parked)
            .filter_map(|shown| match &shown.body {
                Body::View(view) => view.clone().downcast::<BoardyScreen>().ok(),
                Body::Work(_) | Body::Whole(_) => None,
            })
            .collect();
        for boardy in views {
            boardy.update(cx, |boardy, cx| boardy.changed(board, cx));
        }
    }

    fn reread_head(&mut self, cx: &mut Context<Self>) {
        let Some(folder) = self.projects.head.as_ref().map(|head| head.folder.clone()) else {
            return;
        };
        self.projects._head = Some(cx.spawn(async move |this, cx| {
            let head = cx
                .background_executor()
                .spawn(async move { project::head(folder) })
                .await;
            this.update(cx, |desk, cx| {
                eprintln!(
                    "desk: git {} ahead {} changed {}",
                    head.branch, head.ahead, head.changed
                );
                desk.projects.head = Some(head);
                cx.notify();
            })
            .ok();
        }));
        cx.notify();
    }

    fn track_running(&mut self, cx: &mut Context<Self>) {
        let Some(work) = self.work().cloned() else {
            return;
        };
        let chat = work.read(cx).chat().read(cx);
        let busy = chat.open_id().filter(|_| chat.busy(cx));
        let problem = chat.problem().cloned();
        let mut moved = project::keep_active(&mut self.projects.active, chat.rows(), busy);
        for id in running(chat.store().read(cx)) {
            if !self.projects.active.iter().any(|known| known == id) {
                self.projects.active.push(id.to_owned());
                moved = true;
            }
        }
        if moved {
            eprintln!("desk: active {}", self.projects.active.join(" "));
            cx.notify();
        }
        if let Some(before) = self.projects.refused.take() {
            match problem {
                Some(text) if Some(&text) != before.as_ref() => {
                    eprintln!("desk: sidebar: rename refused: {text}");
                    self.toast(text, None, cx);
                }
                _ => self.projects.refused = Some(before),
            }
        }
    }

    fn sidebar_project(&self, cx: &App) -> Option<Project> {
        let head = self.projects.head.as_ref()?;
        let chat = self.work()?.read(cx).chat().read(cx);
        let opened = project::Opened {
            active: &self.projects.active,
            open: chat.open_id(),
            statuses: &chat.store().read(cx).statuses,
        };
        let mut project = project::sidebar(head, chat.rows(), &opened);
        let running = running(chat.store().read(cx));
        for (id, session) in self.projects.active.iter().zip(&mut project.active) {
            if running.contains(&id.as_str()) && session.state != SessionState::Waiting {
                session.state = SessionState::Running;
            }
        }
        Some(project)
    }

    fn session_id(&self, at: SessionAt, chat: &Chat) -> Option<String> {
        match at {
            SessionAt::Active(ix) => self.projects.active.get(ix).cloned(),
            SessionAt::Inactive(ix) => project::inactive(chat.rows(), &self.projects.active)
                .nth(ix)
                .map(|row| row.id.clone()),
        }
    }

    fn cron_menu(&self, live: usize, cx: &mut Context<Self>) -> Option<AnyView> {
        let chat = self.work()?.read(cx).chat().clone();
        let jobs: Vec<CronJob> = {
            let chat = chat.read(cx);
            let session = chat.store().read(cx).sessions.get(chat.open_id()?)?;
            session
                .cron
                .as_ref()?
                .jobs
                .iter()
                .filter(|job| job.ended.is_none())
                .cloned()
                .collect()
        };
        if jobs.is_empty() {
            return None;
        }
        let mut items = Vec::new();
        let mut lines = Vec::new();
        for job in &jobs {
            let toggle = if job.paused { "resume" } else { "pause" };
            items.push(MenuItem::Submenu {
                label: format!("{} {} {}", job.id, job.schedule, shortened(&job.prompt)).into(),
                icon: None,
                items: vec![
                    MenuItem::action(if job.paused { "Resume" } else { "Pause" }),
                    MenuItem::action("Delete"),
                ],
            });
            lines.extend([
                Vec::new(),
                vec![format!("/cron {toggle} {}", job.id)],
                vec![format!("/cron delete {}", job.id)],
            ]);
        }
        items.extend([MenuItem::Separator, MenuItem::action("Stop all")]);
        lines.extend([
            Vec::new(),
            jobs.iter()
                .map(|job| format!("/cron delete {}", job.id))
                .collect(),
        ]);
        let chat = chat.downgrade();
        self.cron.update(cx, |button, cx| {
            button.items(items, cx);
            button.trigger(move |_, theme| cron_trigger(live, theme));
            button.on_pick(move |at, _, cx| {
                let sent = lines.get(*at).cloned().unwrap_or_default();
                let updated = chat.update(cx, |chat, cx| {
                    for line in sent {
                        chat.cron_command(line, cx);
                    }
                });
                if let Err(error) = updated {
                    eprintln!("desk: cron menu lost its chat: {error}");
                }
            });
        });
        Some(self.cron.clone().into())
    }

    pub fn status(&self, cx: &App) -> Status {
        let branch = self
            .projects
            .head
            .as_ref()
            .filter(|head| !head.branch.is_empty())
            .map(|head| Branch {
                name: head.branch.clone().into(),
                ahead: usize::try_from(head.ahead).unwrap_or(usize::MAX),
            });
        let Some(chat) = self.work().map(|work| work.read(cx).chat().read(cx)) else {
            return Status {
                branch,
                accounts: Accounts::Unread(NO_TOFU.into()),
                ..Status::default()
            };
        };
        let store = chat.store().read(cx);
        let open = chat
            .open_id()
            .and_then(|id| Some((id, store.sessions.get(id)?)));
        let source = chat.source(cx);
        let (mut accounts, mut serving, ids) =
            accounts(chat.accounts(), &store.quota, source.as_deref());
        let notice = open
            .and_then(|(_, session)| session.account.as_ref())
            .or_else(|| {
                store
                    .sessions
                    .values()
                    .find_map(|session| session.account.as_ref())
            });
        if let Some(notice) = notice {
            held(notice, &ids, &mut accounts, &mut serving);
        }
        let Some((id, session)) = open else {
            return Status {
                branch,
                accounts,
                serving,
                ..Status::default()
            };
        };
        let name = chat
            .rows()
            .iter()
            .find(|row| row.id == id)
            .and_then(|row| row.name.clone())
            .filter(|name| !unnamed(name))
            .or_else(|| Some(session.name.clone()).filter(|name| !unnamed(name)))
            .unwrap_or_default();
        let replayed = store
            .context
            .read
            .as_ref()
            .filter(|read| read.value.session.as_deref() == Some(id))
            .and_then(|read| Some((read.value.occupancy.as_ref()?.total, read.value.ceiling?)));
        Status {
            branch,
            session: Some(name.into()),
            session_group: Some(SessionGroup {
                context: session
                    .context
                    .or(replayed)
                    .map(|(used, budget)| ContextUse {
                        tokens: format!("{} / {}", thousands(used), thousands(budget)).into(),
                        share: if budget > 0 {
                            used as f32 / budget as f32
                        } else {
                            0.0
                        },
                    }),
                classifier: chat.decisions_shown(cx),
                cron: session
                    .cron
                    .as_ref()
                    .map_or(0, |cron| usize::try_from(cron.live).unwrap_or(0)),
            }),
            accounts,
            serving,
            problem: None,
        }
    }

    fn pick_from_sidebar(
        &mut self,
        pick: &SidebarPick,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        let Some(chat) = self.work().map(|work| work.read(cx).chat().clone()) else {
            return self.open_projects(window, cx);
        };
        match pick {
            SidebarPick::Project => self.open_projects(window, cx),
            SidebarPick::NewSession => chat.update(cx, |chat, cx| chat.open_session(None, cx)),
            SidebarPick::Open(at) | SidebarPick::CopyId(at) => {
                let id = self.session_id(*at, chat.read(cx));
                match (pick, id) {
                    (_, None) => eprintln!("desk: sidebar: session row {at:?} is gone"),
                    (SidebarPick::CopyId(_), Some(id)) => {
                        eprintln!("desk: sidebar: copied session id {id}");
                        cx.write_to_clipboard(ClipboardItem::new_string(id));
                    }
                    (_, Some(id)) if chat.read(cx).open_id() == Some(id.as_str()) => {
                        eprintln!("desk: sidebar: session {id} already shows")
                    }
                    (_, Some(id)) => {
                        eprintln!("desk: sidebar: open {id}");
                        chat.update(cx, |chat, cx| chat.open_session(Some(id), cx));
                    }
                }
            }
            SidebarPick::Rename(at) => {
                let Some(id) = self.session_id(*at, chat.read(cx)) else {
                    return eprintln!("desk: sidebar: session row {at:?} is gone");
                };
                let name = chat
                    .read(cx)
                    .rows()
                    .iter()
                    .find(|row| row.id == id)
                    .and_then(|row| row.name.clone())
                    .filter(|name| !unnamed(name))
                    .unwrap_or_default();
                let input = TextInput::new("Session name".into(), window, cx);
                input.update(cx, |input, cx| {
                    input.replace_text_in_range(None, &name, window, cx)
                });
                window.focus(&input.focus_handle(cx), cx);
                eprintln!("desk: sidebar: rename {id} from {name}");
                self.projects.renaming = Some(Renaming { id, at: *at, input });
                cx.notify();
            }
            SidebarPick::Docs => eprintln!("desk: sidebar: Docs has no source yet"),
        }
    }

    fn rename_key(&mut self, event: &KeyDownEvent, window: &mut Window, cx: &mut Context<Self>) {
        let enter = match event.keystroke.key.as_str() {
            "enter" => true,
            "escape" => false,
            _ => return,
        };
        cx.stop_propagation();
        let Some(renaming) = self.projects.renaming.take() else {
            return;
        };
        window.focus(&self.focus, cx);
        cx.notify();
        let name = renaming.input.read(cx).text().trim().to_owned();
        let Some(chat) = self.work().map(|work| work.read(cx).chat().clone()) else {
            return;
        };
        if !enter || name.is_empty() {
            return eprintln!("desk: sidebar: rename {} cancelled", renaming.id);
        }
        eprintln!("desk: sidebar: rename {} to {name}", renaming.id);
        self.projects.refused = Some(chat.read(cx).problem().cloned());
        chat.update(cx, |chat, cx| chat.rename(renaming.id, name, cx));
    }

    fn open_projects(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let current = self
            .projects
            .head
            .as_ref()
            .map(|head| head.folder.as_path());
        let menu = project::picker(&self.projects.recents, current, window, cx);
        let picked =
            cx.listener(|desk, id: &SharedString, window, cx| desk.project_picked(id, window, cx));
        menu.update(cx, |menu, cx| {
            menu.on_pick(picked);
            menu.on_close(|_, _| eprintln!("desk: projects menu closed"));
            menu.open(window, cx);
        });
        eprintln!(
            "desk: projects menu lists {} recents",
            self.projects.recents.len()
        );
        self.projects.menu = Some(menu);
        cx.notify();
    }

    fn project_picked(&mut self, id: &SharedString, window: &mut Window, cx: &mut Context<Self>) {
        eprintln!("desk: projects menu picked {id}");
        match project::picked(id, &self.projects.recents) {
            Picked::OpenFolder => self.prompt_folder(window, cx),
            Picked::Switch(folder) => self.switch(folder, window, cx),
            Picked::Forget(folder) => match project::forget(&folder) {
                Ok(recents) => {
                    eprintln!("desk: forgot missing project {}", folder.display());
                    self.projects.recents = recents;
                    cx.notify();
                }
                Err(error) => eprintln!("desk: recents: {error}"),
            },
            Picked::Unknown => eprintln!("desk: projects menu: {id} is not a recent project"),
        }
    }

    fn prompt_folder(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let asked = cx.prompt_for_paths(gpui::PathPromptOptions {
            files: false,
            directories: true,
            multiple: false,
            prompt: Some("Open project".into()),
        });
        cx.spawn_in(window, async move |this, cx| {
            let folder = match asked.await {
                Ok(Ok(Some(mut picked))) => picked.pop(),
                Ok(Ok(None)) => None,
                Ok(Err(error)) => {
                    eprintln!("desk: the folder dialog failed: {error}");
                    None
                }
                Err(_) => {
                    eprintln!("desk: the folder dialog closed without an answer");
                    None
                }
            };
            let Some(folder) = folder else {
                return eprintln!("desk: no folder was picked");
            };
            this.update_in(cx, |desk, window, cx| desk.switch(folder, window, cx))
                .ok();
        })
        .detach();
    }

    fn switch(&mut self, folder: PathBuf, window: &mut Window, cx: &mut Context<Self>) {
        if self
            .projects
            .head
            .as_ref()
            .is_some_and(|head| head.folder == folder)
        {
            return eprintln!("desk: {} is already open", folder.display());
        }
        let busy = self
            .work()
            .is_some_and(|work| work.read(cx).chat().read(cx).busy(cx));
        if busy {
            eprintln!("desk: a turn is running, so the switch asks first");
            self.projects.switching = Some(folder);
            self.projects.confirming = true;
            return cx.notify();
        }
        self.switch_now(folder, window, cx);
    }

    fn switch_now(&mut self, folder: PathBuf, window: &mut Window, cx: &mut Context<Self>) {
        let work = match work::open_in(folder.clone(), window, cx) {
            Ok(work) => work,
            Err(error) => return eprintln!("desk: {} did not open: {error}", folder.display()),
        };
        self.parked
            .retain(|parked| !matches!(parked.body, Body::Work(_)));
        let next = Shown {
            name: WORK_SCREEN.into(),
            body: Body::Work(work.clone()),
        };
        let left = std::mem::replace(&mut self.shown, next);
        if !matches!(left.body, Body::Work(_)) {
            self.parked.push(left);
        }
        work.focus_handle(cx).focus(window, cx);
        self.adopt(folder, &work, window, cx);
    }

    fn switch_sheet(&self, cx: &mut Context<Self>) -> Option<AnyElement> {
        let folder = self.projects.switching.as_ref()?;
        let cancel = cx.listener(|desk, _: &(), _, cx| {
            eprintln!("desk: switch cancelled");
            desk.projects.confirming = false;
            cx.notify();
        });
        let confirm = cx.listener(|desk, _: &(), window, cx| {
            desk.projects.confirming = false;
            if let Some(folder) = desk.projects.switching.clone() {
                desk.switch_now(folder, window, cx);
            }
        });
        Some(
            Sheet::new(
                "switch-project",
                format!("Switch to {}?", project::name(folder)),
            )
            .open(self.projects.confirming)
            .on_cancel(move |window, cx| cancel(&(), window, cx))
            .on_confirm(move |window, cx| confirm(&(), window, cx))
            .child(
                div().child(
                    "A turn is running here. Switching stops this tofu, and the turn with it.",
                ),
            )
            .into_any_element(),
        )
    }
}

impl Render for Desk {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let edge_to_edge = window.is_maximized() || window.is_fullscreen();
        let theme = ActiveTheme::theme(cx);
        let toast_layer = self.toast.as_ref().map(|shown| {
            div()
                .absolute()
                .left_0()
                .right_0()
                .bottom(px(TOAST_BOTTOM))
                .flex()
                .justify_center()
                .child(toast(
                    match shown.body.is_empty() {
                        true => shown.title.clone(),
                        false => format!("{}: {}", shown.title, shown.body).into(),
                    },
                    shown.badge,
                    &theme,
                    cx.listener(|desk, _: &ClickEvent, _, cx| {
                        desk.toast = None;
                        cx.notify();
                    }),
                ))
        });
        let whole = matches!(self.shown.body, Body::Whole(_));
        let content = self.content(&theme, cx);
        let content = timed("screen", content).into_any_element();
        let sheet = self.switch_sheet(cx);
        let bar = self.chrome(Part::Title, cx);
        let body = match whole {
            true => div()
                .flex_1()
                .min_h_0()
                .flex()
                .flex_col()
                .child(content)
                .into_any_element(),
            false => self.tiled(content, cx),
        };
        #[cfg(feature = "screen-work")]
        let menu = self.projects.menu.clone();
        #[cfg(not(feature = "screen-work"))]
        let menu: Option<Entity<Palette>> = None;
        let root = div();
        #[cfg(feature = "screen-work")]
        let root = root.on_action(cx.listener(Self::screen_menu));
        #[cfg(feature = "screen-boardy")]
        let root = root.on_action(cx.listener(Self::open_ticket));
        let root = root
            .size_full()
            .relative()
            .track_focus(&self.focus)
            .capture_key_down(cx.listener(Self::global_key))
            .on_key_down(cx.listener(Self::escape))
            .on_action(|_: &Find, _, _| {
                eprintln!("desk: find: Ctrl F is not in the focused module, find is chat only");
            })
            .flex()
            .flex_col()
            .when(!edge_to_edge, |root| root.rounded(px(WINDOW_RADIUS)))
            .bg(theme.color(ColorToken::SurfaceWindow))
            .shadow(vec![ring(ink(&theme, WINDOW_RING))])
            .text_size(px(TEXT))
            .text_color(theme.color(ColorToken::TextBase))
            .child(bar)
            .child(body)
            .child(timed("palette", self.palette.clone()))
            .children(menu)
            .children(sheet)
            .children(toast_layer);
        timed("window", root)
    }
}
