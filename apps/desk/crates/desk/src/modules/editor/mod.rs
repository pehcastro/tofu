mod boards;
mod fixture;
mod kit;
mod parts;

use desk_ui::live::ActiveTheme;
use gpui::{
    AnyView, App, AppContext, ClickEvent, Context, Render, SharedString, Window, prelude::*,
};

pub fn open(board: Option<&str>, _: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    kit::load_fonts(cx)?;
    let board = match board {
        None | Some("IEDITOR-1") => Board::Edit,
        Some("IEDITOR-2") => Board::Changes,
        Some("IEDITOR-3") => Board::Split,
        Some(other) => {
            return Err(format!(
                "the editor draws IEDITOR-1, IEDITOR-2 and IEDITOR-3, not {other}"
            ));
        }
    };
    Ok(cx.new(|_| Editor::new(board)).into())
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum Board {
    Edit,
    Changes,
    Split,
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum File {
    Notes,
    Test,
    Store,
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum Mode {
    Edit,
    Changes,
    Who,
}

pub struct Editor {
    board: Board,
    file: File,
    test_open: bool,
    notes_open: bool,
    cursor: usize,
    trace: bool,
    filtering: bool,
    mode: Mode,
    menu: bool,
    told: Option<SharedString>,
}

impl Editor {
    fn new(board: Board) -> Self {
        Editor {
            board,
            file: File::Notes,
            test_open: true,
            notes_open: true,
            cursor: 13,
            trace: false,
            filtering: false,
            mode: Mode::Changes,
            menu: board == Board::Split,
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

    fn update(
        change: impl Fn(&mut Self) + 'static,
    ) -> impl Fn(&mut Self, &ClickEvent, &mut Window, &mut Context<Self>) {
        move |this, _, _, cx| {
            change(this);
            cx.notify();
        }
    }
}

impl Render for Editor {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let scale = window.scale_factor();
        let theme = ActiveTheme::theme(cx);
        let body = match self.board {
            Board::Edit => self.edit_board(scale, cx),
            Board::Changes => self.changes_board(scale, cx),
            Board::Split => self.split_board(scale, cx),
        };
        kit::frame(
            &theme,
            body,
            self.told.clone(),
            cx.listener(|this, _: &ClickEvent, _, cx| {
                this.told = None;
                cx.notify();
            }),
        )
    }
}
