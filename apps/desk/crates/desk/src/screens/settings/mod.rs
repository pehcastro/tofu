pub mod board;
mod fixture;
mod intro;
mod rows;

pub use rows::Rows;

use desk_ui::live::ActiveTheme;
use gpui::{AnyView, App, AppContext, Window};

pub fn open(board: Option<&str>, _: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    board::load_fonts(cx)?;
    match board {
        None | Some("ISET-1") => Ok(cx.new(|cx| rows::Rows::new(&ActiveTheme::theme(cx))).into()),
        Some("ISET-3") => Ok(cx.new(|_| intro::Intro::new()).into()),
        Some(other) => Err(format!(
            "the settings screen draws ISET-1 and ISET-3, not {other}"
        )),
    }
}
