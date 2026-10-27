use std::rc::Rc;

use gpui::{
    AnyElement, App, AppContext, Div, ElementId, FontWeight, Rgba, SharedString, Stateful, Window,
    div, prelude::*, px, relative,
};

use crate::components::avatar::NameTip;
use crate::components::card::caption;
use crate::components::chip::{Tone, kbd};
use crate::components::overlay::{actions, context_menu};
use crate::components::paint::{ink, pressed, ring, tint};
use crate::components::size::{FIELD_WIDTH, T3};
use crate::theme::{ColorToken, Mode, Theme};

const TITLE: f32 = 18.0;
const TITLE_LINE: f32 = 21.6;
const ABOUT: f32 = 13.0;
const GROUP_PAD_X: f32 = 2.0;
const CARD_FILL: f32 = 0.2;
const CARD_RING: f32 = 0.05;
const CARD_RADIUS: f32 = 11.0;
const ROW_GAP: f32 = 14.0;
const ROW_WRAP_GAP: f32 = 8.0;
const LABEL_MIN: f32 = 140.0;
const ROW_PAD_Y: f32 = 11.0;
const ROW_RULE: f32 = 0.04;
const ROW_LINE: f32 = 18.0;
const ROW_NAME: f32 = 13.5;
const ROW_NAME_LINE: f32 = 16.0;
const ROW_ABOUT: f32 = 12.5;
const ROW_ABOUT_GAP: f32 = 3.0;
const SCOPE_WIDTH: f32 = 88.0;
const PILL_PAD_X: f32 = 7.0;
const PILL_PAD_Y: f32 = 1.0;
const PILL_TEXT: f32 = 11.0;
const TONE_LIVE: f32 = 0.12;
const TONE_LINK: f32 = 0.14;
const TONE_WARN: f32 = 0.14;
const KEY_GAP: f32 = 4.0;
const LIGHT_RULE_GAIN: f32 = 2.5;
const HALF: f32 = 0.5;

fn rule(theme: &Theme, alpha: f32) -> f32 {
    match theme.mode() {
        Mode::Dark => alpha,
        Mode::Light => alpha * LIGHT_RULE_GAIN,
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Source {
    Default,
    Global,
    Project,
}

impl Source {
    fn written(self) -> &'static str {
        match self {
            Source::Default => "Built in: nothing written for it yet",
            Source::Global => "Written to your settings for every project",
            Source::Project => "Written to this project's settings",
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

pub type OnScope = Rc<dyn Fn(&SharedString, Source, &mut Window, &mut App)>;

const SCOPE_CHOICES: [(&str, Source); 2] = [
    ("Set for this project", Source::Project),
    ("Set globally", Source::Global),
];

fn scope_column(
    id: &SharedString,
    source: Source,
    on_scope: Option<OnScope>,
    theme: &Theme,
) -> AnyElement {
    let said = SharedString::from(source.written());
    let pill = match source {
        Source::Default => None,
        Source::Global => Some(toned("Global", Tone::Link, TONE_LINK, theme)),
        Source::Project => Some(toned("Project", Tone::Live, TONE_LIVE, theme)),
    };
    let column = div()
        .id(SharedString::from(format!("{id}-scope")))
        .flex_none()
        .w(px(SCOPE_WIDTH))
        .h_full()
        .flex()
        .items_center()
        .justify_end()
        .children(pill);
    let Some(on_scope) = on_scope else {
        return column
            .tooltip(move |_, cx| cx.new(|_| NameTip(said.clone())).into())
            .into_any_element();
    };
    let picked = id.clone();
    context_menu(actions(SCOPE_CHOICES.map(|(label, _)| label)))
        .id(SharedString::from(format!("{id}-scope-menu")))
        .flex_none()
        .self_stretch()
        .on_pick(move |at, window, cx| {
            if let Some((_, scope)) = SCOPE_CHOICES.get(*at) {
                on_scope(&picked, *scope, window, cx);
            }
        })
        .child(column)
        .into_any_element()
}

fn control_column(id: SharedString, control: AnyElement) -> Stateful<Div> {
    div()
        .id(id)
        .flex_none()
        .w(px(FIELD_WIDTH))
        .flex()
        .justify_end()
        .child(control)
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
        .flex_wrap()
        .items_center()
        .gap_x(px(ROW_GAP))
        .gap_y(px(ROW_WRAP_GAP))
        .py(px(ROW_PAD_Y))
        .px_4()
        .border_b_1()
        .border_color(ink(theme, rule(theme, ROW_RULE)))
        .child(
            div()
                .flex_1()
                .min_w(px(LABEL_MIN))
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
        .child(scope_column(&row.id, row.source, None, theme))
        .child(control_column(row.id, row.control))
}

pub fn setting_group(title: impl Into<SharedString>, rows: Vec<SettingRow>, theme: &Theme) -> Div {
    group_card(
        title,
        None,
        rows.into_iter()
            .map(|row| setting_row(row, theme).into_any_element()),
        theme,
    )
}

pub struct Badge {
    pub text: SharedString,
    pub tone: Tone,
}

pub struct SettingSection {
    pub title: SharedString,
    pub about: Option<SharedString>,
    pub rows: Vec<(SettingRow, Option<Badge>)>,
}

fn half_row(
    row: SettingRow,
    badge: Option<Badge>,
    on_scope: Option<OnScope>,
    theme: &Theme,
) -> Div {
    div()
        .flex()
        .items_center()
        .gap_x(px(ROW_GAP))
        .py(px(ROW_PAD_Y))
        .px_4()
        .border_b_1()
        .border_color(ink(theme, rule(theme, ROW_RULE)))
        .child(
            div()
                .w(relative(HALF))
                .min_w(px(LABEL_MIN))
                .flex()
                .flex_col()
                .line_height(px(ROW_LINE))
                .child(
                    div()
                        .flex()
                        .items_center()
                        .gap(px(KEY_GAP))
                        .text_size(px(ROW_NAME))
                        .line_height(px(ROW_NAME_LINE))
                        .font_weight(FontWeight::MEDIUM)
                        .child(row.name)
                        .children(badge.map(|badge| {
                            toned(badge.text, badge.tone, TONE_WARN, theme)
                                .font_weight(FontWeight::NORMAL)
                        })),
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
        .child(div().flex_1())
        .child(scope_column(&row.id, row.source, on_scope, theme))
        .child(control_column(row.id, row.control))
}

pub fn setting_section(section: SettingSection, on_scope: Option<OnScope>, theme: &Theme) -> Div {
    let rows = section
        .rows
        .into_iter()
        .map(|(row, badge)| half_row(row, badge, on_scope.clone(), theme).into_any_element());
    group_card(section.title, section.about, rows, theme)
}

pub fn setting_section_clickable(
    section: SettingSection,
    on_row_click: impl Fn(&SharedString, &mut Window, &mut App) + 'static,
    on_scope: Option<OnScope>,
    theme: &Theme,
) -> Div {
    let on_row_click = Rc::new(on_row_click);
    let hover = theme.color(ColorToken::StateHover);
    let rows = section.rows.into_iter().map(|(row, badge)| {
        let id = row.id.clone();
        let click = on_row_click.clone();
        pressed(
            half_row(row, badge, on_scope.clone(), theme)
                .id(ElementId::Name(id.clone()))
                .cursor_pointer()
                .hover(move |style| style.bg(hover)),
            theme.color(ColorToken::CardsInnerFill),
        )
        .on_click(move |_, window, cx| click(&id, window, cx))
        .into_any_element()
    });
    group_card(section.title, section.about, rows, theme)
}

fn group_card(
    title: impl Into<SharedString>,
    about: Option<SharedString>,
    rows: impl IntoIterator<Item = AnyElement>,
    theme: &Theme,
) -> Div {
    div()
        .flex()
        .flex_col()
        .gap_1p5()
        .child(
            div()
                .flex()
                .flex_col()
                .pt_2p5()
                .px(px(GROUP_PAD_X))
                .pb_2()
                .child(caption(title, theme))
                .children(about.map(|about| {
                    div()
                        .mt(px(ROW_ABOUT_GAP))
                        .text_size(px(ROW_ABOUT))
                        .text_color(ink(theme, T3))
                        .child(about)
                })),
        )
        .child(
            div()
                .flex()
                .flex_col()
                .mb_2p5()
                .rounded(px(CARD_RADIUS))
                .bg(match theme.mode() {
                    Mode::Dark => tint(theme.color(ColorToken::Shadow), CARD_FILL),
                    Mode::Light => theme.color(ColorToken::CardsInnerFill),
                })
                .shadow(vec![ring(ink(theme, rule(theme, CARD_RING)))])
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
