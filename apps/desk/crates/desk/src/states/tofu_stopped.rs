use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::paint::{ring, tint};
use desk_ui::components::size::{FONT_BODY, RADIUS_ROW};
use desk_ui::theme::{ColorToken, Theme};
use gpui::{App, ClickEvent, Div, Window, div, prelude::*, px};

use crate::modules::chat::TofuStopped;

const TOP: f32 = 12.0;
const PAD_X: f32 = 12.0;
const PAD_Y: f32 = 8.0;
const GAP: f32 = 10.0;
const MAX_WIDTH: f32 = 640.0;
const WARN_RING: f32 = 0.45;

pub fn said(stopped: &TofuStopped) -> String {
    match stopped.exit.last.is_empty() {
        true => format!("tofu stopped ({})", stopped.exit),
        false => format!("tofu stopped ({}): {}", stopped.exit, stopped.exit.last),
    }
}

pub fn banner(
    stopped: &TofuStopped,
    theme: &Theme,
    restart: impl Fn(&ClickEvent, &mut Window, &mut App) + 'static,
) -> Div {
    let warn = theme.color(ColorToken::StatusWarn);
    div()
        .absolute()
        .top(px(TOP))
        .left_0()
        .right_0()
        .flex()
        .justify_center()
        .child(
            div()
                .id("tofu-stopped")
                .occlude()
                .flex()
                .items_center()
                .gap(px(GAP))
                .max_w(px(MAX_WIDTH))
                .min_w_0()
                .px(px(PAD_X))
                .py(px(PAD_Y))
                .rounded(px(RADIUS_ROW))
                .bg(theme.color(ColorToken::CardsOuterFill))
                .shadow(vec![ring(tint(warn, WARN_RING))])
                .text_size(px(FONT_BODY))
                .text_color(theme.color(ColorToken::TextBase))
                .child(div().flex_none().text_color(warn).child("!"))
                .child(div().flex_1().min_w_0().truncate().child(said(stopped)))
                .child(
                    button("tofu-restart", "Restart", None, ButtonKind::Plain, theme)
                        .on_click(restart),
                ),
        )
}
