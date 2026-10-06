mod changes;
mod fixture;
mod history;
mod kit;

use desk_ui::live::ActiveTheme;
use gpui::{
    AnyView, App, AppContext, ClickEvent, Context, Render, SharedString, Window, prelude::*, px,
};

use fixture::CHANGES;

const GAP: f32 = 6.0;

pub fn open(board: Option<&str>, _: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    kit::load_fonts(cx)?;
    let (pane, branches, blocked) = match board {
        None | Some("IGIT-1") => (Pane::Changes, false, false),
        Some("IGIT-2") => (Pane::History, false, false),
        Some("S-GIT-1") => (Pane::History, true, false),
        Some("S-GIT-2") => (Pane::History, false, true),
        Some(other) => {
            return Err(format!(
                "the git module draws IGIT-1, IGIT-2, S-GIT-1 and S-GIT-2, not {other}"
            ));
        }
    };
    Ok(cx
        .new(|_| Git {
            branches,
            blocked,
            ..Git::new(pane)
        })
        .into())
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum Pane {
    Changes,
    History,
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum Group {
    Status,
    Author,
}

pub struct Git {
    pane: Pane,
    staged: [bool; CHANGES.len()],
    group: Group,
    written: bool,
    committed: bool,
    commit: usize,
    branches: bool,
    blocked: bool,
    told: Option<SharedString>,
}

impl Git {
    fn new(pane: Pane) -> Self {
        Git {
            pane,
            staged: [false; CHANGES.len()],
            group: Group::Status,
            written: false,
            committed: false,
            commit: 0,
            branches: false,
            blocked: false,
            told: None,
        }
    }

    fn tell(
        message: &'static str,
    ) -> impl Fn(&mut Self, &ClickEvent, &mut Window, &mut Context<Self>) {
        move |this, _, _, cx| {
            this.told = Some(message.into());
            cx.notify();
        }
    }
}

impl Render for Git {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let body = match self.pane {
            Pane::Changes => self.changes(&theme, cx),
            Pane::History => self.history(&theme, cx),
        };
        kit::frame(
            &theme,
            body.gap(px(GAP)),
            self.told.clone(),
            cx.listener(|this, _: &ClickEvent, _, cx| {
                this.told = None;
                cx.notify();
            }),
        )
    }
}
