use std::rc::Rc;
use std::time::{Duration, SystemTime};

use desk_ui::components::sidebar::{
    Project, SIDEBAR_COLUMN, SessionAt, SessionColumns, SessionState, Sidebar, SidebarPick,
};
use desk_ui::theme::Theme;
use gpui::{App, Context, Div, Window, div, prelude::*, px};

use super::Book;
use super::kit::{label, named, spread};
use super::palette::PalettePage;

const SIDEBAR_HEIGHT: f32 = 794.0;
const MINUTE: u64 = 60;
const ROWS: [(&str, SessionState, u64); 4] = [
    ("clear-sable-eagle", SessionState::Running, 12 * MINUTE),
    ("fond-sandy-mink", SessionState::Stopped, 2 * 60 * MINUTE),
    ("tidy-ochre-wren", SessionState::Failed, 24 * 60 * MINUTE),
    (
        "crisp-azure-swift",
        SessionState::Stopped,
        3 * 24 * 60 * MINUTE,
    ),
];

fn sessions() -> SessionColumns {
    let now = SystemTime::now();
    let mut sessions = SessionColumns::default();
    for (name, state, ago) in ROWS {
        let last = now.checked_sub(Duration::from_secs(ago));
        sessions.push(name.into(), name.into(), last, state);
    }
    sessions.active = vec![0];
    sessions.inactive = vec![1, 2, 3];
    sessions
}

fn told(
    what: &'static str,
    cx: &mut Context<Book>,
) -> impl Fn(&SidebarPick, &mut Window, &mut App) + 'static {
    cx.listener(move |book, pick: &SidebarPick, window, cx| match pick {
        SidebarPick::Project => book
            .palette
            .picker
            .update(cx, |picker, cx| picker.open(window, cx)),
        _ => book.tell(format!("Sidebar, {what}: {pick:?}"), cx),
    })
}

pub(super) fn sidebar_page(palette: &PalettePage, theme: &Theme, cx: &mut Context<Book>) -> Div {
    let notes = Project {
        name: "notes-app".into(),
        branch: "main".into(),
        changed: 3,
        shown: Some(SessionAt::Active(0)),
        sessions: Rc::new(sessions()),
    };
    let fresh = Project {
        name: "hono-starter".into(),
        branch: "".into(),
        changed: 0,
        shown: None,
        sessions: Rc::default(),
    };
    let frame = |sidebar: Sidebar| {
        div()
            .w(px(SIDEBAR_COLUMN))
            .h(px(SIDEBAR_HEIGHT))
            .child(sidebar)
    };
    let projects = div()
        .flex()
        .flex_col()
        .gap_3()
        .child(label(
            format!(
                "The project row opens the projects menu: {}",
                palette.readout
            ),
            theme,
        ))
        .child(palette.picker.clone());
    projects.child(
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
            )),
    )
}
