use desk_ui::components::sidebar::{Project, SIDEBAR_COLUMN, Session, Sidebar, SidebarPick};
use desk_ui::theme::Theme;
use gpui::{App, Context, Div, Window, div, prelude::*, px};

use super::Book;
use super::kit::{named, spread};

const SIDEBAR_HEIGHT: f32 = 794.0;
const RUNNING: [(&str, &str); 2] = [
    ("clear-sable-eagle", "working"),
    ("quiet-amber-heron", "loop 10m"),
];
const INACTIVE: [(&str, &str); 3] = [
    ("fond-sandy-mink", "2h"),
    ("tidy-ochre-wren", "1d"),
    ("crisp-azure-swift", "3d"),
];

fn sessions(rows: &[(&'static str, &'static str)]) -> Vec<Session> {
    rows.iter()
        .map(|&(name, state)| Session {
            name: name.into(),
            state: state.into(),
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
        running: sessions(&RUNNING),
        shown: Some(0),
        inactive: sessions(&INACTIVE),
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
            "IWIN-1 fixture",
            theme,
            frame(Sidebar::new(
                "sidebar-notes",
                Some(notes),
                told("notes-app", cx),
            )),
        ))
        .child(named(
            "No project",
            theme,
            frame(Sidebar::new("sidebar-empty", None, told("no project", cx))),
        ))
}
