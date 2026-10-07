use gpui::{App, Div, ElementId, FontWeight, SharedString, Stateful, Window, div, prelude::*, px};
use std::rc::Rc;

use crate::components::button::{ButtonKind, button};
use crate::components::chip::kbd;
use crate::components::glyph::Glyph;
use crate::components::paint::ink;
use crate::components::size::{FONT_BODY, FONT_TITLE, T1, T2, T3};
use crate::theme::Theme;

const WIDTH: f32 = 420.0;
const HINT_KEYS: f32 = 112.0;

pub struct EmptyAction {
    pub label: SharedString,
    pub glyph: Option<Glyph>,
    pub keys: Option<SharedString>,
}

pub struct EmptyHint {
    pub keys: SharedString,
    pub label: SharedString,
}

pub fn empty_state(
    id: impl Into<ElementId>,
    title: impl Into<SharedString>,
    line: Option<SharedString>,
    actions: &[EmptyAction],
    hints: &[EmptyHint],
    theme: &Theme,
    on: impl Fn(usize, &mut Window, &mut App) + 'static,
) -> Stateful<Div> {
    let on = Rc::new(on);
    let buttons = actions.iter().enumerate().map(|(at, action)| {
        let on = on.clone();
        button(
            at,
            action.label.clone(),
            action.glyph,
            ButtonKind::Plain,
            theme,
        )
        .children(action.keys.clone().map(|keys| kbd(keys, theme)))
        .on_click(move |_, window, cx| on(at, window, cx))
    });
    let rows = hints.iter().map(|hint| {
        div()
            .flex()
            .items_center()
            .gap_3()
            .child(
                div()
                    .w(px(HINT_KEYS))
                    .flex()
                    .justify_end()
                    .child(kbd(hint.keys.clone(), theme)),
            )
            .child(div().text_color(ink(theme, T3)).child(hint.label.clone()))
    });
    let body = div()
        .w_full()
        .max_w(px(WIDTH))
        .flex()
        .flex_col()
        .items_center()
        .gap_4()
        .text_size(px(FONT_BODY))
        .child(
            div()
                .flex()
                .flex_col()
                .items_center()
                .gap_1()
                .child(
                    div()
                        .text_size(px(FONT_TITLE))
                        .font_weight(FontWeight::SEMIBOLD)
                        .text_color(ink(theme, T1))
                        .child(title.into()),
                )
                .children(
                    line.map(|line| div().text_center().text_color(ink(theme, T2)).child(line)),
                ),
        )
        .when(!actions.is_empty(), |body| {
            body.child(
                div()
                    .flex()
                    .flex_wrap()
                    .justify_center()
                    .gap_2()
                    .children(buttons.collect::<Vec<_>>()),
            )
        })
        .when(!hints.is_empty(), |body| {
            body.child(
                div()
                    .flex()
                    .flex_col()
                    .gap_1p5()
                    .children(rows.collect::<Vec<_>>()),
            )
        });
    div()
        .id(id)
        .size_full()
        .flex()
        .items_center()
        .justify_center()
        .p_4()
        .child(body)
}
