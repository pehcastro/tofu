use desk_ui::components::glyph::Glyph;
use desk_ui::components::overlay::{MenuItem, menu};
use desk_ui::components::sidebar::{
    Project, SIDEBAR_COLUMN, Session, SessionAt, SessionState, Sidebar, SidebarPick,
};
use desk_ui::components::status_bar::{
    Account, Accounts, Branch, ContextUse, Reset, SessionGroup, Standing, Status, StatusBar,
    StatusPick, UsageWindow,
};
use desk_ui::components::tabs::{Tab, TabEvent, TabMark, header_tabs};
use desk_ui::components::title_bar::{
    Github, GithubUser, Notice, Title, TitleBar, TitlePick, TitlePop, WindowKeys,
};
use desk_ui::icon::Icon;
use desk_ui::theme::Theme;
use gpui::{App, Context, Div, IntoElement, Window, div, prelude::*, px};

use super::Book;
use super::kit::named;

const BOARD_WIDTH: f32 = 960.0;
const NARROW_WIDTH: f32 = 720.0;
const WORKSPACES: [&str; 3] = ["work", "editor", "data"];
const POP_ROOM: f32 = 250.0;
const SESSIONS_HEIGHT: f32 = 300.0;

fn board_title(open: Option<TitlePop>) -> Title {
    let notice = |glyph, text: &str, code: Option<&str>, detail: &str, (needs_you, fresh)| Notice {
        glyph,
        text: text.to_owned().into(),
        code: code.map(|code| code.to_owned().into()),
        detail: detail.to_owned().into(),
        needs_you,
        fresh,
    };
    Title {
        sidebar_open: true,
        palette_keys: "Ctrl K".into(),
        notices: vec![
            notice(
                Glyph::Lock,
                "shell wants to run",
                Some("rm -rf build/"),
                "2m ago",
                (true, true),
            ),
            notice(
                Glyph::Agents,
                "ts-dev finished",
                Some("sort order is now by date"),
                "12m ago",
                (false, true),
            ),
            notice(
                Glyph::Cron,
                "Cron nightly fired",
                None,
                "1h ago",
                (false, false),
            ),
            notice(
                Glyph::Trace,
                "A turn failed",
                Some("fix the build"),
                "2h ago",
                (false, false),
            ),
        ],
        unread: true,
        github: Github::SignedIn(GithubUser {
            login: "pehcastro".into(),
            name: Some("Luiz".into()),
            picture: None,
        }),
        keys: WindowKeys::Shown,
        open,
        tabs_x: SIDEBAR_COLUMN,
    }
}

fn each_state() -> Project {
    let ages = ["for 12m", "8m ago", "2m ago", "4h ago", "1d ago"];
    let names = [
        "clear-sable-eagle",
        "bold-teal-otter",
        "quiet-amber-heron",
        "35672da8-faa2-4bbd-a7b9-6ec14c3a2e0f",
        "tidy-ochre-wren",
    ];
    let mut sessions = SessionState::ALL
        .into_iter()
        .zip(names.into_iter().zip(ages))
        .map(|(state, (name, age))| Session {
            name: name.into(),
            state,
            age: age.into(),
        });
    Project {
        name: "notes-app".into(),
        branch: "main".into(),
        changed: 3,
        active: sessions.by_ref().take(3).collect(),
        shown: Some(SessionAt::Active(0)),
        inactive: sessions.collect(),
    }
}

fn empty_title(open: Option<TitlePop>) -> Title {
    Title {
        sidebar_open: true,
        palette_keys: "Ctrl K".into(),
        notices: Vec::new(),
        unread: false,
        github: Github::SignedOut,
        keys: WindowKeys::Shown,
        open,
        tabs_x: SIDEBAR_COLUMN,
    }
}

fn picked_title(cx: &mut Context<Book>) -> impl Fn(&TitlePick, &mut Window, &mut App) + 'static {
    cx.listener(|book, pick: &TitlePick, _, cx| book.tell(format!("Title bar: {pick:?}"), cx))
}

fn picked_status(cx: &mut Context<Book>) -> impl Fn(&StatusPick, &mut Window, &mut App) + 'static {
    cx.listener(|book, pick: &StatusPick, _, cx| book.tell(format!("Status bar: {pick:?}"), cx))
}

fn workspaces(theme: &Theme, cx: &mut Context<Book>) -> impl IntoElement {
    let tabs: Vec<Tab> = WORKSPACES
        .iter()
        .map(|name| Tab {
            label: (*name).into(),
            icon: None,
            count: None,
            mark: TabMark::Close,
            flag: None,
        })
        .collect();
    header_tabs(
        "chrome-workspaces",
        &tabs,
        0,
        &[],
        theme,
        cx.listener(|book, event: &TabEvent, _, cx| {
            book.tell(format!("Workspace tabs: {event:?}"), cx)
        }),
    )
}

fn with_screen(theme: &Theme, cx: &mut Context<Book>) -> impl IntoElement {
    let tab = |name: &str| Tab {
        label: name.to_owned().into(),
        icon: None,
        count: None,
        mark: TabMark::Close,
        flag: None,
    };
    let tabs: Vec<Tab> = WORKSPACES.iter().map(|name| tab(name)).collect();
    header_tabs(
        "chrome-workspaces-screen",
        &tabs,
        0,
        &[tab("theme")],
        theme,
        cx.listener(|book, event: &TabEvent, _, cx| {
            book.tell(format!("Workspace tabs: {event:?}"), cx)
        }),
    )
}

fn narrow(theme: &Theme, cx: &mut Context<Book>) -> impl IntoElement {
    let tab = |name: &str| Tab {
        label: name.to_owned().into(),
        icon: None,
        count: None,
        mark: TabMark::Close,
        flag: None,
    };
    header_tabs(
        "chrome-workspaces-narrow",
        &[tab("work")],
        2,
        &[tab("session"), tab("usage")],
        theme,
        cx.listener(|book, event: &TabEvent, _, cx| {
            book.tell(format!("Workspace tabs: {event:?}"), cx)
        }),
    )
}

fn plus_menu(theme: &Theme, cx: &mut Context<Book>) -> impl IntoElement {
    let items: Vec<MenuItem> = [MenuItem::Action {
        label: "New workspace".into(),
        keys: Some("Ctrl T".into()),
        icon: Some(Icon::Plus.into()),
    }]
    .into_iter()
    .chain([MenuItem::Caption("Tiles, open in this workspace".into())])
    .chain([
        MenuItem::action("Chat").icon(Glyph::Chat),
        MenuItem::action("Sub-agents").icon(Glyph::Agents),
        MenuItem::action("File edits").icon(Glyph::File),
        MenuItem::action("Shells").icon(Glyph::Terminal),
    ])
    .chain([MenuItem::Caption("Screens, open as a tab".into())])
    .chain([MenuItem::action("theme").icon(Icon::Sidebar)])
    .collect();
    menu(
        "chrome-plus-menu",
        &items,
        theme,
        cx.listener(|book, pick: &usize, _, cx| book.tell(format!("Plus menu: {pick}"), cx)),
    )
}

fn frame(bar: impl IntoElement) -> Div {
    div().w(px(BOARD_WIDTH)).child(bar)
}

pub(super) fn chrome_page(theme: &Theme, cx: &mut Context<Book>) -> Div {
    let window = |label: &str, percent: u8, left: &str, at: &str| UsageWindow {
        label: label.to_owned().into(),
        percent,
        reset: Some(Reset {
            left: left.to_owned().into(),
            at: at.to_owned().into(),
        }),
    };
    let account =
        |email: Option<&str>, source: &str, plan: Option<&str>, standing, windows| Account {
            provider: match source {
                "codex-sub" => "Codex Sub".into(),
                _ => "Claude Sub".into(),
            },
            email: email.map(|email| email.to_owned().into()),
            source: source.to_owned().into(),
            plan: plan.map(|plan| plan.to_owned().into()),
            standing,
            windows,
        };
    let claude = account(
        Some("pehcastro@gmail.com"),
        "claude-sub",
        Some("max"),
        Standing::Serving,
        vec![
            window("5h", 34, "in 2h 14m", "Resets Thu 9 Oct, 02:00"),
            window("7d", 72, "in 3d", "Resets Wed 14 Oct, 03:00"),
        ],
    );
    let second = account(
        Some("trophosai@gmail.com"),
        "claude-sub",
        Some("pro"),
        Standing::Serving,
        vec![window("5h", 91, "in 14m", "Resets Thu 9 Oct, 00:14")],
    );
    let codex = account(
        None,
        "codex-sub",
        Some("pro"),
        Standing::Serving,
        vec![window("7d", 3, "in 5d", "Resets Wed 14 Oct, 01:23")],
    );
    let reauth = account(
        Some("trophosai@gmail.com"),
        "claude-sub",
        None,
        Standing::Reauth("refresh failed".into()),
        Vec::new(),
    );
    let limited = account(
        Some("pehcastro@gmail.com"),
        "claude-sub",
        None,
        Standing::Trouble("rate limited".into()),
        Vec::new(),
    );
    let unreported = account(
        Some("pehcastro@gmail.com"),
        "claude-sub",
        Some("max"),
        Standing::Serving,
        Vec::new(),
    );
    let board = Status {
        branch: Some(Branch {
            name: "main".into(),
            ahead: 2,
        }),
        session: Some("clear-sable-eagle".into()),
        session_group: Some(SessionGroup {
            context: Some(ContextUse {
                tokens: "20k".into(),
                share: 0.08,
            }),
            classifier: 4,
            cron: 1,
        }),
        accounts: Accounts::Read(vec![claude.clone()]),
        problem: None,
    };
    let no_session = Status {
        branch: board.branch.clone(),
        accounts: Accounts::Read(vec![claude.clone()]),
        ..Status::default()
    };
    let quota_states = [
        ("Status bar, no session, quota open", no_session.clone()),
        (
            "Status bar, no session, reading",
            Status {
                accounts: Accounts::Reading,
                ..no_session.clone()
            },
        ),
        ("Status bar, one account two windows", board.clone()),
        (
            "Status bar, two providers",
            Status {
                accounts: Accounts::Read(vec![claude.clone(), codex.clone()]),
                ..board.clone()
            },
        ),
        (
            "Status bar, two accounts one provider, told apart by email",
            Status {
                accounts: Accounts::Read(vec![claude.clone(), second]),
                ..board.clone()
            },
        ),
        (
            "Status bar, re-auth needed with Sign in, and a rate limited account",
            Status {
                accounts: Accounts::Read(vec![codex, limited, reauth]),
                ..no_session.clone()
            },
        ),
        (
            "Status bar, quota not reported",
            Status {
                accounts: Accounts::Read(vec![unreported]),
                ..no_session.clone()
            },
        ),
        (
            "Status bar, a session tofu has not named yet",
            Status {
                session: Some("".into()),
                accounts: Accounts::Read(vec![claude]),
                ..board.clone()
            },
        ),
    ];
    let opened = [
        (
            "Title bar, IWIN-1, bell open",
            "chrome-title-bell",
            board_title(Some(TitlePop::Notifications)),
        ),
        (
            "Title bar, IWIN-1, account open",
            "chrome-title-account",
            board_title(Some(TitlePop::Account)),
        ),
        (
            "Title bar, empty, bell open",
            "chrome-title-bell-empty",
            empty_title(Some(TitlePop::Notifications)),
        ),
        (
            "Title bar, empty, account open",
            "chrome-title-account-empty",
            empty_title(Some(TitlePop::Account)),
        ),
    ];
    div()
        .flex()
        .flex_col()
        .gap_4()
        .children(
            quota_states
                .into_iter()
                .enumerate()
                .map(|(at, (name, status))| {
                    named(
                        name,
                        theme,
                        frame(
                            StatusBar::new(("chrome-status-quota", at), status, picked_status(cx))
                                .quota_open(true),
                        )
                        .pt(px(POP_ROOM)),
                    )
                }),
        )
        .child(named(
            "Sidebar sessions, each state, right click a row for its menu",
            theme,
            div()
                .w(px(SIDEBAR_COLUMN))
                .h(px(SESSIONS_HEIGHT))
                .child(Sidebar::new(
                    "chrome-sidebar-states",
                    Some(each_state()),
                    cx.listener(|book, pick: &SidebarPick, _, cx| {
                        book.tell(format!("Sidebar: {pick:?}"), cx)
                    }),
                )),
        ))
        .child(named(
            "Title bar, IWIN-1",
            theme,
            frame(TitleBar::new(
                "chrome-title-board",
                board_title(None),
                Some(workspaces(theme, cx).into_any_element()),
                picked_title(cx),
            )),
        ))
        .child(named(
            "Title bar, a screen tab after the workspaces",
            theme,
            frame(TitleBar::new(
                "chrome-title-screen",
                board_title(None),
                Some(with_screen(theme, cx).into_any_element()),
                picked_title(cx),
            )),
        ))
        .child(named(
            "Title bar, 720 wide, the last screen tab active",
            theme,
            div().w(px(NARROW_WIDTH)).child(TitleBar::new(
                "chrome-title-narrow",
                board_title(None),
                Some(narrow(theme, cx).into_any_element()),
                picked_title(cx),
            )),
        ))
        .child(named("Title bar, + menu open", theme, plus_menu(theme, cx)))
        .child(named(
            "Title bar, empty",
            theme,
            frame(TitleBar::new(
                "chrome-title-empty",
                empty_title(None),
                None,
                picked_title(cx),
            )),
        ))
        .child(named(
            "Title bar, controls only, over the intro and onboarding",
            theme,
            frame(
                TitleBar::new(
                    "chrome-title-controls",
                    empty_title(None),
                    None,
                    picked_title(cx),
                )
                .controls_only(),
            ),
        ))
        .child(named(
            "Status bar, IWIN-1",
            theme,
            frame(StatusBar::new(
                "chrome-status-board",
                board,
                picked_status(cx),
            )),
        ))
        .child(named(
            "Status bar, empty",
            theme,
            frame(StatusBar::new(
                "chrome-status-empty",
                Status::default(),
                picked_status(cx),
            )),
        ))
        .children(opened.map(|(name, id, title)| {
            named(
                name,
                theme,
                frame(TitleBar::new(id, title, None, picked_title(cx))).pb(px(POP_ROOM)),
            )
        }))
}
