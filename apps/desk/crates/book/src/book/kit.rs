use std::time::Instant;

use desk_motion::{Phase, Presence};
use desk_ui::components::card::{Header, inner_card, shell};
use desk_ui::components::paint::{ink, presented};
use desk_ui::components::size::CAPTION_TEXT;
use desk_ui::theme::{ColorToken, Theme};
use gpui::{Div, IntoElement, ParentElement, SharedString, Styled, Window, div};

pub const TILE_HEIGHT: f32 = 320.0;
pub const LIST_HEIGHT: f32 = 480.0;
pub const MODELS: [&str; 4] = [
    "claude-sub/claude-opus-5",
    "codex-sub/gpt-5.6-sol",
    "opencode-sub/deepseek-v3",
    "anthropic/claude-opus-5",
];

pub const EFFORTS: [&str; 4] = ["high", "medium", "low", "minimal"];

pub fn models() -> Vec<SharedString> {
    MODELS.map(SharedString::from).to_vec()
}

pub fn efforts() -> Vec<SharedString> {
    EFFORTS.map(SharedString::from).to_vec()
}

pub fn label(text: impl Into<SharedString>, theme: &Theme) -> Div {
    div()
        .text_color(ink(theme, CAPTION_TEXT))
        .child(text.into())
}

pub fn titled(title: &'static str) -> Header {
    Header::Title(None, title.into(), None)
}

pub fn block(title: &'static str, theme: &Theme, body: impl IntoElement) -> Div {
    shell(titled(title), theme).child(inner_card(theme).flex_col().gap_3().p_3().child(body))
}

pub fn spread(theme: &Theme) -> Div {
    div()
        .flex()
        .flex_wrap()
        .items_center()
        .gap_2()
        .text_color(theme.color(ColorToken::TextBase))
}

pub fn named(name: impl Into<SharedString>, theme: &Theme, shown: impl IntoElement) -> Div {
    div()
        .flex()
        .flex_col()
        .items_start()
        .gap_1()
        .child(shown)
        .child(label(name, theme))
}

pub fn toggle(presence: &mut Presence) {
    let now = Instant::now();
    let open = matches!(presence.phase(now), Phase::Closed | Phase::Closing);
    presence.set_open(open, now);
}

pub fn present(
    presence: &Presence,
    reduced: bool,
    window: &mut Window,
    element: Div,
) -> Option<Div> {
    let now = Instant::now();
    if matches!(presence.phase(now), Phase::Opening | Phase::Closing) {
        window.request_animation_frame();
    }
    presence
        .mounted(now)
        .then(|| presented(element, presence.progress(now), reduced))
}
