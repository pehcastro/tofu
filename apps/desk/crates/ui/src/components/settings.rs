use std::rc::Rc;

use gpui::{
    AnyElement, App, Div, ElementId, FontWeight, Rgba, SharedString, Window, div, prelude::*, px,
};

use crate::components::card::caption;
use crate::components::chip::{Tone, kbd};
use crate::components::paint::{ink, pressed, ring, tint};
use crate::components::size::T3;
use crate::theme::{ColorToken, Theme};

const TITLE: f32 = 18.0;
const TITLE_LINE: f32 = 21.6;
const ABOUT: f32 = 13.0;
const GROUP_PAD_X: f32 = 2.0;
const CARD_FILL: f32 = 0.2;
const CARD_RING: f32 = 0.05;
const CARD_RADIUS: f32 = 11.0;
const ROW_GAP: f32 = 14.0;
const ROW_PAD_Y: f32 = 11.0;
const ROW_RULE: f32 = 0.04;
const ROW_LINE: f32 = 18.0;
const ROW_NAME: f32 = 13.5;
const ROW_NAME_LINE: f32 = 16.0;
const ROW_ABOUT: f32 = 12.5;
const ROW_ABOUT_GAP: f32 = 3.0;
const CONTROL_MIN: f32 = 96.0;
const PILL_PAD_X: f32 = 7.0;
const PILL_PAD_Y: f32 = 1.0;
const PILL_TEXT: f32 = 11.0;
const PILL_FILL: f32 = 0.06;
const PILL_INK: f32 = 0.45;
const TONE_LIVE: f32 = 0.12;
const TONE_LINK: f32 = 0.14;
const TONE_WARN: f32 = 0.14;
const KEY_GAP: f32 = 4.0;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Source {
    Default,
    Global,
    Project,
}

impl Source {
    fn label(self) -> &'static str {
        match self {
            Source::Default => "default",
            Source::Global => "global",
            Source::Project => "project",
        }
    }
}

pub struct SettingRow {
    pub id: SharedString,
    pub name: SharedString,
    pub about: SharedString,
    pub source: Source,
    pub control: AnyElement,
}

fn pill(text: impl Into<SharedString>, fill: Rgba, color: Rgba) -> Div {
    div()
        .flex_none()
        .py(px(PILL_PAD_Y))
        .px(px(PILL_PAD_X))
        .rounded_full()
        .bg(fill)
        .text_size(px(PILL_TEXT))
        .text_color(color)
        .child(text.into())
}

fn toned(text: impl Into<SharedString>, tone: Tone, alpha: f32, theme: &Theme) -> Div {
    let color = tone.color(theme);
    pill(text, tint(color, alpha), color)
}

pub fn source_pill(source: Source, theme: &Theme) -> Div {
    match source {
        Source::Default => pill(source.label(), ink(theme, PILL_FILL), ink(theme, PILL_INK)),
        Source::Global => toned(source.label(), Tone::Link, TONE_LINK, theme),
        Source::Project => toned(source.label(), Tone::Live, TONE_LIVE, theme),
    }
}

pub fn page_title(
    title: impl Into<SharedString>,
    about: Option<SharedString>,
    theme: &Theme,
) -> Div {
    div()
        .flex()
        .flex_col()
        .gap_1p5()
        .child(
            div()
                .text_size(px(TITLE))
                .line_height(px(TITLE_LINE))
                .font_weight(FontWeight::SEMIBOLD)
                .child(title.into()),
        )
        .child(
            div()
                .mb_3()
                .text_size(px(ABOUT))
                .text_color(ink(theme, T3))
                .children(about),
        )
}

pub fn setting_row(row: SettingRow, theme: &Theme) -> Div {
    div()
        .flex()
        .items_center()
        .gap(px(ROW_GAP))
        .py(px(ROW_PAD_Y))
        .px_4()
        .border_b_1()
        .border_color(ink(theme, ROW_RULE))
        .child(
            div()
                .flex_1()
                .min_w_0()
                .flex()
                .flex_col()
                .line_height(px(ROW_LINE))
                .child(
                    div()
                        .text_size(px(ROW_NAME))
                        .line_height(px(ROW_NAME_LINE))
                        .child(row.name),
                )
                .when(!row.about.is_empty(), |text| {
                    text.child(
                        div()
                            .mt(px(ROW_ABOUT_GAP))
                            .text_size(px(ROW_ABOUT))
                            .text_color(ink(theme, T3))
                            .child(row.about),
                    )
                }),
        )
        .child(source_pill(row.source, theme))
        .child(
            div()
                .id(row.id)
                .min_w(px(CONTROL_MIN))
                .flex()
                .justify_end()
                .child(row.control),
        )
}

pub fn setting_group(title: impl Into<SharedString>, rows: Vec<SettingRow>, theme: &Theme) -> Div {
    group_card(
        title,
        rows.into_iter()
            .map(|row| setting_row(row, theme).into_any_element()),
        theme,
    )
}

pub fn setting_group_clickable(
    title: impl Into<SharedString>,
    rows: Vec<SettingRow>,
    on_row_click: impl Fn(&SharedString, &mut Window, &mut App) + 'static,
    theme: &Theme,
) -> Div {
    let on_row_click = Rc::new(on_row_click);
    let hover = theme.color(ColorToken::StateHover);
    let rows = rows.into_iter().map(|row| {
        let id = row.id.clone();
        let click = on_row_click.clone();
        pressed(
            setting_row(row, theme)
                .id(ElementId::Name(id.clone()))
                .cursor_pointer()
                .hover(move |style| style.bg(hover)),
            theme.color(ColorToken::CardsInnerFill),
        )
        .on_click(move |_, window, cx| click(&id, window, cx))
        .into_any_element()
    });
    group_card(title, rows, theme)
}

fn group_card(
    title: impl Into<SharedString>,
    rows: impl IntoIterator<Item = AnyElement>,
    theme: &Theme,
) -> Div {
    div()
        .flex()
        .flex_col()
        .gap_1p5()
        .child(caption(title, theme).pt_2p5().px(px(GROUP_PAD_X)).pb_2())
        .child(
            div()
                .flex()
                .flex_col()
                .mb_2p5()
                .rounded(px(CARD_RADIUS))
                .bg(tint(theme.color(ColorToken::Shadow), CARD_FILL))
                .shadow(vec![ring(ink(theme, CARD_RING))])
                .children(rows),
        )
}

pub fn key_binding(keys: Vec<SharedString>, conflict: Option<SharedString>, theme: &Theme) -> Div {
    div()
        .flex()
        .flex_none()
        .items_center()
        .gap(px(KEY_GAP))
        .children(conflict.map(|other| {
            toned(
                format!("conflicts with {other}"),
                Tone::Warn,
                TONE_WARN,
                theme,
            )
            .mr(px(KEY_GAP))
        }))
        .children(keys.into_iter().map(|key| kbd(key, theme)))
}
