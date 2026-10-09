use std::borrow::Cow;
use std::time::{Instant, SystemTime, UNIX_EPOCH};

use crate::desk::screen_mark;
use desk_ui::components::avatar::spinner;
use desk_ui::components::card::{
    Header, caption, header_button, inner_card, outer_card, shell, strip_tab,
};
use desk_ui::components::chip::mono;
use desk_ui::components::glyph::Glyph;
use desk_ui::components::paint::{ink, ring};
use desk_ui::components::scroll::ScrollArea;
use desk_ui::components::size::{HEADER, HEADER_PAD_LEFT, HEADER_PAD_RIGHT, SHELL_PAD, T3};
use desk_ui::components::skeleton::{Hold, Stage, reveal};
use desk_ui::theme::{ColorToken, Theme, WordToken};
use gpui::{
    AnyElement, App, ClickEvent, Context, Div, FontWeight, HighlightStyle, Rgba, SharedString,
    StyledText, Task, div, prelude::*, px, relative, rgb_to_hsla,
};

const FONTS: [&[u8]; 6] = [
    include_bytes!("../../../../assets/fonts/Geist-Regular.ttf"),
    include_bytes!("../../../../assets/fonts/Geist-Medium.ttf"),
    include_bytes!("../../../../assets/fonts/Geist-SemiBold.ttf"),
    include_bytes!("../../../../assets/fonts/GeistMono-Regular.ttf"),
    include_bytes!("../../../../assets/fonts/GeistMono-Medium.ttf"),
    include_bytes!("../../../../assets/fonts/GeistMono-SemiBold.ttf"),
];

const GAP: f32 = 10.0;
const FRAME_PAD: f32 = 20.0;
const SHELL_RING: f32 = 0.06;
pub const BODY_TEXT: f32 = 0.9;
const FONT_BASE: f32 = 14.0;
pub const LINE: f32 = 23.0;
const PILL_ON: f32 = 0.12;
const PILL_OFF: f32 = 0.5;

pub struct Pills {
    pad: f32,
    radius: f32,
    fill: f32,
    font: f32,
    line: f32,
    item_x: f32,
    item_y: f32,
    item_radius: f32,
}

pub const SHELL_PILLS: Pills = Pills {
    pad: 2.0,
    radius: 8.0,
    fill: 0.04,
    font: 12.0,
    line: 12.0,
    item_x: 10.0,
    item_y: 3.0,
    item_radius: 6.0,
};

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Mark {
    Strong,
    Warn,
    Dim,
    Mono,
}

pub fn rich(parts: &[(&str, Mark)], theme: &Theme) -> StyledText {
    let mut text = String::new();
    let mut looks = Vec::new();
    let mut monos = Vec::new();
    for (part, mark) in parts {
        let range = text.len()..text.len() + part.len();
        text.push_str(part);
        let color = |color: Rgba| HighlightStyle {
            color: Some(rgb_to_hsla(color)),
            ..HighlightStyle::default()
        };
        match mark {
            Mark::Strong => looks.push((
                range,
                HighlightStyle {
                    font_weight: Some(FontWeight::SEMIBOLD),
                    ..HighlightStyle::default()
                },
            )),
            Mark::Warn => looks.push((range, color(theme.color(ColorToken::StatusWarn)))),
            Mark::Dim => looks.push((range, color(ink(theme, T3)))),
            Mark::Mono => monos.push((range, mono(theme))),
        }
    }
    StyledText::new(text)
        .with_highlights(looks)
        .with_font_family_overrides(monos)
}

pub fn load_fonts(cx: &App) -> Result<(), String> {
    let text = cx.text_system();
    if text.all_font_names().iter().any(|name| name == "Geist") {
        return Ok(());
    }
    text.add_fonts(FONTS.iter().map(|font| Cow::Borrowed(*font)).collect())
        .map_err(|error| format!("the desk cannot load Geist: {error}"))
}

pub fn note(text: impl Into<SharedString>, theme: &Theme) -> Div {
    div()
        .text_size(px(12.0))
        .text_color(ink(theme, T3))
        .child(text.into())
}

pub fn panel(title: impl Into<SharedString>, trailing: Option<AnyElement>, theme: &Theme) -> Div {
    outer_card(theme)
        .flex_col()
        .shadow(vec![ring(ink(theme, SHELL_RING))])
        .px(px(SHELL_PAD))
        .pb(px(SHELL_PAD))
        .child(
            div()
                .h(px(HEADER))
                .flex()
                .flex_none()
                .items_center()
                .gap(px(6.0))
                .pl(px(HEADER_PAD_LEFT))
                .pr(px(HEADER_PAD_RIGHT))
                .line_height(relative(1.0))
                .child(caption(title.into(), theme).flex_1())
                .children(trailing),
        )
}

pub fn pills<V: 'static, T: Copy + PartialEq + 'static>(
    id: &'static str,
    options: &[(T, &'static str)],
    chosen: T,
    look: &Pills,
    theme: &Theme,
    cx: &mut Context<V>,
    pick: fn(&mut V, T),
) -> Div {
    div()
        .flex()
        .flex_none()
        .gap(px(2.0))
        .p(px(look.pad))
        .rounded(px(look.radius))
        .bg(ink(theme, look.fill))
        .text_size(px(look.font))
        .line_height(px(look.line))
        .children(options.iter().enumerate().map(|(at, (value, label))| {
            let value = *value;
            let on = value == chosen;
            div()
                .id((id, at))
                .px(px(look.item_x))
                .py(px(look.item_y))
                .rounded(px(look.item_radius))
                .cursor_pointer()
                .when(on, |pill| {
                    pill.bg(ink(theme, PILL_ON))
                        .text_color(theme.color(ColorToken::TextStrong))
                })
                .when(!on, |pill| pill.text_color(ink(theme, PILL_OFF)))
                .on_click(cx.listener(move |view, _: &ClickEvent, _, cx| {
                    pick(view, value);
                    cx.notify();
                }))
                .child(*label)
        }))
}

pub fn titled(name: &'static str, trailing: Option<AnyElement>) -> Header {
    let (glyph, label) = named(name);
    Header::Title(glyph, label, trailing)
}

pub fn tabbed(name: &'static str, tabs: AnyElement, trailing: Option<AnyElement>) -> Header {
    let (glyph, label) = named(name);
    Header::TitleTabs(glyph, label, tabs, trailing)
}

fn named(name: &'static str) -> (Option<Glyph>, SharedString) {
    let mark = screen_mark(name);
    (
        mark.map(|mark| mark.glyph),
        mark.map_or(name, |mark| mark.label).into(),
    )
}

pub fn reread<V: 'static>(
    id: &'static str,
    asking: bool,
    theme: &Theme,
    cx: &mut Context<V>,
    again: fn(&mut V, &mut Context<V>),
) -> Div {
    div()
        .flex()
        .flex_none()
        .items_center()
        .gap(px(6.0))
        .when(asking, |strip| {
            strip.child(spinner(SharedString::from(format!("{id}-asking")), theme))
        })
        .child(
            header_button(
                SharedString::from(format!("{id}-reread")),
                "Read again",
                theme,
            )
            .on_click(cx.listener(move |view, _: &ClickEvent, _, cx| again(view, cx))),
        )
}

pub fn tile_tabs<V: 'static, T: Copy + PartialEq + 'static>(
    id: &'static str,
    options: &[(T, &'static str)],
    chosen: T,
    theme: &Theme,
    cx: &mut Context<V>,
    pick: fn(&mut V, T, &mut Context<V>),
) -> AnyElement {
    div()
        .flex()
        .h_full()
        .items_end()
        .gap_0p5()
        .children(options.iter().map(|(value, label)| {
            let value = *value;
            strip_tab(
                SharedString::from(format!("{id}-{label}")),
                (*label).into(),
                value == chosen,
                theme,
            )
            .on_click(cx.listener(move |view, _: &ClickEvent, _, cx| {
                pick(view, value, cx);
                cx.notify();
            }))
        }))
        .into_any_element()
}

pub fn stamp() -> u128 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_or(0, |since| since.as_millis())
}

#[derive(Default)]
pub struct Gate {
    hold: Hold,
    wake: Option<Task<()>>,
}

impl Gate {
    pub fn wait(&mut self) {
        self.hold.wait(Instant::now());
    }

    pub fn show<V: 'static>(
        &mut self,
        screen: &'static str,
        ready: bool,
        skeleton: impl FnOnce(&mut Context<V>) -> AnyElement,
        content: impl FnOnce(&mut Context<V>) -> AnyElement,
        cx: &mut Context<V>,
    ) -> AnyElement {
        match self.hold.stage(ready, Instant::now()) {
            Stage::Skeleton => skeleton(cx),
            Stage::Wake(left) => {
                self.wake = Some(cx.spawn(async move |this, cx| {
                    cx.background_executor().timer(left).await;
                    if let Err(error) = this.update(cx, |_, cx| cx.notify()) {
                        eprintln!(
                            "desk: {screen}: the skeleton hold ended on a closed screen: {error}"
                        );
                    }
                }));
                skeleton(cx)
            }
            Stage::Reveal(step, held) => {
                if let Some(held) = held {
                    eprintln!(
                        "desk: {screen}: first content frame at {} after {} ms of skeleton",
                        stamp(),
                        held.as_millis()
                    );
                }
                reveal(
                    SharedString::from(format!("{screen}-reveal")),
                    step,
                    content(cx),
                )
            }
            Stage::Shown => content(cx),
        }
    }
}

pub fn tile(header: Header, theme: &Theme, body: impl IntoElement) -> Div {
    shell(header, theme).relative().size_full().min_w_0().child(
        inner_card(theme)
            .font_family(theme.word(WordToken::ShapeFont))
            .text_size(px(FONT_BASE))
            .line_height(px(LINE))
            .text_color(ink(theme, BODY_TEXT))
            .child(body),
    )
}

pub fn window(header: Header, theme: &Theme, body: Div) -> Div {
    tile(
        header,
        theme,
        ScrollArea::new("screen").child(body.min_h_full().p(px(FRAME_PAD)).flex().flex_col()),
    )
}

pub fn panes() -> Div {
    div().flex().flex_wrap().gap(px(GAP))
}

pub fn fraction<E: Styled>(item: E, grow: f32, least: f32) -> E {
    item.flex_grow(grow).flex_basis(px(least)).min_w(px(least))
}

pub fn ellipsis<E: Styled>(item: E) -> E {
    item.flex_shrink_1().min_w_0().truncate()
}
