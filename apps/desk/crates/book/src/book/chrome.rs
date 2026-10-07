use desk_ui::components::status_bar::{Branch, ContextUse, Quota, Status, StatusBar, StatusPick};
use desk_ui::components::tabs::{Tab, TabEvent, TabMark, header_tabs};
use desk_ui::components::title_bar::{Title, TitleBar, TitlePick, WindowKeys};
use desk_ui::theme::Theme;
use gpui::{App, Context, Div, IntoElement, Window, div, prelude::*, px};

use super::Book;
use super::kit::named;

const BOARD_WIDTH: f32 = 1400.0;
const WORKSPACES: [&str; 3] = ["work", "editor", "data"];

fn title(account: Option<&str>, unread: bool) -> Title {
    Title {
        sidebar_open: true,
        palette_keys: "Ctrl K".into(),
        unread,
        account: account.map(Into::into),
        keys: WindowKeys::Shown,
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

fn frame(bar: impl IntoElement) -> Div {
    div().w(px(BOARD_WIDTH)).child(bar)
}

pub(super) fn chrome_page(theme: &Theme, cx: &mut Context<Book>) -> Div {
    let board = Status {
        branch: Some(Branch {
            name: "main".into(),
            ahead: 2,
        }),
        session: Some("clear-sable-eagle".into()),
        context: Some(ContextUse {
            tokens: "20k".into(),
            share: 0.08,
        }),
        quota: Some(Quota {
            account: "claude-sub".into(),
            window: "5h".into(),
            percent: 34,
        }),
        classifier: 4,
        cron: 1,
        problem: None,
    };
    div()
        .flex()
        .flex_col()
        .gap_4()
        .child(named(
            "Title bar, IWIN-1",
            theme,
            frame(TitleBar::new(
                "chrome-title-board",
                title(Some("p"), true),
                Some(workspaces(theme, cx).into_any_element()),
                picked_title(cx),
            )),
        ))
        .child(named(
            "Title bar, empty",
            theme,
            frame(TitleBar::new(
                "chrome-title-empty",
                title(None, false),
                None,
                picked_title(cx),
            )),
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
}
