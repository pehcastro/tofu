use gpui::{App, ClickEvent, ElementId, FontWeight, SharedString, Window, div, prelude::*, px};

use crate::components::paint::{ink, pressed, ring};
use crate::components::size::{DIM_TEXT, FONT_TAB, HOVER, T1, TAB_ON};
use crate::live::ActiveTheme;
use crate::theme::ColorToken;

const PILL: f32 = 26.0;
const PILL_PAD: f32 = 11.0;
const PILL_RING: f32 = 0.1;
const PILL_RING_ON: f32 = 0.14;

type OnTab = Box<dyn Fn(&ClickEvent, &mut Window, &mut App)>;

#[derive(IntoElement)]
pub struct PillTabs {
    id: ElementId,
    tabs: Vec<(SharedString, bool, OnTab)>,
}

impl PillTabs {
    pub fn new(id: impl Into<ElementId>) -> Self {
        PillTabs {
            id: id.into(),
            tabs: Vec::new(),
        }
    }

    pub fn tab(
        mut self,
        label: impl Into<SharedString>,
        selected: bool,
        on_click: impl Fn(&ClickEvent, &mut Window, &mut App) + 'static,
    ) -> Self {
        self.tabs.push((label.into(), selected, Box::new(on_click)));
        self
    }
}

impl RenderOnce for PillTabs {
    fn render(self, _: &mut Window, cx: &mut App) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let backdrop = theme.color(ColorToken::CardsInnerFill);
        let hover = ink(&theme, HOVER);
        let pills = self
            .tabs
            .into_iter()
            .enumerate()
            .map(|(at, (label, selected, on_click))| {
                let pill = div()
                    .id(at)
                    .flex()
                    .flex_none()
                    .items_center()
                    .h(px(PILL))
                    .px(px(PILL_PAD))
                    .rounded_full()
                    .cursor_pointer()
                    .text_size(px(FONT_TAB))
                    .font_weight(FontWeight::MEDIUM)
                    .text_color(ink(&theme, if selected { T1 } else { DIM_TEXT }))
                    .bg(ink(&theme, if selected { TAB_ON } else { 0.0 }))
                    .shadow(vec![ring(ink(
                        &theme,
                        if selected { PILL_RING_ON } else { PILL_RING },
                    ))])
                    .when(!selected, |pill| pill.hover(move |style| style.bg(hover)))
                    .on_click(on_click)
                    .child(label);
                pressed(pill, backdrop)
            });
        div()
            .id(self.id)
            .flex()
            .items_center()
            .gap_1p5()
            .children(pills)
    }
}
