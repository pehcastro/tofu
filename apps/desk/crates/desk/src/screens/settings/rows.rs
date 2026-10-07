use std::collections::HashMap;

use desk_ui::component::icon;
use desk_ui::components::card::{caption, outer_card};
use desk_ui::components::chip::{chip, mono};
use desk_ui::components::paint::{ink, ring, tint};
use desk_ui::components::size::T3;
use desk_ui::icon::Icon;
use desk_ui::live::ActiveTheme;
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    AnyElement, ClickEvent, Context, Div, FontWeight, Render, Rgba, Window, div, prelude::*, px,
    relative,
};

use super::board::{inner, root, segment};
use super::fixture::{Control, NAV, PageId, Scope, Setting, Source};
use super::{ROW_TRACK, track};

const SHELL_HEADER: f32 = 28.0;
const SCOPE_FILL: f32 = 0.04;
const NAV_WIDTH: f32 = 247.0;
const NAV_TEXT: f32 = 13.5;
const NAV_RULE: f32 = 0.35;
const SEARCH: f32 = 30.0;
const SEARCH_ICON: f32 = 13.0;
const CONTENT_PAD_X: f32 = 28.0;
const CONTENT_PAD_Y: f32 = 20.0;
const TITLE: f32 = 18.0;
const TITLE_LINE: f32 = 21.6;
const DESC: f32 = 13.0;
const CARD_FILL: f32 = 0.2;
const CARD_RING: f32 = 0.05;
const CARD_RADIUS: f32 = 11.0;
const ROW_RULE: f32 = 0.04;
const ROW_NAME: f32 = 13.5;
const ROW_DESC: f32 = 12.5;
const ROW_LINE: f32 = 18.0;
const ROW_NAME_LINE: f32 = 16.0;
const ROW_DESC_GAP: f32 = 3.0;
const ROW_ON: f32 = 0.08;
const BADGE_TEXT: f32 = 11.0;
const BADGE_FILL: f32 = 0.06;
const BADGE_INK: f32 = 0.45;
const SOURCE_TINT_LIVE: f32 = 0.12;
const SOURCE_TINT_ACCENT: f32 = 0.14;
const CONTROL_MIN: f32 = 96.0;
const VALUE_TEXT: f32 = 12.0;
const SHELL_BOTTOM: f32 = 8.0;

pub struct Rows {
    page: PageId,
    scope: Scope,
    flipped: HashMap<&'static str, bool>,
}

impl Rows {
    pub fn new() -> Self {
        Rows {
            page: PageId::Turn,
            scope: Scope::Project,
            flipped: HashMap::new(),
        }
    }

    fn nav(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let mut entries: Vec<AnyElement> = Vec::new();
        for (label, items) in NAV {
            entries.push(
                caption(label, theme)
                    .pt_2p5()
                    .px_2p5()
                    .pb_1()
                    .into_any_element(),
            );
            for page in items.iter().copied() {
                entries.push(
                    div()
                        .id(page.name())
                        .flex()
                        .items_center()
                        .px_2p5()
                        .py_1p5()
                        .rounded_lg()
                        .cursor_pointer()
                        .when(page == self.page, |item| item.bg(ink(theme, ROW_ON)))
                        .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                            this.page = page;
                            cx.notify();
                        }))
                        .child(page.name())
                        .into_any_element(),
                );
            }
        }
        div()
            .w(px(NAV_WIDTH))
            .flex_none()
            .flex()
            .flex_col()
            .gap(px(1.0))
            .py_2p5()
            .px_2()
            .border_r_1()
            .border_color(black(theme, NAV_RULE))
            .text_size(px(NAV_TEXT))
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap_2()
                    .h(px(SEARCH))
                    .px_2p5()
                    .mb_2p5()
                    .rounded_lg()
                    .bg(theme.color(ColorToken::FieldFill))
                    .text_size(px(DESC))
                    .text_color(ink(theme, T3))
                    .child(icon(Icon::Search, SEARCH_ICON, ink(theme, T3)))
                    .child("Search settings"),
            )
            .children(entries)
    }

    fn row(
        &self,
        setting: &'static Setting,
        theme: &Theme,
        cx: &mut Context<Self>,
    ) -> gpui::Stateful<Div> {
        let flipped = self.flipped.get(setting.key).copied();
        let source = flipped.map_or(setting.source, |_| self.scope.source());
        let control = match setting.control {
            Control::Switch(start) => track(flipped.unwrap_or(start), ROW_TRACK, theme),
            Control::Value(value) => chip(value, None, theme)
                .font_family(mono(theme))
                .text_size(px(VALUE_TEXT)),
        };
        let (fill, text) = source_colors(source, theme);
        div()
            .id(setting.key)
            .flex()
            .items_center()
            .gap(px(14.0))
            .py(px(11.0))
            .px_4()
            .border_b_1()
            .border_color(ink(theme, ROW_RULE))
            .cursor_pointer()
            .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                if let Control::Switch(start) = setting.control {
                    let now = this.flipped.get(setting.key).copied().unwrap_or(start);
                    this.flipped.insert(setting.key, !now);
                    cx.notify();
                }
            }))
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
                            .child(setting.name),
                    )
                    .when(!setting.desc.is_empty(), |text| {
                        text.child(
                            div()
                                .mt(px(ROW_DESC_GAP))
                                .text_size(px(ROW_DESC))
                                .text_color(ink(theme, T3))
                                .child(setting.desc),
                        )
                    }),
            )
            .child(
                div()
                    .flex_none()
                    .py(px(1.0))
                    .px(px(7.0))
                    .rounded_full()
                    .bg(fill)
                    .text_size(px(BADGE_TEXT))
                    .text_color(text)
                    .child(source.label()),
            )
            .child(
                div()
                    .min_w(px(CONTROL_MIN))
                    .flex()
                    .justify_end()
                    .child(control),
            )
    }

    fn content(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let page = self.page.page();
        let groups: Vec<AnyElement> = page
            .groups
            .iter()
            .flat_map(|(label, settings)| {
                [
                    caption(*label, theme)
                        .pt_2p5()
                        .px(px(2.0))
                        .pb_2()
                        .into_any_element(),
                    div()
                        .flex()
                        .flex_col()
                        .mb_2p5()
                        .rounded(px(CARD_RADIUS))
                        .bg(black(theme, CARD_FILL))
                        .shadow(vec![ring(ink(theme, CARD_RING))])
                        .children(settings.iter().map(|setting| self.row(setting, theme, cx)))
                        .into_any_element(),
                ]
            })
            .collect();
        div()
            .flex_1()
            .min_w_0()
            .overflow_hidden()
            .px(px(CONTENT_PAD_X))
            .py(px(CONTENT_PAD_Y))
            .flex()
            .flex_col()
            .gap_1p5()
            .child(
                div()
                    .text_size(px(TITLE))
                    .line_height(px(TITLE_LINE))
                    .font_weight(FontWeight::SEMIBOLD)
                    .child(page.title),
            )
            .child(
                div()
                    .mb_3()
                    .text_size(px(DESC))
                    .text_color(ink(theme, T3))
                    .when(!page.desc.is_empty(), |desc| desc.child(page.desc)),
            )
            .children(groups)
    }
}

fn black(theme: &Theme, alpha: f32) -> Rgba {
    tint(theme.color(ColorToken::Shadow), alpha)
}

fn source_colors(source: Source, theme: &Theme) -> (Rgba, Rgba) {
    match source {
        Source::Default => (ink(theme, BADGE_FILL), ink(theme, BADGE_INK)),
        Source::Project => {
            let live = theme.color(ColorToken::StatusLive);
            (tint(live, SOURCE_TINT_LIVE), live)
        }
        Source::Global => {
            let accent = theme.color(ColorToken::StatusAccent);
            (tint(accent, SOURCE_TINT_ACCENT), accent)
        }
    }
}

impl Render for Rows {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let chosen = Scope::ALL
            .iter()
            .position(|scope| *scope == self.scope)
            .unwrap_or(0);
        let header = div()
            .flex()
            .flex_none()
            .items_center()
            .gap_1p5()
            .h(px(SHELL_HEADER))
            .pl(px(9.0))
            .pr_1p5()
            .child(caption("Settings", &theme).flex_1())
            .child(
                div()
                    .flex()
                    .gap(px(2.0))
                    .p(px(2.0))
                    .rounded(px(8.0))
                    .bg(ink(&theme, SCOPE_FILL))
                    .font_weight(FontWeight::MEDIUM)
                    .children(Scope::ALL.iter().zip(Scope::LABELS).enumerate().map(
                        |(at, (scope, label))| {
                            let scope = *scope;
                            segment(label, at == chosen, &theme)
                                .px(px(10.0))
                                .line_height(relative(1.0))
                                .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                                    this.scope = scope;
                                    cx.notify();
                                }))
                        },
                    )),
            );
        let shell = outer_card(&theme)
            .flex_col()
            .flex_1()
            .mb(px(SHELL_BOTTOM))
            .child(header)
            .child(
                inner(&theme)
                    .flex_row()
                    .child(self.nav(&theme, cx))
                    .child(self.content(&theme, cx)),
            );
        root(&theme, shell, None, |_, _, _| {})
    }
}
