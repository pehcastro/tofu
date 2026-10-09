use std::rc::Rc;

use desk_motion::reduced_motion;
use desk_motion::tokens::{EASE_OUT, PANEL_OUT_MS};
use gpui::{
    App, Div, ElementId, Entity, FontWeight, Motion, MouseButton, Pixels, Rgba, SharedString,
    Stateful, Window, div, prelude::*, px, relative,
};

use crate::component::icon;
use crate::components::avatar::AgentKind;
use crate::components::glyph::Glyph;
use crate::components::paint::{glyph, ink, pressed, solid, tint};
use crate::components::size::{
    AGENT_PAD_X, AGENT_PAD_Y, AGENT_TINT, BADGE_FILL, BADGE_PAD_X, BADGE_PAD_Y, CAPTION_TEXT, CHIP,
    CHIP_FILL, CHIP_PAD, CHIP_TEXT, CODE_FILL, CODE_PAD_X, CODE_PAD_Y, DIM_TEXT, FCHIP, FCHIP_PAD,
    FCHIP_TEXT, FONT_BADGE, FONT_KBD, FONT_SMALL, FONT_TAB, FONT_WHO, HOVER, IGNORED, KBD_PAD_X,
    KBD_PAD_Y, LINE_CAP, MENTION_TINT, RADIUS_BADGE, RADIUS_CHIP, RADIUS_CHIP_SMALL, T1, TRACE,
};
use crate::icon::Icon;
use crate::live::ActiveTheme;
use crate::metrics::{ICON_SMALL, ICON_TINY};
use crate::theme::{ColorToken, Theme, WordToken};

const MENTION_WIDTH: f32 = 220.0;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Tone {
    Live,
    Warn,
    Link,
    Added,
    Deleted,
}

impl Tone {
    pub fn color(self, theme: &Theme) -> Rgba {
        theme.color(match self {
            Tone::Live => ColorToken::StatusLive,
            Tone::Warn => ColorToken::StatusWarn,
            Tone::Link => ColorToken::StatusAccent,
            Tone::Added => ColorToken::SwitchOn,
            Tone::Deleted => ColorToken::GitDeleted,
        })
    }
}

pub fn tabular() -> gpui::FontFeatures {
    gpui::FontFeatures(std::sync::Arc::new(vec![("tnum".into(), 1)]))
}

pub fn mono(theme: &Theme) -> SharedString {
    theme.word(WordToken::ShapeMono)
}

pub fn badge(text: impl Into<SharedString>, theme: &Theme) -> Div {
    div()
        .flex_none()
        .px(px(BADGE_PAD_X))
        .py(px(BADGE_PAD_Y))
        .rounded(px(RADIUS_BADGE))
        .bg(ink(theme, BADGE_FILL))
        .font_family(mono(theme))
        .text_size(px(FONT_BADGE))
        .line_height(relative(1.0))
        .font_weight(FontWeight::MEDIUM)
        .text_color(ink(theme, CAPTION_TEXT))
        .child(text.into())
}

pub fn kbd(text: impl Into<SharedString>, theme: &Theme) -> Div {
    div()
        .flex_none()
        .px(px(KBD_PAD_X))
        .py(px(KBD_PAD_Y))
        .rounded(px(RADIUS_BADGE))
        .bg(ink(theme, CODE_FILL))
        .font_family(mono(theme))
        .text_size(px(FONT_KBD))
        .line_height(px(LINE_CAP))
        .font_weight(FontWeight::MEDIUM)
        .text_color(ink(theme, DIM_TEXT))
        .child(text.into())
}

pub fn chip(text: impl Into<SharedString>, trailing: Option<Glyph>, theme: &Theme) -> Div {
    div()
        .flex()
        .flex_none()
        .items_center()
        .gap_1p5()
        .h(px(CHIP))
        .px(px(CHIP_PAD))
        .rounded(px(RADIUS_CHIP))
        .bg(ink(theme, CHIP_FILL))
        .text_size(px(FONT_TAB))
        .font_weight(FontWeight::MEDIUM)
        .text_color(ink(theme, CHIP_TEXT))
        .child(text.into())
        .children(trailing.map(|trailing| glyph(trailing, ICON_SMALL, ink(theme, CAPTION_TEXT))))
}

type OnChip = Rc<dyn Fn(&mut Window, &mut App)>;

#[derive(IntoElement)]
pub struct Chip {
    id: ElementId,
    text: SharedString,
    open: Option<OnChip>,
    close: Option<OnChip>,
    gap_after: Pixels,
}

impl Chip {
    pub fn new(id: impl Into<ElementId>, text: impl Into<SharedString>) -> Self {
        Self {
            id: id.into(),
            text: text.into(),
            open: None,
            close: None,
            gap_after: Pixels::ZERO,
        }
    }

    pub fn gap_after(mut self, gap: Pixels) -> Self {
        self.gap_after = gap;
        self
    }

    pub fn on_open(mut self, on_open: impl Fn(&mut Window, &mut App) + 'static) -> Self {
        self.open = Some(Rc::new(on_open));
        self
    }

    pub fn on_close(mut self, on_close: impl Fn(&mut Window, &mut App) + 'static) -> Self {
        self.close = Some(Rc::new(on_close));
        self
    }
}

impl RenderOnce for Chip {
    fn render(self, window: &mut Window, cx: &mut App) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let fill = solid(ink(&theme, CHIP_FILL), theme.color(ColorToken::ToastFill));
        let hover = ink(&theme, HOVER);
        let mark = ink(&theme, CAPTION_TEXT);
        let closing = window.use_keyed_state(self.id.clone(), cx, |_, _| false);
        let shut = *closing.read(cx);
        let face = div()
            .id("chip-face")
            .flex()
            .flex_none()
            .items_center()
            .gap_1p5()
            .h(px(CHIP))
            .px(px(CHIP_PAD))
            .rounded(px(RADIUS_CHIP))
            .bg(fill)
            .text_size(px(FONT_TAB))
            .font_weight(FontWeight::MEDIUM)
            .text_color(ink(&theme, CHIP_TEXT));
        let face = match self.open {
            Some(open) => face
                .cursor_pointer()
                .hover(move |style| style.bg(solid(hover, fill)))
                .on_click(move |_, window, cx| open(window, cx))
                .child(self.text)
                .child(glyph(Glyph::Chevron, ICON_SMALL, mark)),
            None => face.child(self.text),
        };
        let face = face.when_some(self.close, |face, close| {
            face.child(
                remove_x("chip-close", mark, &theme)
                    .on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation())
                    .on_click(move |_, window, cx| {
                        cx.stop_propagation();
                        shrink(&closing, close.clone(), window, cx);
                    }),
            )
        });
        let leave = || Motion::new(PANEL_OUT_MS).with_easing(EASE_OUT);
        div()
            .id(self.id)
            .flex()
            .flex_none()
            .overflow_hidden()
            .opacity(if shut { 0.0 } else { 1.0 })
            .when(shut, |wrap| wrap.w(px(0.0)).mr(-self.gap_after))
            .transitions(|transitions| transitions.w(leave()).mr(leave()).opacity(leave()))
            .child(face)
    }
}

fn shrink(closing: &Entity<bool>, close: OnChip, window: &mut Window, cx: &mut App) {
    if reduced_motion(cx) {
        close(window, cx);
        return;
    }
    closing.update(cx, |closing, cx| {
        *closing = true;
        cx.notify();
    });
    let timer = cx.background_executor().timer(PANEL_OUT_MS);
    window
        .spawn(cx, async move |cx| {
            timer.await;
            cx.update(|window, cx| close(window, cx))
        })
        .detach_and_log_err(cx);
}

pub fn flat_chip(
    id: impl Into<ElementId>,
    text: impl Into<SharedString>,
    theme: &Theme,
) -> Stateful<Div> {
    let base = theme.color(ColorToken::ToastFill);
    let fill = solid(ink(theme, CHIP_FILL), base);
    let hover = solid(ink(theme, BADGE_FILL + CHIP_FILL), base);
    div()
        .id(id)
        .flex()
        .flex_none()
        .items_center()
        .gap_1p5()
        .h(px(CHIP))
        .px(px(CHIP_PAD))
        .rounded(px(RADIUS_CHIP))
        .cursor_pointer()
        .bg(fill)
        .text_size(px(FONT_SMALL))
        .font_weight(FontWeight::MEDIUM)
        .text_color(ink(theme, CHIP_TEXT))
        .hover(move |style| style.bg(hover))
        .child(text.into())
        .child(glyph(Glyph::Chevron, ICON_SMALL, ink(theme, CAPTION_TEXT)))
}

pub fn file_chip(lead: impl IntoElement, name: impl IntoElement, theme: &Theme) -> Div {
    div()
        .flex()
        .flex_none()
        .items_center()
        .gap_1p5()
        .h(px(FCHIP))
        .px(px(FCHIP_PAD))
        .rounded(px(RADIUS_CHIP_SMALL))
        .bg(ink(theme, CHIP_FILL))
        .font_family(mono(theme))
        .text_size(px(FONT_SMALL))
        .font_weight(FontWeight::MEDIUM)
        .text_color(ink(theme, FCHIP_TEXT))
        .child(lead)
        .child(div().min_w_0().truncate().child(name))
}

pub fn code(text: impl Into<SharedString>, theme: &Theme) -> Div {
    div()
        .flex_none()
        .px(px(CODE_PAD_X))
        .py(px(CODE_PAD_Y))
        .rounded(px(RADIUS_CHIP_SMALL))
        .bg(ink(theme, CODE_FILL))
        .font_family(mono(theme))
        .text_size(px(FONT_TAB))
        .line_height(relative(1.0))
        .text_color(ink(theme, T1))
        .child(text.into())
}

pub fn agent_pill(kind: AgentKind, text: impl Into<SharedString>, theme: &Theme) -> Div {
    let tone = kind.color(theme);
    div()
        .flex_none()
        .px(px(AGENT_PAD_X))
        .py(px(AGENT_PAD_Y))
        .rounded(px(RADIUS_CHIP_SMALL))
        .bg(tint(tone, AGENT_TINT))
        .text_size(px(FONT_WHO))
        .line_height(relative(1.0))
        .font_weight(FontWeight::MEDIUM)
        .text_color(tone)
        .child(text.into())
}

pub fn mention(
    id: impl Into<ElementId>,
    kind: Glyph,
    text: impl Into<SharedString>,
    broken: bool,
    theme: &Theme,
    on_remove: impl Fn(&gpui::ClickEvent, &mut gpui::Window, &mut gpui::App) + 'static,
) -> Div {
    let (mark, words) = match broken {
        true => (
            theme.color(ColorToken::StatusDanger),
            theme.color(ColorToken::StatusDanger),
        ),
        false => (
            theme.color(ColorToken::Trace),
            theme.color(ColorToken::MentionText),
        ),
    };
    div()
        .flex()
        .flex_none()
        .items_center()
        .gap_1p5()
        .max_w(px(MENTION_WIDTH))
        .h(px(FCHIP))
        .pl(px(FCHIP_PAD))
        .pr_1()
        .rounded(px(RADIUS_CHIP_SMALL))
        .bg(tint(mark, MENTION_TINT))
        .font_family(mono(theme))
        .text_size(px(FONT_SMALL))
        .font_weight(FontWeight::MEDIUM)
        .text_color(words)
        .child(glyph(kind, ICON_SMALL, mark))
        .child(div().min_w_0().truncate().child(text.into()))
        .child(remove_x(id, words, theme).on_click(on_remove))
}

pub fn remove_x(id: impl Into<ElementId>, color: Rgba, theme: &Theme) -> Stateful<Div> {
    let hover = ink(theme, HOVER);
    pressed(
        div()
            .id(id)
            .aria_label("Remove")
            .flex()
            .flex_none()
            .items_center()
            .justify_center()
            .size(px(ICON_SMALL + 2.0))
            .rounded(px(RADIUS_BADGE))
            .cursor_pointer()
            .hover(move |style| style.bg(hover)),
        color,
    )
    .child(icon(Icon::Close, ICON_TINY, color))
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum GitStatus {
    Modified,
    Added,
    Deleted,
    Untracked,
    Ignored,
    Conflict,
}

impl GitStatus {
    pub fn color(self, theme: &Theme) -> Rgba {
        match self {
            GitStatus::Modified => theme.color(ColorToken::GitModified),
            GitStatus::Added => theme.color(ColorToken::GitAdded),
            GitStatus::Deleted => theme.color(ColorToken::GitDeleted),
            GitStatus::Untracked => theme.color(ColorToken::GitUntracked),
            GitStatus::Ignored => ink(theme, IGNORED),
            GitStatus::Conflict => theme.color(ColorToken::StatusDanger),
        }
    }
}

pub fn git_name(name: impl Into<SharedString>, status: Option<GitStatus>, theme: &Theme) -> Div {
    let text = div().min_w_0().truncate().child(name.into());
    match status {
        None => text,
        Some(GitStatus::Deleted) => text
            .line_through()
            .text_color(GitStatus::Deleted.color(theme)),
        Some(GitStatus::Ignored) => text.italic().text_color(GitStatus::Ignored.color(theme)),
        Some(status) => text.text_color(status.color(theme)),
    }
}

pub fn trace(id: impl Into<ElementId>, label: SharedString, theme: &Theme) -> Stateful<Div> {
    let hover = ink(theme, HOVER);
    div()
        .id(id)
        .aria_label(label)
        .flex()
        .flex_none()
        .items_center()
        .justify_center()
        .size(px(TRACE))
        .rounded(px(RADIUS_CHIP))
        .cursor_pointer()
        .hover(move |style| style.bg(hover))
        .child(glyph(
            Glyph::Trace,
            ICON_SMALL,
            theme.color(ColorToken::Trace),
        ))
}
