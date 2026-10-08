use desk_ui::components::sidebar::{
    Project, SIDEBAR_COLUMN, Session, SessionAt, Sidebar, SidebarPick,
};
use desk_ui::theme::Theme;
use gpui::{App, Context, Div, Window, div, prelude::*, px};

use super::Book;
use super::kit::{named, spread};

const SIDEBAR_HEIGHT: f32 = 794.0;
const RUNNING: [(&str, &str, &str); 1] = [("clear-sable-eagle", "Running", "12m ago")];
const INACTIVE: [(&str, &str, &str); 3] = [
    ("fond-sandy-mink", "done", "2h ago"),
    ("tidy-ochre-wren", "failed", "1d ago"),
    ("crisp-azure-swift", "ended", "3d ago"),
];

fn sessions(rows: &[(&'static str, &'static str, &'static str)]) -> Vec<Session> {
    rows.iter()
        .map(|&(name, state, age)| Session {
            name: name.into(),
            state: state.into(),
            age: age.into(),
        })
        .collect()
}

fn told(
    what: &'static str,
    cx: &mut Context<Book>,
) -> impl Fn(&SidebarPick, &mut Window, &mut App) + 'static {
    cx.listener(move |book, pick: &SidebarPick, _, cx| {
        book.tell(format!("Sidebar, {what}: {pick:?}"), cx)
    })
}

pub(super) fn sidebar_page(theme: &Theme, cx: &mut Context<Book>) -> Div {
    let notes = Project {
        name: "notes-app".into(),
        branch: "main".into(),
        changed: 3,
        active: sessions(&RUNNING),
        shown: Some(SessionAt::Active(0)),
        inactive: sessions(&INACTIVE),
    };
    let fresh = Project {
        name: "hono-starter".into(),
        branch: "".into(),
        changed: 0,
        active: Vec::new(),
        shown: None,
        inactive: Vec::new(),
    };
    let frame = |sidebar: Sidebar| {
        div()
            .w(px(SIDEBAR_COLUMN))
            .h(px(SIDEBAR_HEIGHT))
            .child(sidebar)
    };
    spread(theme)
        .items_start()
        .child(named(
            "Sessions",
            theme,
            frame(Sidebar::new(
                "sidebar-notes",
                Some(notes),
                told("notes-app", cx),
            )),
        ))
        .child(named(
            "Empty project",
            theme,
            frame(Sidebar::new(
                "sidebar-fresh",
                Some(fresh),
                told("hono-starter", cx),
            )),
        ))
        .child(named(
            "No project",
            theme,
            frame(Sidebar::new("sidebar-empty", None, told("no project", cx))),
        ))
}
