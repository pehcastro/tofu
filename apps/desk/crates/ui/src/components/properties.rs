use gpui::{AnyElement, App, FontWeight, SharedString, Window, div, prelude::*, px};

use crate::components::paint::ink;
use crate::components::size::{CAPTION_TEXT, FONT_SMALL, T1};
use crate::live::ActiveTheme;
use crate::metrics::{CONTROL, TEXT};

pub const PROPERTIES_WIDTH: f32 = 264.0;
const LABEL_WIDTH: f32 = 84.0;
const SECTION_GAP: f32 = 18.0;

#[derive(IntoElement)]
pub struct PropertiesPanel {
    sections: Vec<(SharedString, Vec<(SharedString, AnyElement)>)>,
}

pub fn properties_panel() -> PropertiesPanel {
    PropertiesPanel {
        sections: Vec::new(),
    }
}

impl PropertiesPanel {
    pub fn section(
        mut self,
        title: impl Into<SharedString>,
        rows: impl IntoIterator<Item = (SharedString, AnyElement)>,
    ) -> Self {
        self.sections
            .push((title.into(), rows.into_iter().collect()));
        self
    }
}

impl RenderOnce for PropertiesPanel {
    fn render(self, _: &mut Window, cx: &mut App) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let sections = self.sections.into_iter().map(|(title, rows)| {
            let rows = rows.into_iter().map(|(label, value)| {
                div()
                    .flex()
                    .items_center()
                    .gap_2()
                    .min_h(px(CONTROL))
                    .when(!label.is_empty(), |row| {
                        row.child(
                            div()
                                .w(px(LABEL_WIDTH))
                                .flex_none()
                                .text_size(px(FONT_SMALL))
                                .text_color(ink(&theme, CAPTION_TEXT))
                                .child(label),
                        )
                    })
                    .child(
                        div()
                            .flex()
                            .flex_1()
                            .flex_wrap()
                            .items_center()
                            .gap_1p5()
                            .min_w_0()
                            .text_color(ink(&theme, T1))
                            .child(value),
                    )
            });
            div()
                .flex()
                .flex_col()
                .gap_0p5()
                .child(
                    div()
                        .pb_1()
                        .text_size(px(FONT_SMALL))
                        .font_weight(FontWeight::MEDIUM)
                        .text_color(ink(&theme, CAPTION_TEXT))
                        .child(title),
                )
                .children(rows)
        });
        div()
            .w(px(PROPERTIES_WIDTH))
            .flex_none()
            .flex()
            .flex_col()
            .gap(px(SECTION_GAP))
            .text_size(px(TEXT))
            .children(sections)
    }
}
