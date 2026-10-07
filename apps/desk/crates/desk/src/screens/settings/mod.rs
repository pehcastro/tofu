pub mod board;
mod fixture;
mod intro;
mod rows;

use desk_ui::theme::{ColorToken, Theme};
use gpui::{AnyView, App, AppContext, Div, Window, div, prelude::*, px};

pub fn open(board: Option<&str>, _: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    board::load_fonts(cx)?;
    match board {
        None | Some("ISET-1") => Ok(cx.new(|_| rows::Rows::new()).into()),
        Some("ISET-3") => Ok(cx.new(|_| intro::Intro::new()).into()),
        Some(other) => Err(format!(
            "the settings screen draws ISET-1 and ISET-3, not {other}"
        )),
    }
}

#[derive(Clone, Copy)]
struct Track {
    width: f32,
    height: f32,
    thumb: f32,
    travel: f32,
}

const ROW_TRACK: Track = Track {
    width: 30.0,
    height: 17.0,
    thumb: 13.0,
    travel: 13.0,
};
const EFFECT_TRACK: Track = Track {
    width: 28.0,
    height: 16.0,
    thumb: 12.0,
    travel: 13.0,
};
const THUMB_INSET: f32 = 2.0;

fn track(on: bool, size: Track, theme: &Theme) -> Div {
    div()
        .relative()
        .flex_none()
        .w(px(size.width))
        .h(px(size.height))
        .rounded_full()
        .bg(theme.color(if on {
            ColorToken::SwitchOn
        } else {
            ColorToken::SwitchOff
        }))
        .child(
            div()
                .absolute()
                .top(px(THUMB_INSET))
                .left(px(THUMB_INSET + if on { size.travel } else { 0.0 }))
                .size(px(size.thumb))
                .rounded_full()
                .bg(theme.color(ColorToken::SwitchThumb)),
        )
}
